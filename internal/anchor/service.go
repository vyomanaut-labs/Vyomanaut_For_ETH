package anchor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// Contract is the part of the AuditAnchor contract the Service uses.
// *chain.AnchorContract satisfies it.
type Contract interface {
	LastEpoch(ctx context.Context) (uint64, error)
	Roots(ctx context.Context, epoch uint64) (chain.AnchorRecord, error)
	Publish(ctx context.Context, epoch uint64, root [32]byte, leafCount uint32) (*chain.Receipt, error)
}

// Observer receives events for metrics. Methods must not block.
type Observer interface {
	EpochReserved(leaves int)
	EpochAnchored(epoch uint64, leaves int)
	EpochFailed(reason string)
	StepError(step string)
}

type nopObserver struct{}

func (nopObserver) EpochReserved(int)         {}
func (nopObserver) EpochAnchored(uint64, int) {}
func (nopObserver) EpochFailed(string)        {}
func (nopObserver) StepError(string)          {}

// Option customises a Service.
type Option func(*Service)

// WithObserver sends events to o.
func WithObserver(o Observer) Option { return func(s *Service) { s.obs = o } }

// WithClock replaces the time source (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithoutLock skips the Postgres advisory lock (tests running several services).
func WithoutLock() Option { return func(s *Service) { s.noLock = true } }

// Service anchors audit history. Build one with New.
type Service struct {
	cfg      Config
	store    *store
	db       *sql.DB
	contract Contract
	obs      Observer
	now      func() time.Time
	noLock   bool
}

