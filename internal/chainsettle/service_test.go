package chainsettle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

func testConfig() Config {
	return Config{MicroPerPaise: 10, Confirmations: 2, LogChunk: 10, SettleLag: 20 * time.Second}
}

// payoutSetup is one owner who has deposited and been charged, and two
// providers who have been released earnings and have payout wallets.
type payoutSetup struct {
	owner  uuid.UUID
	ownerW chain.Address
	p1, p2 uuid.UUID
	w1, w2 chain.Address
	r1, r2 uuid.UUID
}

func setupPayout(t *testing.T, e *testEnv, f *fakeEscrow) payoutSetup {
	t.Helper()
	s := payoutSetup{owner: e.newOwner(t), ownerW: randWallet(), p1: e.newProvider(t), p2: e.newProvider(t),
		w1: randWallet(), w2: randWallet()}
	e.bindOwner(t, s.owner, s.ownerW)
	e.bindProvider(t, s.p1, s.w1)
	e.bindProvider(t, s.p2, s.w2)
	f.credit[s.ownerW] = 10_000_000 // 10 tokens deposited on chain
	e.charge(t, s.owner, 100_000)   // 100,000 paise = 1,000,000 micro-units owed
	s.r1 = e.release(t, s.p1, 600)  // 6,000 micro-units
	s.r2 = e.release(t, s.p2, 400)  // 4,000 micro-units
	return s
}

type recordingObserver struct {
	nopObserver
	reserved, posted, claimed, failed int
	credited                          int64
}

func (o *recordingObserver) PeriodReserved(int64, int) { o.reserved++ }
func (o *recordingObserver) PeriodPosted(int64)        { o.posted++ }
func (o *recordingObserver) PeriodClaimed(int64)       { o.claimed++ }
func (o *recordingObserver) PeriodFailed(string)       { o.failed++ }
func (o *recordingObserver) DepositCredited(m int64)   { o.credited += m }

// randTx returns a random transaction hash. Real hashes never repeat, and the
// ledger idempotency key is derived from them, so tests must not reuse one.
func randTx() (h [32]byte) {
	copy(h[:], randHex(32))
	return h
}

func mustRun(t *testing.T, s *Service) {
	t.Helper()
	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
}

// ── deposits ─────────────────────────────────────────────────────────────

func TestDepositsAreCreditedOnceAndOnlyWhenTheWalletIsBound(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	owner, w := e.newOwner(t), randWallet()
	f.events = []chain.DepositEvent{{Owner: w, Amount: big.NewInt(12_345), BlockNumber: 10, TxHash: randTx(), LogIndex: 0}}
	svc := e.service(t, f, testConfig())

	mustRun(t, svc) // wallet unknown: recorded, not credited
	if e.count(t, `SELECT COUNT(*) FROM chain_deposits`) != 1 {
		t.Fatal("the log should be recorded even before the wallet is bound")
	}
	if n, _ := e.ledgerDeposits(t, owner); n != 0 {
		t.Fatal("an unbound wallet must not be credited")
	}

	e.bindOwner(t, owner, w)
	mustRun(t, svc)
	n, paise := e.ledgerDeposits(t, owner)
	if n != 1 || paise != 1234 { // 12,345 micro / 10 = 1,234 paise; 5 micro is dust
		t.Fatalf("ledger deposits = %d rows, %d paise; want 1 row of 1234", n, paise)
	}

	// the same log delivered again, and further cycles, must not credit again
	f.events = append(f.events, f.events[0])
	f.head += 50
	mustRun(t, svc)
	mustRun(t, svc)
	if n, _ := e.ledgerDeposits(t, owner); n != 1 {
		t.Fatalf("a deposit was credited %d times", n)
	}
}

