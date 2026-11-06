package anchor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// old is a countersign time safely past the settle lag.
func old(secondsAgo int) time.Time {
	return time.Now().UTC().Add(-time.Duration(secondsAgo) * time.Second).Truncate(time.Microsecond)
}

func newService(t *testing.T, d *testDB, c Contract, opts ...Option) *Service {
	t.Helper()
	s, err := New(d.app, c, Config{Interval: time.Minute}, append([]Option{WithoutLock()}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func rowOf(t *testing.T, d *testDB, epoch uint64) epochRow {
	t.Helper()
	r, ok, err := (&store{db: d.app}).byEpoch(context.Background(), epoch)
	if err != nil || !ok {
		t.Fatalf("epoch %d row: ok=%v err=%v", epoch, ok, err)
	}
	return r
}

func TestAnchorsFinishedReceiptsAndContinuesWhereItStopped(t *testing.T) {
	d := newTestDB(t)
	p := d.newProvider(t)
	for i := 0; i < 5; i++ {
		d.addReceipt(t, p, old(600-i))
	}
	c := newFakeContract()
	obs := &recorder{}
	s := newService(t, d, c, WithObserver(obs))
	ctx := context.Background()

	if err := s.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	r1 := rowOf(t, d, 1)
	if r1.State != stateAnchored || r1.Leaves != 5 || len(r1.AnchorTx) != 32 {
		t.Fatalf("epoch 1 = %+v", r1)
	}
	if rec := c.records[1]; rec.Root != r1.Root || rec.LeafCount != 5 {
		t.Fatalf("chain holds %+v, database %x/%d", rec, r1.Root, r1.Leaves)
	}

	// Nothing new: no epoch.
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if c.publishes != 1 {
		t.Fatalf("publishes = %d, want 1", c.publishes)
	}

	// New receipts start the next epoch exactly after the first.
	for i := 0; i < 3; i++ {
		d.addReceipt(t, p, old(100-i))
	}
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	r2 := rowOf(t, d, 2)
	if r2.State != stateAnchored || r2.Leaves != 3 {
		t.Fatalf("epoch 2 = %+v", r2)
	}
	if len(obs.anchored) != 2 || len(obs.reserved) != 2 {
		t.Fatalf("observer saw reserved=%v anchored=%v", obs.reserved, obs.anchored)
	}
}

func TestRootUsesTheSharedLeafAndTreeRules(t *testing.T) {
	d := newTestDB(t)
	p := d.newProvider(t)
	for i := 0; i < 6; i++ {
		d.addReceipt(t, p, old(300-i))
	}
	c := newFakeContract()
	if err := newService(t, d, c).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rs, err := (&store{db: d.app}).receipts(context.Background(), nil, nil, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	leaves, err := receiptleaf.LeafHashes(rs)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := c.records[1].Root, receiptleaf.MerkleRoot(leaves); got != want {
		t.Fatalf("anchored root %x, receiptleaf root %x", got, want)
	}
}

func TestSkipsPendingRowsAndRowsInsideTheSettleLag(t *testing.T) {
	d := newTestDB(t)
	p := d.newProvider(t)
	d.addReceipt(t, p, old(600))
	d.addReceipt(t, p, time.Time{})                                 // pending
	d.addReceipt(t, p, time.Now().UTC().Truncate(time.Microsecond)) // too fresh
	c := newFakeContract()
	if err := newService(t, d, c).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rowOf(t, d, 1).Leaves; got != 1 {
		t.Fatalf("leaves = %d, want only the one old finished receipt", got)
	}
}

func TestAFailedSendIsRetriedAsTheSameEpoch(t *testing.T) {
	d := newTestDB(t)
	p := d.newProvider(t)
	d.addReceipt(t, p, old(600))
	d.addReceipt(t, p, old(599))
	c := newFakeContract()
	c.failNext = errors.New("rpc unreachable")
	obs := &recorder{}
	s := newService(t, d, c, WithObserver(obs))
	ctx := context.Background()

	if err := s.RunOnce(ctx); err == nil {
		t.Fatal("expected the publish error")
	}
	if got := rowOf(t, d, 1); got.State != stateReserved || got.LastError == "" {
		t.Fatalf("after failure: %+v", got)
	}
	// A new receipt arrives while the epoch is stuck: it must not join epoch 1.
	d.addReceipt(t, p, old(598))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatalf("retry: %v", err)
	}
	r1 := rowOf(t, d, 1)
	if r1.State != stateAnchored || r1.Leaves != 2 {
		t.Fatalf("epoch 1 after retry = %+v (the reserved range must not change)", r1)
	}
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := rowOf(t, d, 2).Leaves; got != 1 {
		t.Fatalf("epoch 2 leaves = %d, want the late receipt", got)
	}
}

func TestALostReplyIsAdoptedNotRepeated(t *testing.T) {
	d := newTestDB(t)
	p := d.newProvider(t)
	d.addReceipt(t, p, old(600))
	c := newFakeContract()
	c.loseReply = true
	s := newService(t, d, c)
	ctx := context.Background()

	if err := s.RunOnce(ctx); err == nil {
		t.Fatal("expected the lost-reply error")
	}
	if err := s.RunOnce(ctx); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if c.publishes != 1 {
		t.Fatalf("publishes = %d: the epoch was sent twice", c.publishes)
	}
	if got := rowOf(t, d, 1); got.State != stateAnchored || got.AnchorTx != nil {
		t.Fatalf("adopted epoch = %+v (no transaction hash is known)", got)
	}
}

func TestAnEpochTakenByADifferentRootIsAbandonedAndRebatched(t *testing.T) {
	d := newTestDB(t)
	p := d.newProvider(t)
	d.addReceipt(t, p, old(600))
	d.addReceipt(t, p, old(599))
	c := newFakeContract()
	obs := &recorder{}
	s := newService(t, d, c, WithObserver(obs))
	ctx := context.Background()

	// Reserve epoch 1, then let someone else take the number before it is sent.
	c.failNext = errors.New("rpc unreachable")
	_ = s.RunOnce(ctx)
	c.forceRecord(1, chain.AnchorRecord{Root: [32]byte{9}, LeafCount: 1, Timestamp: 1})

	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := rowOf(t, d, 1); got.State != stateFailed {
		t.Fatalf("epoch 1 = %+v, want FAILED", got)
	}
	if len(obs.failed) != 1 {
		t.Fatalf("failed events = %v", obs.failed)
	}
	// The same two receipts are batched again, under the next epoch number.
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := rowOf(t, d, 2); got.State != stateAnchored || got.Leaves != 2 {
		t.Fatalf("epoch 2 = %+v", got)
	}
}

func TestEpochNumbersFollowTheChainWhenTheDatabaseWasReset(t *testing.T) {
	d := newTestDB(t)
	p := d.newProvider(t)
	d.addReceipt(t, p, old(600))
	c := newFakeContract()
	c.forceRecord(7, chain.AnchorRecord{Root: [32]byte{1}, LeafCount: 1, Timestamp: 5})
	if err := newService(t, d, c).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rowOf(t, d, 8); got.State != stateAnchored {
		t.Fatalf("epoch 8 = %+v", got)
	}
}

func TestMaxLeavesSplitsALargeBacklog(t *testing.T) {
	d := newTestDB(t)
	p := d.newProvider(t)
	for i := 0; i < 5; i++ {
		d.addReceipt(t, p, old(600-i))
	}
	c := newFakeContract()
	s, err := New(d.app, c, Config{MaxLeaves: 2}, WithoutLock())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := s.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for epoch, want := range map[uint64]int{1: 2, 2: 2, 3: 1} {
		if got := rowOf(t, d, epoch).Leaves; got != want {
			t.Fatalf("epoch %d leaves = %d, want %d", epoch, got, want)
		}
	}
}

func TestConfigRejectsALagBelowTheSharedSettleLag(t *testing.T) {
	d := newTestDB(t)
	if _, err := New(d.app, newFakeContract(), Config{Lag: time.Second}); err == nil {
		t.Fatal("a lag below receiptleaf.SettleLag must be rejected")
	}
	if _, err := New(d.app, newFakeContract(), Config{MaxLeaves: -1}); err == nil {
		t.Fatal("a negative leaf limit must be rejected")
	}
}

// ── the Verifier ─────────────────────────────────────────────────────────

func anchoredWorld(t *testing.T) (*testDB, *fakeContract, []uuid.UUID) {
	t.Helper()
	d := newTestDB(t)
	p := d.newProvider(t)
	var ids []uuid.UUID
	for i := 0; i < 7; i++ {
		ids = append(ids, d.addReceipt(t, p, old(900-i)))
	}
	c := newFakeContract()
	s := newService(t, d, c)
	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A second epoch, so the first epoch's bounds are exercised.
	for i := 0; i < 4; i++ {
		ids = append(ids, d.addReceipt(t, p, old(200-i)))
	}
	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d, c, ids
}

func TestVerifierMatchesAnUntouchedHistory(t *testing.T) {
	d, c, ids := anchoredWorld(t)
	v := NewVerifier(d.app, c, nil)
	ctx := context.Background()

	for epoch := uint64(1); epoch <= 2; epoch++ {
		rep, err := v.VerifyEpoch(ctx, epoch)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Verdict != VerdictMatch {
			t.Fatalf("epoch %d: %+v", epoch, rep)
		}
	}
	for _, id := range []uuid.UUID{ids[0], ids[3], ids[6], ids[7], ids[10]} {
		rr, err := v.VerifyReceipt(ctx, id.String())
		if err != nil {
			t.Fatalf("VerifyReceipt %s: %v", id, err)
		}
		if !rr.Included {
			t.Fatalf("receipt %s not proven included: %+v", id, rr)
		}
	}
	if latest, err := v.Latest(ctx); err != nil || latest != 2 {
		t.Fatalf("Latest = %d, %v", latest, err)
	}
}

func TestVerifierCatchesAnAlteredReceipt(t *testing.T) {
	d, c, ids := anchoredWorld(t)
	v := NewVerifier(d.app, c, nil)
	ctx := context.Background()

	// The operator, with database rights, changes one stored latency.
	if _, err := d.admin.Exec(`UPDATE audit_receipts SET response_latency_ms = 9999 WHERE receipt_id = $1`, ids[2]); err != nil {
		t.Fatal(err)
	}
	rep, err := v.VerifyEpoch(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictMismatch || rep.DBLeaves != 7 || rep.ChainLeaves != 7 {
		t.Fatalf("altered receipt not caught: %+v", rep)
	}
	// Epoch 2 is untouched and still matches.
	if rep2, _ := v.VerifyEpoch(ctx, 2); rep2.Verdict != VerdictMatch {
		t.Fatalf("epoch 2 should be unaffected: %+v", rep2)
	}
	rr, err := v.VerifyReceipt(ctx, ids[2].String())
	if err != nil {
		t.Fatal(err)
	}
	if rr.Included {
		t.Fatal("the altered receipt was still proven included")
	}
}

func TestVerifierCatchesADeletedAndAnInsertedReceipt(t *testing.T) {
	d, c, ids := anchoredWorld(t)
	v := NewVerifier(d.app, c, nil)
	ctx := context.Background()

	if _, err := d.admin.Exec(`DELETE FROM audit_receipts WHERE receipt_id = $1`, ids[1]); err != nil {
		t.Fatal(err)
	}
	rep, _ := v.VerifyEpoch(ctx, 1)
	if rep.Verdict != VerdictMismatch || rep.DBLeaves != 6 || rep.ChainLeaves != 7 {
		t.Fatalf("deleted receipt not caught: %+v", rep)
	}

	// A receipt slipped into the middle of epoch 2's range.
	second := rowOf(t, d, 2)
	d.addReceipt(t, d.newProvider(t), second.First.Add(time.Millisecond))
	rep, _ = v.VerifyEpoch(ctx, 2)
	if rep.Verdict != VerdictMismatch || rep.DBLeaves != 5 {
		t.Fatalf("inserted receipt not caught: %+v", rep)
	}
}

func TestVerifierReportsAnEpochTheChainNeverSaw(t *testing.T) {
	d, c, _ := anchoredWorld(t)
	v := NewVerifier(d.app, c, nil)
	c.mu.Lock()
	delete(c.records, 2)
	c.mu.Unlock()
	rep, err := v.VerifyEpoch(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictNotOnChain {
		t.Fatalf("verdict = %s", rep.Verdict)
	}
}

func TestVerifierErrorsForUnknownThings(t *testing.T) {
	d, c, _ := anchoredWorld(t)
	v := NewVerifier(d.app, c, nil)
	ctx := context.Background()
	if _, err := v.VerifyEpoch(ctx, 99); !errors.Is(err, ErrUnknownEpoch) {
		t.Fatalf("unknown epoch: %v", err)
	}
	if _, err := v.VerifyReceipt(ctx, uuid.NewString()); !errors.Is(err, ErrReceiptNotFound) {
		t.Fatalf("unknown receipt: %v", err)
	}
	// A receipt newer than every anchored epoch is not in any epoch yet.
	late := d.addReceipt(t, d.newProvider(t), old(40))
	if _, err := v.VerifyReceipt(ctx, late.String()); !errors.Is(err, ErrReceiptNotFound) {
		t.Fatalf("not-yet-anchored receipt: %v", err)
	}
	empty := newTestDB(t)
	if _, err := NewVerifier(empty.app, c, nil).Latest(ctx); !errors.Is(err, ErrNothingAnchored) {
		t.Fatalf("empty database: %v", err)
	}
}

type fakeHead struct{ block, finalized uint64 }

func (f fakeHead) Receipt(context.Context, [32]byte) (*chain.Receipt, error) {
	return &chain.Receipt{BlockNumber: f.block}, nil
}
func (f fakeHead) FinalizedBlock(context.Context) (uint64, error) { return f.finalized, nil }

func TestVerifierReportsFinality(t *testing.T) {
	d, c, _ := anchoredWorld(t)
	ctx := context.Background()
	soft, _ := NewVerifier(d.app, c, fakeHead{block: 500, finalized: 400}).VerifyEpoch(ctx, 1)
	if soft.Finality != FinalitySoft || soft.AnchorBlock != 500 {
		t.Fatalf("soft: %+v", soft)
	}
	fin, _ := NewVerifier(d.app, c, fakeHead{block: 300, finalized: 400}).VerifyEpoch(ctx, 1)
	if fin.Finality != FinalityFinalized {
		t.Fatalf("finalized: %+v", fin)
	}
	none, _ := NewVerifier(d.app, c, nil).VerifyEpoch(ctx, 1)
	if none.Finality != FinalityUnknown {
		t.Fatalf("unknown: %+v", none)
	}
}
