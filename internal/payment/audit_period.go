// Package payment is declared in doc.go.
// This file creates the audit_periods rows that ComputeMonthlyRelease pays
// against.
//
// [Fixed] Before this file, no production code path inserted into
// audit_periods at all: ComputeMonthlyRelease was fully implemented but its
// pendingReleaseCandidates query always came back empty in a live run, so no
// provider was ever released anything (only the tests, which seed the table
// by hand, ever saw a release). See scripts/test/helpers_test.go's
// seedAuditPeriodsForActiveProviders for the original note.
//
// [Decision] CloseAuditPeriods writes a period that is already over: it runs
// on the release loop's own cadence and covers [end of the provider's latest
// period, now), so the release computed in the same tick pays for exactly the
// time the provider just served. The cadence is the release loop's
// (every ReleaseComputationInterval in demo, the 23rd of the month in
// production), so no new profile field is needed.
//
// [Decision] A provider that still has a pending period (release_computed =
// FALSE, for example because the last release was skipped on a stale score)
// gets no new one. ComputeMonthlyRelease releases against the provider's
// whole balance once per pending period and reads that balance from a
// materialised view, so two pending periods in one pass would release the
// same balance twice.
//
// [Known limit] A period written here has already ended, so an announced
// departure (internal/api computeAnnouncedDepartureRelease) finds no open
// period to prorate against and releases nothing; the held balance is then
// handled by the existing departure rules. That is unchanged from before this
// file, when no period existed at all.

package payment

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/config"
)

// CloseAuditPeriods inserts one audit_periods row, ending at until, for every
// ACTIVE provider that has no pending period and whose latest period (or, for
// a first period, whose registration) ended before until. It returns the
// number of rows inserted. Safe to run concurrently: the table's
// no-overlap exclusion constraint turns a duplicate into a skipped row.
func CloseAuditPeriods(ctx context.Context, db *sql.DB, until time.Time) (int64, error) {
	const insert = `
INSERT INTO audit_periods (provider_id, period_start, period_end)
SELECT p.provider_id, COALESCE(last.period_end, p.created_at), $1
FROM providers p
LEFT JOIN LATERAL (
    SELECT MAX(a.period_end) AS period_end
    FROM audit_periods a
    WHERE a.provider_id = p.provider_id
) last ON TRUE
WHERE p.status = 'ACTIVE'
  AND COALESCE(last.period_end, p.created_at) < $1
  AND NOT EXISTS (
      SELECT 1 FROM audit_periods pending
      WHERE pending.provider_id = p.provider_id AND pending.release_computed = FALSE
  )
ON CONFLICT DO NOTHING`
	res, err := db.ExecContext(ctx, insert, until)
	if err != nil {
		return 0, fmt.Errorf("payment.CloseAuditPeriods: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("payment.CloseAuditPeriods: rows affected: %w", err)
	}
	return n, nil
}

// closePeriodsAndRelease is one tick of the release loop: close the audit
// periods that are due, then compute their releases. A failure to close
// periods does not stop the release of periods that already exist.
func closePeriodsAndRelease(ctx context.Context, db, primaryDB *sql.DB, profile config.NetworkProfile, provider PaymentProvider) {
	_, _ = CloseAuditPeriods(ctx, db, time.Now()) // best-effort; retried next tick
	_ = ComputeMonthlyRelease(ctx, db, primaryDB, profile, provider)
}