func TestADepositBelowOnePaisaIsMarkedDoneWithoutALedgerRow(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	owner, w := e.newOwner(t), randWallet()
	e.bindOwner(t, owner, w)
	f.events = []chain.DepositEvent{{Owner: w, Amount: big.NewInt(7), BlockNumber: 5, TxHash: randTx()}}
	svc := e.service(t, f, testConfig())
	mustRun(t, svc)
	mustRun(t, svc)
	if n, _ := e.ledgerDeposits(t, owner); n != 0 {
		t.Fatal("7 micro-units are below one paisa and must not create a ledger row")
	}
	if e.count(t, `SELECT COUNT(*) FROM chain_deposits WHERE credited_at IS NOT NULL`) != 1 {
		t.Fatal("the deposit should be marked processed so it is not retried forever")
	}
}

func TestTheScannerWaitsForConfirmationsAndResumesFromItsCursor(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	cfg := testConfig()
	cfg.StartBlock = 1
	f.head = 100
	svc := e.service(t, f, cfg)

	mustRun(t, svc)
	want := [][2]uint64{{1, 10}, {11, 20}, {21, 30}, {31, 40}, {41, 50}, {51, 60}, {61, 70}, {71, 80}, {81, 90}, {91, 98}}
	if fmt.Sprint(f.logRanges) != fmt.Sprint(want) {
		t.Fatalf("log ranges = %v, want %v (two blocks of confirmations held back)", f.logRanges, want)
	}

	f.logRanges = nil
	owner, w := e.newOwner(t), randWallet()
	e.bindOwner(t, owner, w)
	f.events = []chain.DepositEvent{{Owner: w, Amount: big.NewInt(1000), BlockNumber: 99, TxHash: randTx()}}
	f.head = 105
	mustRun(t, svc)
	if fmt.Sprint(f.logRanges) != fmt.Sprint([][2]uint64{{99, 103}}) {
		t.Fatalf("resumed ranges = %v, want [[99 103]]", f.logRanges)
	}
	if n, paise := e.ledgerDeposits(t, owner); n != 1 || paise != 100 {
		t.Fatalf("the deposit in block 99 was not credited: %d rows, %d paise", n, paise)
	}
}

// ── payouts ──────────────────────────────────────────────────────────────

func TestReleasesArePostedThenClaimedOnceTheWindowIsOver(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	s := setupPayout(t, e, f)
	obs := &recordingObserver{}
	svc, err := New(e.db, f, f, testConfig(), WithoutLock(), WithObserver(obs),
		WithClock(func() time.Time { return f.chainNow }))
	if err != nil {
		t.Fatal(err)
	}

	mustRun(t, svc)
	if got := e.periodStatus(t, 1); got != statusPosted {
		t.Fatalf("period 1 is %s, want POSTED", got)
	}
	if e.count(t, `SELECT COUNT(*) FROM chain_period_items WHERE period = 1`) != 2 {
		t.Fatal("both releases should belong to period 1")
	}
	if f.credit[s.ownerW] != 10_000_000-10_000 {
		t.Fatalf("owner credit on chain = %d, want %d", f.credit[s.ownerW], 10_000_000-10_000)
	}
	if len(f.claimCalls) != 0 {
		t.Fatal("nothing may be claimed while the challenge window is open")
	}

	f.advance(f.window + 5*time.Second)
	mustRun(t, svc)
	if got := e.periodStatus(t, 1); got != statusClaimed {
		t.Fatalf("period 1 is %s, want CLAIMED", got)
	}
	if f.allocation(1, s.w1) != 0 || f.allocation(1, s.w2) != 0 {
		t.Fatal("the claims did not clear the allocations")
	}

	mustRun(t, svc) // nothing new to do
	mustRun(t, svc)
	if len(f.postCalls) != 1 || len(f.claimCalls) != 1 {
		t.Fatalf("post calls %v, claim calls %v; each should happen exactly once", f.postCalls, f.claimCalls)
	}
	if obs.reserved != 1 || obs.posted != 1 || obs.claimed != 1 || obs.failed != 0 {
		t.Fatalf("observer saw %+v", obs)
	}
}

