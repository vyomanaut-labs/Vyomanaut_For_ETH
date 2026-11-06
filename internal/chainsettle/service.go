package chainsettle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// Escrow is the part of the escrow contract binding the service uses.
// *chain.EscrowContract implements it.
type Escrow interface {
	DepositedEvents(ctx context.Context, from, to uint64) ([]chain.DepositEvent, error)
	Credit(ctx context.Context, owner chain.Address) (*big.Int, error)
	LastPeriod(ctx context.Context) (uint64, error)
	PeriodInfo(ctx context.Context, period uint64) (chain.PeriodInfo, error)
	PostPeriod(ctx context.Context, period uint64, ref [32]byte, owners []chain.Address,
		debits []*big.Int, accounts []chain.Address, amounts []*big.Int) (*chain.Receipt, error)
	ClaimMany(ctx context.Context, period uint64, accounts []chain.Address) (*chain.Receipt, error)
}

// Head reports the chain's latest block. *chain.RPC implements it.
type Head interface {
	BlockNumber(ctx context.Context) (uint64, error)
}

// Observer receives what the service does, for metrics. Every method may be
// called from the service goroutine only.
type Observer interface {
	ScannedTo(block uint64)
	DepositsRecorded(n int)
	DepositCredited(micro int64)
	PeriodReserved(totalMicro int64, payees int)
	PeriodPosted(totalMicro int64)
	PeriodClaimed(totalMicro int64)
	PeriodFailed(reason string)
	UnboundReleases(n int64)
	StepError(step string)
}

type nopObserver struct{}

func (nopObserver) ScannedTo(uint64)          {}
func (nopObserver) DepositsRecorded(int)      {}
func (nopObserver) DepositCredited(int64)     {}
func (nopObserver) PeriodReserved(int64, int) {}
func (nopObserver) PeriodPosted(int64)        {}
func (nopObserver) PeriodClaimed(int64)       {}
func (nopObserver) PeriodFailed(string)       {}
func (nopObserver) UnboundReleases(int64)     {}
func (nopObserver) StepError(string)          {}

// Service mirrors the ledger onto the chain. Create one with New and call Run.
type Service struct {
	cfg    Config
	store  *store
	db     *sql.DB
	escrow Escrow
	head   Head
	obs    Observer
	now    func() time.Time
	noLock bool

	// lastWaitLog is when the "earnings are waiting for on-chain USDT" line was
	// last printed, so a long wait is one line a minute and not one per cycle.
	lastWaitLog time.Time
}

// Option customises a Service.
type Option func(*Service)

// WithObserver sends events to o.
func WithObserver(o Observer) Option { return func(s *Service) { s.obs = o } }

// WithClock replaces the time source (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithoutLock skips the Postgres advisory lock (tests running several services).
func WithoutLock() Option { return func(s *Service) { s.noLock = true } }

// New builds a Service. db must be the application role's pool.
func New(db *sql.DB, escrow Escrow, head Head, cfg Config, opts ...Option) (*Service, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	s := &Service{cfg: cfg, store: &store{db: db}, db: db, escrow: escrow, head: head, obs: nopObserver{}, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// RunOnce performs one full cycle: scan deposits, credit them, then advance
// payouts. Steps are independent: a failure in one does not skip the others,
// and every error is returned together.
func (s *Service) RunOnce(ctx context.Context) error {
	var errs []error
	step := func(name string, fn func(context.Context) error) {
		if err := fn(ctx); err != nil {
			s.obs.StepError(name)
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	step("scan-deposits", s.scanDeposits)
	step("credit-deposits", s.creditDeposits)
	step("settle", s.settle)
	return errors.Join(errs...)
}

const (
	advisoryLockKey    = 0x56594f4d41 // "VYOMA"
	lockRetry          = 15 * time.Second
	repeatLogEvery     = 6
	maxBackoffMultiple = 6
)

// Run settles until ctx ends. It holds a Postgres advisory lock so only one
// coordinator settles at a time.
func (s *Service) Run(ctx context.Context) {
	if !s.noLock {
		release, err := s.acquireLock(ctx)
		if err != nil {
			return // the context ended while waiting
		}
		defer release()
	}

	var last string
	var repeats, failures int
	for {
		if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			failures++
			msg := err.Error()
			if msg != last || repeats%repeatLogEvery == 0 {
				log.Printf("[CHAIN] cycle error (will retry): %v", err)
			}
			if msg == last {
				repeats++
			} else {
				last, repeats = msg, 1
			}
		} else {
			failures, last, repeats = 0, "", 0
		}

		wait := s.cfg.PollInterval * time.Duration(1+min(failures, maxBackoffMultiple))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (s *Service) acquireLock(ctx context.Context) (release func(), err error) {
	for {
		conn, cerr := s.db.Conn(ctx)
		if cerr == nil {
			var got bool
			if qerr := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, int64(advisoryLockKey)).Scan(&got); qerr == nil && got {
				return func() {
					_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, int64(advisoryLockKey))
					_ = conn.Close()
				}, nil
			}
			_ = conn.Close()
			log.Printf("[CHAIN] settlement lock is held elsewhere; waiting")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetry):
		}
	}
}
