// Package payment is declared in doc.go.
// Live-database tests for CloseAuditPeriods (audit_period.go).
//
// Tests:
//   - TestCloseAuditPeriodsOpensOneClosedPeriodPerActiveProvider
//   - TestCloseAuditPeriodsSkipsAProviderWithAPendingPeriod
//   - TestCloseAuditPeriodsChainsPeriodsWithoutGapOrOverlap
//   - TestCloseAuditPeriodsLetsTheReleaseLoopPayAProvider

package payment

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// periodsOf returns a provider's audit periods, oldest first.
func periodsOf(t *testing.T, db *sql.DB, providerID uuid.UUID) []struct {
	start, end time.Time
	computed   bool
} {
	t.Helper()
	rows, err := db.Query(`SELECT period_start, period_end, release_computed FROM audit_periods
		WHERE provider_id = $1 ORDER BY period_start`, providerID)
	if err != nil {
		t.Fatalf("periodsOf: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []struct {
		start, end time.Time
		computed   bool
	}
	for rows.Next() {
		var p struct {
			start, end time.Time
			computed   bool
		}
		if err := rows.Scan(&p.start, &p.end, &p.computed); err != nil {
			t.Fatalf("periodsOf scan: %v", err)
		}
		out = append(out, p)
	}
	return out
}

func setProviderStatus(t *testing.T, verify *sql.DB, providerID uuid.UUID, status string) {
	t.Helper()
	if _, err := verify.Exec(`UPDATE providers SET status = $2 WHERE provider_id = $1`, providerID, status); err != nil {
		t.Fatalf("setProviderStatus: %v", err)
	}
}

func TestCloseAuditPeriodsOpensOneClosedPeriodPerActiveProvider(t *testing.T) {
	db := openTestDB(t)
	verify := openVerifyDB(t)
	active := insertTestProvider(t, db, testProviderSpec{})
	vetting := insertTestProvider(t, db, testProviderSpec{})
	setProviderStatus(t, verify, vetting, "VETTING")

	until := time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)
	if _, err := CloseAuditPeriods(context.Background(), db, until); err != nil {
		t.Fatalf("CloseAuditPeriods: %v", err)
	}

	got := periodsOf(t, verify, active)
	if len(got) != 1 {
		t.Fatalf("ACTIVE provider has %d periods, want 1", len(got))
	}
	if !got[0].end.Equal(until) || got[0].computed {
		t.Errorf("period = %+v, want end %v and release_computed false", got[0], until)
	}
	var created time.Time
	if err := verify.QueryRow(`SELECT created_at FROM providers WHERE provider_id = $1`, active).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if !got[0].start.Equal(created) {
		t.Errorf("first period starts %v, want the provider's created_at %v", got[0].start, created)
	}
	if n := len(periodsOf(t, verify, vetting)); n != 0 {
		t.Errorf("VETTING provider has %d periods, want 0 (only ACTIVE providers earn releases)", n)
	}
}

func TestCloseAuditPeriodsSkipsAProviderWithAPendingPeriod(t *testing.T) {
	db := openTestDB(t)
	verify := openVerifyDB(t)
	p := insertTestProvider(t, db, testProviderSpec{})
	insertTestAuditPeriod(t, db, p) // pending, ends now

	if _, err := CloseAuditPeriods(context.Background(), db, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CloseAuditPeriods: %v", err)
	}
	if n := len(periodsOf(t, verify, p)); n != 1 {
		t.Fatalf("provider has %d periods, want 1: a second pending period would release the same "+
			"balance twice in one ComputeMonthlyRelease pass", n)
	}
}

func TestCloseAuditPeriodsChainsPeriodsWithoutGapOrOverlap(t *testing.T) {
	db := openTestDB(t)
	verify := openVerifyDB(t)
	p := insertTestProvider(t, db, testProviderSpec{})
	ctx := context.Background()

	first := time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)
	second := first.Add(2 * time.Minute)
	if _, err := CloseAuditPeriods(ctx, db, first); err != nil {
		t.Fatal(err)
	}
	// running again for the same moment adds nothing (pending period exists)
	if _, err := CloseAuditPeriods(ctx, db, first); err != nil {
		t.Fatal(err)
	}
	if _, err := verify.Exec(`UPDATE audit_periods SET release_computed = TRUE WHERE provider_id = $1`, p); err != nil {
		t.Fatal(err)
	}
	// a moment no later than the last period's end adds nothing either
	if _, err := CloseAuditPeriods(ctx, db, first); err != nil {
		t.Fatal(err)
	}
	if _, err := CloseAuditPeriods(ctx, db, second); err != nil {
		t.Fatal(err)
	}

	got := periodsOf(t, verify, p)
	if len(got) != 2 {
		t.Fatalf("provider has %d periods, want 2", len(got))
	}
	if !got[1].start.Equal(got[0].end) || !got[1].end.Equal(second) {
		t.Errorf("second period = [%v, %v), want [%v, %v)", got[1].start, got[1].end, got[0].end, second)
	}
}

func TestCloseAuditPeriodsLetsTheReleaseLoopPayAProvider(t *testing.T) {
	db := openTestDB(t)
	verify := openVerifyDB(t)
	drainPendingReleaseCandidates(t, verify)
	p := insertTestProvider(t, db, testProviderSpec{})

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		insertTestAuditReceiptForRelease(t, verify, p, now.Add(-time.Duration(i)*time.Hour), "PASS")
	}
	refreshProviderScoresForRelease(t, verify)
	if err := InsertEscrowEvent(context.Background(), db, p, EscrowDeposit, 31000, testEscrowSeedKey(), nil); err != nil {
		t.Fatalf("seed balance: %v", err)
	}
	if _, err := verify.Exec(`REFRESH MATERIALIZED VIEW mv_provider_escrow_balance`); err != nil {
		t.Fatalf("refresh balance view: %v", err)
	}

	var released int64
	mock := &recordingPaymentProvider{
		onRelease: func(providerID uuid.UUID, amountPaise int64, auditPeriodID uuid.UUID, idempotencyKey string) error {
			if providerID != p {
				return nil // other ACTIVE providers in the shared test database
			}
			released += amountPaise
			err := InsertEscrowEvent(context.Background(), db, providerID, EscrowRelease, amountPaise, idempotencyKey, &auditPeriodID)
			if errors.Is(err, ErrDuplicateIdempotencyKey) {
				return nil
			}
			return err
		},
	}

	// exactly what one tick of runReleaseOnTicker does
	closePeriodsAndRelease(context.Background(), db, db, testDemoProfileForRelease(), mock)

	if released != 31000 {
		t.Fatalf("released %d paise, want 31000 (full multiplier on a 10/10 PASS score)", released)
	}
	got := periodsOf(t, verify, p)
	if len(got) != 1 || !got[0].computed {
		t.Fatalf("periods = %+v, want one period marked release_computed", got)
	}

	// the next tick releases nothing more: the balance is spent
	if _, err := verify.Exec(`REFRESH MATERIALIZED VIEW mv_provider_escrow_balance`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	closePeriodsAndRelease(context.Background(), db, db, testDemoProfileForRelease(), mock)
	if released != 31000 {
		t.Fatalf("second tick released more: total %d paise", released)
	}
	var rows int
	if err := verify.QueryRow(`SELECT COUNT(*) FROM escrow_events WHERE provider_id = $1 AND event_type = 'RELEASE'`, p).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("RELEASE rows = %d, want 1", rows)
	}
}