func TestTheOnChainPayoutsMatchTheLedgerReleasesExactly(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	s := setupPayout(t, e, f)
	mustRun(t, e.service(t, f, testConfig()))

	f.mu.Lock()
	defer f.mu.Unlock()
	fp := f.periods[1]
	if fp.alloc[s.w1] != 6000 || fp.alloc[s.w2] != 4000 || fp.total != 10_000 {
		t.Fatalf("allocations %v total %d; want 6000 and 4000 totalling 10000", fp.alloc, fp.total)
	}
	if fp.debits[s.ownerW] != 10_000 {
		t.Fatalf("owner debit = %d, want 10000 (payouts and debits must match)", fp.debits[s.ownerW])
	}
}

func TestAProviderWithoutAWalletWaitsAndIsPaidOnceItHasOne(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	owner, ow := e.newOwner(t), randWallet()
	e.bindOwner(t, owner, ow)
	f.credit[ow] = 1_000_000
	e.charge(t, owner, 10_000)
	p := e.newProvider(t)
	e.release(t, p, 500)
	svc := e.service(t, f, testConfig())

	mustRun(t, svc)
	if e.count(t, `SELECT COUNT(*) FROM chain_periods`) != 0 {
		t.Fatal("a release for a provider with no wallet must not be posted")
	}
	st := &store{db: e.db}
	if n, err := st.unboundReleases(context.Background()); err != nil || n < 1 {
		t.Fatalf("unbound releases = %d, %v; want at least 1", n, err)
	}

	pw := randWallet()
	e.bindProvider(t, p, pw)
	mustRun(t, svc)
	if got := e.periodStatus(t, 1); got != statusPosted {
		t.Fatalf("once bound, the release should be posted; status %s", got)
	}
	if f.periods[1].alloc[pw] != 5000 {
		t.Fatalf("allocation = %d, want 5000", f.periods[1].alloc[pw])
	}
}

func TestWhenOwnersCannotFundEverythingTheOldestReleasesArePaidFirst(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	owner, ow := e.newOwner(t), randWallet()
	e.bindOwner(t, owner, ow)
	f.credit[ow] = 10_000_000
	e.charge(t, owner, 500) // 5,000 micro-units owed in total
	p := e.newProvider(t)
	e.bindProvider(t, p, randWallet())
	first := e.release(t, p, 300) // 3,000 micro-units each
	e.release(t, p, 300)
	e.release(t, p, 300)
	svc := e.service(t, f, testConfig())

	mustRun(t, svc)
	if n := e.count(t, `SELECT COUNT(*) FROM chain_period_items`); n != 1 {
		t.Fatalf("%d releases were batched; only the first fits within 5,000", n)
	}
	if e.count(t, `SELECT COUNT(*) FROM chain_period_items WHERE release_event_id = $1`, first) != 1 {
		t.Fatal("the oldest release should be paid first")
	}
	mustRun(t, svc) // 2,000 remain; a 3,000 release still does not fit
	if e.count(t, `SELECT COUNT(*) FROM chain_periods`) != 1 {
		t.Fatal("no further period should be reserved when nothing fits")
	}
}

func TestOwnersAreDebitedInProportionToTheirCharges(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	o1, o2 := e.newOwner(t), e.newOwner(t)
	w1, w2 := randWallet(), randWallet()
	e.bindOwner(t, o1, w1)
	e.bindOwner(t, o2, w2)
	f.credit[w1], f.credit[w2] = 10_000_000, 10_000_000
	e.charge(t, o1, 3000) // 30,000 micro-units
	e.charge(t, o2, 1000) // 10,000 micro-units
	p := e.newProvider(t)
	e.bindProvider(t, p, randWallet())
	e.release(t, p, 800) // 8,000 micro-units
	svc := e.service(t, f, testConfig())

	mustRun(t, svc)
	if f.periods[1].debits[w1] != 6000 || f.periods[1].debits[w2] != 2000 {
		t.Fatalf("debits = %v, want 6000 and 2000", f.periods[1].debits)
	}

	// the second round starts from what is still owed (24,000 and 8,000)
	e.release(t, p, 800)
	mustRun(t, svc)
	if f.periods[2].debits[w1] != 6000 || f.periods[2].debits[w2] != 2000 {
		t.Fatalf("second round debits = %v, want 6000 and 2000", f.periods[2].debits)
	}
}

