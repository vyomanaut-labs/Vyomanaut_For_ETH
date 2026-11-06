package chainsettle

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq" // registers the "postgres" driver

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// ── database fixtures ────────────────────────────────────────────────────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func dsn(userKey, userFallback, passKey string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		envOr("PGHOST", "localhost"), envOr("PGPORT", "5432"), envOr(userKey, userFallback),
		os.Getenv(passKey), envOr("PGDATABASE", "vyomanaut_test"), envOr("PGSSLMODE", "disable"))
}

func openDB(t *testing.T, conn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", conn)
	if err != nil {
		t.Skipf("sql.Open failed, skipping live-DB test: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("live Postgres not reachable, skipping live-DB test: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// testEnv is a live database with the settlement tables emptied, so each test
// starts from a known state.
type testEnv struct {
	db     *sql.DB // the application role
	admin  *sql.DB // a superuser, for cleanup only
	owners []uuid.UUID
	provs  []uuid.UUID
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	e := &testEnv{
		db:    openDB(t, dsn("PGUSER", "vyomanaut_app", "PGPASSWORD")),
		admin: openDB(t, dsn("PGVERIFY_USER", "postgres", "PGVERIFY_PASSWORD")),
	}
	if _, err := e.db.Exec(`SELECT 1 FROM chain_periods LIMIT 1`); err != nil {
		t.Skipf("migration 002 is not applied; skipping: %v", err)
	}
	if _, err := e.db.Exec(`SELECT 1 FROM chain_refunds LIMIT 1`); err != nil {
		t.Skipf("migration 004 is not applied; skipping: %v", err)
	}
	e.reset(t)
	t.Cleanup(func() { e.cleanup(t) })
	return e
}

func (e *testEnv) reset(t *testing.T) {
	t.Helper()
	if _, err := e.admin.Exec(`
TRUNCATE chain_period_items, chain_period_payouts, chain_period_debits, chain_periods,
         chain_deposits, chain_refunds, chain_cursors`); err != nil {
		t.Fatalf("reset: %v", err)
	}
}

// cleanup removes the bindings this test made, so nothing it leaves behind can
// be picked up by a later test's service.
func (e *testEnv) cleanup(t *testing.T) {
	t.Helper()
	e.reset(t)
	for _, id := range e.provs {
		_, _ = e.admin.Exec(`DELETE FROM provider_payout_addresses WHERE provider_id = $1`, id)
	}
	for _, id := range e.owners {
		_, _ = e.admin.Exec(`DELETE FROM owner_wallets WHERE owner_id = $1`, id)
	}
}

func randHex(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func (e *testEnv) newOwner(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := e.db.Exec(`INSERT INTO owners (owner_id, phone_number, ed25519_public_key, smart_collect_vpa)
		VALUES ($1, $2, $3, $4)`, id, fmt.Sprintf("+91%x", randHex(5)), randHex(32), fmt.Sprintf("v.%x@icici", randHex(6)))
	if err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	e.owners = append(e.owners, id)
	return id
}

func (e *testEnv) newProvider(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := e.db.Exec(`INSERT INTO providers (provider_id, phone_number, ed25519_public_key, status,
		declared_storage_gb, city, region, asn) VALUES ($1,$2,$3,'ACTIVE',50,'TestCity','TestRegion','SIM-AS1')`,
		id, fmt.Sprintf("+91%x", randHex(5)), randHex(32))
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	e.provs = append(e.provs, id)
	return id
}

func randWallet() chain.Address {
	var a chain.Address
	copy(a[:], randHex(chain.AddressLen))
	return a
}

func (e *testEnv) bindOwner(t *testing.T, owner uuid.UUID, w chain.Address) {
	t.Helper()
	if _, err := e.db.Exec(`INSERT INTO owner_wallets (owner_id, wallet) VALUES ($1, $2)`, owner, w[:]); err != nil {
		t.Fatalf("bind owner wallet: %v", err)
	}
}

func (e *testEnv) bindProvider(t *testing.T, p uuid.UUID, w chain.Address) {
	t.Helper()
	if _, err := e.db.Exec(`INSERT INTO provider_payout_addresses (provider_id, wallet) VALUES ($1, $2)`, p, w[:]); err != nil {
		t.Fatalf("bind provider wallet: %v", err)
	}
}

// charge records a ledger CHARGE against an owner, in paise.
func (e *testEnv) charge(t *testing.T, owner uuid.UUID, paise int64) {
	t.Helper()
	_, err := e.db.Exec(`INSERT INTO owner_escrow_events (owner_id, event_type, amount_paise, idempotency_key)
		VALUES ($1, 'CHARGE', $2, $3)`, owner, paise, uuid.NewString()+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("insert charge: %v", err)
	}
}

// release records a ledger RELEASE to a provider, old enough to be settled.
func (e *testEnv) release(t *testing.T, provider uuid.UUID, paise int64) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := e.db.Exec(`INSERT INTO escrow_events (event_id, provider_id, event_type, amount_paise, idempotency_key, created_at)
		VALUES ($1, $2, 'RELEASE', $3, $4, NOW() - interval '1 hour')`,
		id, provider, paise, uuid.NewString()+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("insert release: %v", err)
	}
	return id
}

func (e *testEnv) count(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (e *testEnv) periodStatus(t *testing.T, period uint64) string {
	t.Helper()
	var s string
	if err := e.db.QueryRow(`SELECT status FROM chain_periods WHERE period = $1`, int64(period)).Scan(&s); err != nil {
		t.Fatalf("period %d: %v", period, err)
	}
	return s
}

// ledgerWithdrawals counts and sums an owner's WITHDRAWAL rows.
func (e *testEnv) ledgerWithdrawals(t *testing.T, owner uuid.UUID) (n, paise int64) {
	t.Helper()
	if err := e.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(amount_paise),0)::bigint FROM owner_escrow_events
		WHERE owner_id = $1 AND event_type = 'WITHDRAWAL'`, owner).Scan(&n, &paise); err != nil {
		t.Fatalf("ledger withdrawals: %v", err)
	}
	return n, paise
}

func (e *testEnv) ledgerDeposits(t *testing.T, owner uuid.UUID) (n, paise int64) {
	t.Helper()
	if err := e.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(amount_paise),0)::bigint FROM owner_escrow_events
		WHERE owner_id = $1 AND event_type = 'DEPOSIT'`, owner).Scan(&n, &paise); err != nil {
		t.Fatalf("ledger deposits: %v", err)
	}
	return n, paise
}

// ── an in-memory escrow that enforces the contract's rules ───────────────

type fakeEscrow struct {
	mu       sync.Mutex
	head     uint64
	events   []chain.DepositEvent
	refunds  []chain.RefundEvent
	credit   map[chain.Address]int64
	periods  map[uint64]*fakePeriod
	last     uint64
	chainNow time.Time
	window   time.Duration

	postErrs     []error // returned, in order, before a post is attempted
	postThenFail bool    // record the post, then report a failure (a lost reply)
	claimErrs    []error
	postCalls    []uint64
	claimCalls   []uint64
	logRanges    [][2]uint64
}

type fakePeriod struct {
	total       int64
	claimableAt time.Time
	vetoed      bool
	alloc       map[chain.Address]int64
	debits      map[chain.Address]int64
}

func newFakeEscrow() *fakeEscrow {
	return &fakeEscrow{
		credit: map[chain.Address]int64{}, periods: map[uint64]*fakePeriod{},
		chainNow: time.Now(), window: 120 * time.Second, head: 1000,
	}
}

func revert(reason string) error { return &chain.RevertError{Reason: reason} }

func (f *fakeEscrow) BlockNumber(context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.head, nil
}

func (f *fakeEscrow) DepositedEvents(_ context.Context, from, to uint64) ([]chain.DepositEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logRanges = append(f.logRanges, [2]uint64{from, to})
	var out []chain.DepositEvent
	for _, e := range f.events {
		if e.BlockNumber >= from && e.BlockNumber <= to {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeEscrow) RefundedEvents(_ context.Context, from, to uint64) ([]chain.RefundEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []chain.RefundEvent
	for _, e := range f.refunds {
		if e.BlockNumber >= from && e.BlockNumber <= to {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeEscrow) Credit(_ context.Context, o chain.Address) (*big.Int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return big.NewInt(f.credit[o]), nil
}

func (f *fakeEscrow) LastPeriod(context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last, nil
}

func (f *fakeEscrow) PeriodInfo(_ context.Context, p uint64) (chain.PeriodInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.periods[p]
	if !ok {
		return chain.PeriodInfo{Total: big.NewInt(0), Unclaimed: big.NewInt(0)}, nil
	}
	return chain.PeriodInfo{Total: big.NewInt(fp.total), Unclaimed: big.NewInt(fp.total),
		ClaimableAt: uint64(fp.claimableAt.Unix()), Vetoed: fp.vetoed}, nil
}

func (f *fakeEscrow) PostPeriod(_ context.Context, period uint64, _ [32]byte, owners []chain.Address,
	debits []*big.Int, accounts []chain.Address, amounts []*big.Int) (*chain.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.postCalls = append(f.postCalls, period)
	if len(f.postErrs) > 0 {
		err := f.postErrs[0]
		f.postErrs = f.postErrs[1:]
		return nil, err
	}
	if period <= f.last {
		return nil, revert("PeriodNotIncreasing(uint64,uint64)")
	}
	var sumD, sumA int64
	for i, o := range owners {
		d := debits[i].Int64()
		if d > f.credit[o] {
			return nil, revert("InsufficientCredit(address,uint256,uint256)")
		}
		sumD += d
	}
	for _, a := range amounts {
		sumA += a.Int64()
	}
	if sumD != sumA {
		return nil, revert("TotalMismatch(uint256,uint256)")
	}
	fp := &fakePeriod{total: sumD, claimableAt: f.chainNow.Add(f.window),
		alloc: map[chain.Address]int64{}, debits: map[chain.Address]int64{}}
	for i, o := range owners {
		f.credit[o] -= debits[i].Int64()
		fp.debits[o] += debits[i].Int64()
	}
	for i, a := range accounts {
		fp.alloc[a] += amounts[i].Int64()
	}
	f.periods[period] = fp
	f.last = period
	rc := &chain.Receipt{Status: 1}
	copy(rc.TxHash[:], randHex(32))
	if f.postThenFail {
		f.postThenFail = false
		return nil, fmt.Errorf("%w (tx lost)", chain.ErrReceiptTimeout)
	}
	return rc, nil
}

func (f *fakeEscrow) ClaimMany(_ context.Context, period uint64, accounts []chain.Address) (*chain.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimCalls = append(f.claimCalls, period)
	if len(f.claimErrs) > 0 {
		err := f.claimErrs[0]
		f.claimErrs = f.claimErrs[1:]
		return nil, err
	}
	fp, ok := f.periods[period]
	switch {
	case !ok:
		return nil, revert("UnknownPeriod(uint64)")
	case fp.vetoed:
		return nil, revert("PeriodIsVetoed(uint64)")
	case f.chainNow.Before(fp.claimableAt):
		return nil, revert("WindowStillOpen(uint64,uint64)")
	}
	for _, a := range accounts {
		fp.alloc[a] = 0
	}
	rc := &chain.Receipt{Status: 1}
	copy(rc.TxHash[:], randHex(32))
	return rc, nil
}

// veto cancels a period the way the guardian would: its debits return to the owners.
func (f *fakeEscrow) veto(period uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp := f.periods[period]
	fp.vetoed = true
	for o, d := range fp.debits {
		f.credit[o] += d
	}
}

func (f *fakeEscrow) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chainNow = f.chainNow.Add(d)
}

func (f *fakeEscrow) allocation(period uint64, a chain.Address) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.periods[period].alloc[a]
}

// service builds a Service wired to the fake, with its clock following the fake chain's.
func (e *testEnv) service(t *testing.T, f *fakeEscrow, cfg Config) *Service {
	t.Helper()
	s, err := New(e.db, f, f, cfg, WithoutLock(), WithClock(func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.chainNow
	}))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