// New builds a Service. db must be the application role's pool.
func New(db *sql.DB, contract Contract, cfg Config, opts ...Option) (*Service, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	s := &Service{cfg: cfg, store: &store{db: db}, db: db, contract: contract, obs: nopObserver{}, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// RunOnce finishes a reserved epoch if there is one, then builds and sends the
// next. It anchors at most one new epoch per call.
func (s *Service) RunOnce(ctx context.Context) error {
	if row, ok, err := s.store.reserved(ctx); err != nil {
		s.obs.StepError("read-reserved")
		return fmt.Errorf("read reserved epoch: %w", err)
	} else if ok {
		return s.publish(ctx, row)
	}

	row, ok, err := s.build(ctx)
	if err != nil {
		s.obs.StepError("build")
		return err
	}
	if !ok {
		return nil
	}
	return s.publish(ctx, row)
}

// build selects the next batch and reserves it. ok is false when there is
// nothing new to anchor.
func (s *Service) build(ctx context.Context) (epochRow, bool, error) {
	var after *position
	if p, found, err := s.store.cursor(ctx); err != nil {
		return epochRow{}, false, fmt.Errorf("read cursor: %w", err)
	} else if found {
		after = &p
	}

	receipts, err := s.store.receipts(ctx, after, nil, s.now().Add(-s.cfg.Lag), s.cfg.MaxLeaves)
	if err != nil {
		return epochRow{}, false, fmt.Errorf("read receipts: %w", err)
	}
	if len(receipts) == 0 {
		return epochRow{}, false, nil
	}

	leaves, err := receiptleaf.LeafHashes(receipts)
	if err != nil {
		return epochRow{}, false, fmt.Errorf("hash receipts: %w", err)
	}
	first, last := receipts[0], receipts[len(receipts)-1]

	chainLast, err := s.contract.LastEpoch(ctx)
	if err != nil {
		return epochRow{}, false, fmt.Errorf("read last epoch: %w", err)
	}
	dbMax, err := s.store.maxEpoch(ctx)
	if err != nil {
		return epochRow{}, false, fmt.Errorf("read newest epoch: %w", err)
	}

	row := epochRow{
		Epoch:  max(chainLast, dbMax) + 1,
		State:  stateReserved,
		Root:   receiptleaf.MerkleRoot(leaves),
		Leaves: len(receipts),
		First:  first.ServiceCountersignTS,
		Last:   position{TS: last.ServiceCountersignTS, ID: receiptleaf.FormatUUID(last.ReceiptID)},
	}
	if err := s.store.reserve(ctx, row); err != nil {
		return epochRow{}, false, fmt.Errorf("reserve epoch %d: %w", row.Epoch, err)
	}
	s.obs.EpochReserved(row.Leaves)
	log.Printf("[ANCHOR] reserved epoch %d: %d receipts", row.Epoch, row.Leaves)
	return row, true, nil
}

// publish sends a reserved epoch, or adopts it when the chain already has it.
func (s *Service) publish(ctx context.Context, row epochRow) error {
	// A reply lost after the chain accepted the epoch would otherwise make the
	// next attempt revert: ask the chain first.
	rec, err := s.contract.Roots(ctx, row.Epoch)
	if err != nil {
		s.obs.StepError("read-root")
		return fmt.Errorf("read epoch %d: %w", row.Epoch, err)
	}
	if rec.Timestamp != 0 {
		return s.settleExisting(ctx, row, rec)
	}

	rc, err := s.contract.Publish(ctx, row.Epoch, row.Root, uint32(row.Leaves)) //nolint:gosec // bounded by MaxLeaves
	if err != nil {
		s.store.recordError(ctx, row.Epoch, err.Error())
		s.obs.StepError("publish")
		if errors.Is(err, chain.ErrReverted) {
			// Often the epoch landed in the meantime: look again before giving up.
			if rec, rerr := s.contract.Roots(ctx, row.Epoch); rerr == nil && rec.Timestamp != 0 {
				return s.settleExisting(ctx, row, rec)
			}
		}
		return fmt.Errorf("epoch %d: publish: %w", row.Epoch, err)
	}
	if err := s.store.markAnchored(ctx, row.Epoch, rc.TxHash[:]); err != nil {
		return fmt.Errorf("epoch %d: mark anchored: %w", row.Epoch, err)
	}
	s.obs.EpochAnchored(row.Epoch, row.Leaves)
	log.Printf("[ANCHOR] epoch %d anchored: %d receipts, root %x", row.Epoch, row.Leaves, row.Root[:8])
	return nil
}

// settleExisting handles an epoch number the chain already holds: ours (adopt)
// or someone else's (abandon, so the same receipts go into a later epoch).
func (s *Service) settleExisting(ctx context.Context, row epochRow, rec chain.AnchorRecord) error {
	if rec.Root == row.Root && int(rec.LeafCount) == row.Leaves {
		if err := s.store.markAnchored(ctx, row.Epoch, nil); err != nil {
			return fmt.Errorf("epoch %d: adopt: %w", row.Epoch, err)
		}
		s.obs.EpochAnchored(row.Epoch, row.Leaves)
		log.Printf("[ANCHOR] epoch %d is already on chain; adopting it", row.Epoch)
		return nil
	}
	const reason = "the chain holds a different root for this epoch number"
	if err := s.store.markFailed(ctx, row.Epoch, reason); err != nil {
		return fmt.Errorf("epoch %d: mark failed: %w", row.Epoch, err)
	}
	s.obs.EpochFailed(reason)
	log.Printf("[ANCHOR] abandoning epoch %d: %s", row.Epoch, reason)
	return nil
}

const (
	advisoryLockKey    = 0x56594f414e // "VYOAN"
	lockRetry          = 15 * time.Second
	maxBackoffMultiple = 6
	repeatLogEvery     = 6
)

// Run anchors every Interval until ctx ends. It holds a Postgres advisory lock
// so only one coordinator anchors at a time.
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
			if msg := err.Error(); msg != last || repeats%repeatLogEvery == 0 {
				log.Printf("[ANCHOR] cycle error (will retry): %v", err)
			}
			if msg := err.Error(); msg == last {
				repeats++
			} else {
				last, repeats = msg, 1
			}
		} else {
			failures, last, repeats = 0, "", 0
		}

		wait := s.cfg.Interval
		if failures > 0 {
			wait = min(s.cfg.Interval, lockRetry) * time.Duration(1+min(failures, maxBackoffMultiple))
		}
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
			log.Printf("[ANCHOR] anchoring lock is held elsewhere; waiting")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetry):
		}
	}
}