// ── failure handling ─────────────────────────────────────────────────────

func TestATransientPostFailureRetriesTheSamePeriod(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	setupPayout(t, e, f)
	f.postErrs = []error{errors.New("rpc connection reset")}
	svc := e.service(t, f, testConfig())

	if err := svc.RunOnce(context.Background()); err == nil {
		t.Fatal("expected the failure to be reported")
	}
	if got := e.periodStatus(t, 1); got != statusReserved {
		t.Fatalf("status %s, want RESERVED", got)
	}
	var lastErr string
	_ = e.db.QueryRow(`SELECT COALESCE(last_error,'') FROM chain_periods WHERE period = 1`).Scan(&lastErr)
	if lastErr == "" {
		t.Fatal("the failure should be recorded on the period")
	}

	mustRun(t, svc)
	if got := e.periodStatus(t, 1); got != statusPosted {
		t.Fatalf("status %s, want POSTED after the retry", got)
	}
	if fmt.Sprint(f.postCalls) != "[1 1]" {
		t.Fatalf("post calls %v; the same period number must be retried", f.postCalls)
	}
}

// The reply to a post is lost, so the service does not know it succeeded. On
// the next cycle it must find the period on chain and adopt it, not post again.
func TestAPostWhoseReplyWasLostIsAdoptedNotRepeated(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	s := setupPayout(t, e, f)
	f.postThenFail = true
	svc := e.service(t, f, testConfig())

	if err := svc.RunOnce(context.Background()); err == nil {
		t.Fatal("expected the lost reply to be reported")
	}
	if f.last != 1 {
		t.Fatal("the fake chain should hold the period already")
	}
	mustRun(t, svc)
	if got := e.periodStatus(t, 1); got != statusPosted {
		t.Fatalf("status %s, want POSTED (adopted)", got)
	}
	if len(f.postCalls) != 1 {
		t.Fatalf("post calls %v; the period must not be posted twice", f.postCalls)
	}
	if f.credit[s.ownerW] != 10_000_000-10_000 {
		t.Fatalf("owner was debited %d, want exactly 10000", 10_000_000-f.credit[s.ownerW])
	}
}

func TestARejectedPostIsAbandonedAndItsReleasesAreBatchedAgain(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	setupPayout(t, e, f)
	f.postErrs = []error{revert("InsufficientCredit(address,uint256,uint256)")}
	svc := e.service(t, f, testConfig())

	mustRun(t, svc) // a deterministic rejection is not a retryable error
	if got := e.periodStatus(t, 1); got != statusFailed {
		t.Fatalf("period 1 is %s, want FAILED", got)
	}
	if e.count(t, `SELECT COUNT(*) FROM chain_period_items`) != 0 {
		t.Fatal("a failed period must free its releases")
	}

	mustRun(t, svc)
	if got := e.periodStatus(t, 2); got != statusPosted {
		t.Fatalf("period 2 is %s; the freed releases should be posted under a new number", got)
	}
	if e.count(t, `SELECT COUNT(*) FROM chain_period_items WHERE period = 2`) != 2 {
		t.Fatal("both releases should now belong to period 2")
	}
}

func TestAVetoedPeriodIsAbandonedAndPaidAgainInANewOne(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	s := setupPayout(t, e, f)
	svc := e.service(t, f, testConfig())

	mustRun(t, svc)
	f.veto(1) // the guardian cancels it inside the window; the owner's credit returns
	mustRun(t, svc)
	if got := e.periodStatus(t, 1); got != statusFailed {
		t.Fatalf("period 1 is %s, want FAILED after a veto", got)
	}
	mustRun(t, svc)
	if got := e.periodStatus(t, 2); got != statusPosted {
		t.Fatalf("period 2 is %s, want POSTED", got)
	}
	f.advance(f.window + 5*time.Second)
	mustRun(t, svc)
	if got := e.periodStatus(t, 2); got != statusClaimed {
		t.Fatalf("period 2 is %s, want CLAIMED", got)
	}
	if f.credit[s.ownerW] != 10_000_000-10_000 {
		t.Fatalf("after the veto and the repost the owner paid %d, want 10000", 10_000_000-f.credit[s.ownerW])
	}
}

func TestPeriodsAreNeverPostedOutOfOrder(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	owner, ow := e.newOwner(t), randWallet()
	e.bindOwner(t, owner, ow)
	f.credit[ow] = 10_000_000
	p := e.newProvider(t)
	pw := randWallet()
	e.bindProvider(t, p, pw)
	st := &store{db: e.db}
	for period, paise := range map[uint64]int64{1: 100, 2: 200} {
		rel := pendingRelease{EventID: e.release(t, p, paise), ProviderID: p, AmountPaise: paise, Wallet: pw}
		micro := paise * 10
		err := st.reserve(context.Background(), reservation{Period: period, Ref: referenceOf([]pendingRelease{rel}),
			TotalMicro: micro, Items: []pendingRelease{rel}, Payouts: []payout{{Wallet: pw, Micro: micro}},
			Debits: []Debit{{Wallet: ow, Micro: micro}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	f.postErrs = []error{errors.New("rpc timeout")}
	svc := e.service(t, f, testConfig())

	if err := svc.RunOnce(context.Background()); err == nil {
		t.Fatal("expected the transient failure")
	}
	if fmt.Sprint(f.postCalls) != "[1]" {
		t.Fatalf("post calls %v; period 2 must not be attempted while period 1 is stuck", f.postCalls)
	}
	mustRun(t, svc)
	if fmt.Sprint(f.postCalls) != "[1 1 2]" {
		t.Fatalf("post calls %v, want [1 1 2]", f.postCalls)
	}
}

func TestAClaimThatIsTooEarlyByTheChainsClockIsRetriedQuietly(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	setupPayout(t, e, f)
	svc := e.service(t, f, testConfig())
	mustRun(t, svc)

	f.advance(f.window + 5*time.Second)
	f.claimErrs = []error{revert("WindowStillOpen(uint64,uint64) args=0x00")}
	mustRun(t, svc) // not an error: the window simply is not over yet
	if got := e.periodStatus(t, 1); got != statusPosted {
		t.Fatalf("status %s, want POSTED", got)
	}
	mustRun(t, svc)
	if got := e.periodStatus(t, 1); got != statusClaimed {
		t.Fatalf("status %s, want CLAIMED", got)
	}
}

// ── coordination ─────────────────────────────────────────────────────────

func TestOnlyOneServiceHoldsTheSettlementLock(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	first, err := New(e.db, f, f, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	release, err := first.acquireLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	second, _ := New(e.db, f, f, testConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	if _, err := second.acquireLock(ctx); err == nil {
		t.Fatal("a second service acquired the lock while the first held it")
	}

	release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	third, err := second.acquireLock(ctx2)
	if err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
	third()
}

func TestConfigValidation(t *testing.T) {
	if _, err := New(nil, nil, nil, Config{MicroPerPaise: -1}); !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig", err)
	}
	if _, err := New(nil, nil, nil, Config{MaxPayees: -1}); !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig", err)
	}
}

func TestTheWaitingLineIsPrintedAtMostOncePerMinute(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	now := time.Unix(1_700_000_000, 0)
	s := &Service{now: func() time.Time { return now }}
	count := func() int { return strings.Count(out.String(), "earned release(s) are waiting") }

	s.logWaiting(3)
	s.logWaiting(3)
	now = now.Add(59 * time.Second)
	s.logWaiting(4)
	if count() != 1 {
		t.Fatalf("printed %d times within a minute, want 1", count())
	}
	now = now.Add(2 * time.Second)
	s.logWaiting(4)
	if count() != 2 {
		t.Fatalf("printed %d times after a minute, want 2", count())
	}
}
