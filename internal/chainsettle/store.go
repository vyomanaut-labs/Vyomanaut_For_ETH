package chainsettle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// Period states, as stored in chain_periods.status.
const (
	statusReserved = "RESERVED"
	statusPosted   = "POSTED"
	statusClaimed  = "CLAIMED"
	statusFailed   = "FAILED"
)

const (
	cursorDeposits   = "deposits"
	depositKeyDomain = "vyomanaut/chain-deposit/v1"
	maxErrorText     = 500
)

type store struct{ db *sql.DB }

func toInt64(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("chainsettle: %d does not fit in a BIGINT", v)
	}
	return int64(v), nil
}

func clip(s string) string {
	if len(s) > maxErrorText {
		return s[:maxErrorText]
	}
	return s
}

// ── deposits ─────────────────────────────────────────────────────────────

func (s *store) cursor(ctx context.Context, name string) (block uint64, found bool, err error) {
	var b int64
	err = s.db.QueryRowContext(ctx, `SELECT block_number FROM chain_cursors WHERE name = $1`, name).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("chainsettle: read cursor: %w", err)
	}
	return uint64(b), true, nil
}

func (s *store) setCursor(ctx context.Context, name string, block uint64) error {
	b, err := toInt64(block)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO chain_cursors (name, block_number) VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET block_number = EXCLUDED.block_number, updated_at = NOW()`, name, b)
	if err != nil {
		return fmt.Errorf("chainsettle: write cursor: %w", err)
	}
	return nil
}

// recordDeposit stores one Deposited log. It reports whether the log was new.
func (s *store) recordDeposit(ctx context.Context, ev chain.DepositEvent) (bool, error) {
	if !ev.Amount.IsInt64() || ev.Amount.Sign() <= 0 {
		return false, nil // not representable or empty; the caller logs it
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO chain_deposits (tx_hash, log_index, wallet, amount_micro, block_number)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tx_hash, log_index) DO NOTHING`,
		ev.TxHash[:], int64(ev.LogIndex), ev.Owner[:], ev.Amount.Int64(), int64(ev.BlockNumber))
	if err != nil {
		return false, fmt.Errorf("chainsettle: record deposit: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// depositIdempotencyKey is the ledger idempotency key of one deposit log.
func depositIdempotencyKey(txHash []byte, logIndex int64) string {
	var idx [8]byte
	binary.BigEndian.PutUint64(idx[:], uint64(logIndex))
	sum := sha256.Sum256(append(append([]byte(depositKeyDomain), txHash...), idx[:]...))
	return fmt.Sprintf("%x", sum)
}

type creditedDeposit struct {
	Micro, Paise int64
	Owner        uuid.UUID
}

// creditPending credits every recorded deposit whose wallet is now bound to an
// owner. Each deposit is credited in one transaction together with marking it
// done, so it can never be credited twice.
func (s *store) creditPending(ctx context.Context, microPerPaise int64) ([]creditedDeposit, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("chainsettle: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
SELECT d.tx_hash, d.log_index, d.amount_micro, w.owner_id
FROM chain_deposits d
JOIN owner_wallets w ON w.wallet = d.wallet
WHERE d.credited_at IS NULL
ORDER BY d.block_number, d.tx_hash, d.log_index
FOR UPDATE OF d SKIP LOCKED`)
	if err != nil {
		return nil, fmt.Errorf("chainsettle: select deposits: %w", err)
	}
	type pending struct {
		txHash   []byte
		logIndex int64
		micro    int64
		owner    uuid.UUID
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.txHash, &p.logIndex, &p.micro, &p.owner); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("chainsettle: scan deposit: %w", err)
		}
		todo = append(todo, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()

	out := make([]creditedDeposit, 0, len(todo))
	for _, p := range todo {
		paise := p.micro / microPerPaise // any remainder is dust below one paisa
		if paise > 0 {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO owner_escrow_events (owner_id, event_type, amount_paise, idempotency_key)
VALUES ($1, 'DEPOSIT', $2, $3)
ON CONFLICT (idempotency_key) DO NOTHING`, p.owner, paise, depositIdempotencyKey(p.txHash, p.logIndex)); err != nil {
				return nil, fmt.Errorf("chainsettle: credit ledger: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE chain_deposits SET owner_id = $3, amount_paise = $4, credited_at = NOW()
WHERE tx_hash = $1 AND log_index = $2`, p.txHash, p.logIndex, p.owner, paise); err != nil {
			return nil, fmt.Errorf("chainsettle: mark deposit credited: %w", err)
		}
		out = append(out, creditedDeposit{Micro: p.micro, Paise: paise, Owner: p.owner})
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("chainsettle: commit: %w", err)
	}
	return out, nil
}

// ── payouts ──────────────────────────────────────────────────────────────

type pendingRelease struct {
	EventID     uuid.UUID
	ProviderID  uuid.UUID
	AmountPaise int64
	Wallet      chain.Address
}

// unsettledReleases returns RELEASE rows older than cutoff that belong to no
// period and whose provider has a payout wallet.
func (s *store) unsettledReleases(ctx context.Context, cutoff time.Time, limit int) ([]pendingRelease, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT e.event_id, e.provider_id, e.amount_paise, a.wallet
FROM escrow_events e
JOIN provider_payout_addresses a ON a.provider_id = e.provider_id
WHERE e.event_type = 'RELEASE'
  AND e.created_at <= $1
  AND NOT EXISTS (SELECT 1 FROM chain_period_items i WHERE i.release_event_id = e.event_id)
ORDER BY e.created_at, e.event_id
LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("chainsettle: select releases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []pendingRelease
	for rows.Next() {
		var r pendingRelease
		var w []byte
		if err := rows.Scan(&r.EventID, &r.ProviderID, &r.AmountPaise, &w); err != nil {
			return nil, fmt.Errorf("chainsettle: scan release: %w", err)
		}
		if len(w) != chain.AddressLen {
			return nil, fmt.Errorf("chainsettle: stored wallet is %d bytes", len(w))
		}
		copy(r.Wallet[:], w)
		out = append(out, r)
	}
	return out, rows.Err()
}

// unboundReleases counts RELEASE rows waiting only for a payout wallet.
func (s *store) unboundReleases(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM escrow_events e
WHERE e.event_type = 'RELEASE'
  AND NOT EXISTS (SELECT 1 FROM provider_payout_addresses a WHERE a.provider_id = e.provider_id)
  AND NOT EXISTS (SELECT 1 FROM chain_period_items i WHERE i.release_event_id = e.event_id)`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("chainsettle: count unbound releases: %w", err)
	}
	return n, nil
}

type ownerWeight struct {
	Wallet chain.Address
	Weight int64
}

// ownerWeights returns, for every owner with a bound wallet, what they still
// owe providers on chain: ledger charges (converted to micro-units) minus what
// earlier periods already debited.
func (s *store) ownerWeights(ctx context.Context, microPerPaise int64) ([]ownerWeight, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT w.wallet,
       COALESCE((SELECT SUM(e.amount_paise) FROM owner_escrow_events e
                 WHERE e.owner_id = w.owner_id AND e.event_type = 'CHARGE'), 0)::bigint * $1
     - COALESCE((SELECT SUM(d.amount_micro) FROM chain_period_debits d
                 JOIN chain_periods p ON p.period = d.period
                 WHERE d.wallet = w.wallet AND p.status <> 'FAILED'), 0)::bigint AS weight
FROM owner_wallets w
ORDER BY w.wallet`, microPerPaise)
	if err != nil {
		return nil, fmt.Errorf("chainsettle: owner weights: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ownerWeight
	for rows.Next() {
		var ow ownerWeight
		var w []byte
		if err := rows.Scan(&w, &ow.Weight); err != nil {
			return nil, fmt.Errorf("chainsettle: scan owner weight: %w", err)
		}
		copy(ow.Wallet[:], w)
		if ow.Weight > 0 {
			out = append(out, ow)
		}
	}
	return out, rows.Err()
}

func (s *store) maxPeriod(ctx context.Context) (uint64, error) {
	var m int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(period), 0) FROM chain_periods`).Scan(&m); err != nil {
		return 0, fmt.Errorf("chainsettle: max period: %w", err)
	}
	return uint64(m), nil
}

type payout struct {
	Wallet chain.Address
	Micro  int64
}

type reservation struct {
	Period     uint64
	Ref        [32]byte
	TotalMicro int64
	Items      []pendingRelease
	Payouts    []payout
	Debits     []Debit
}

// referenceOf is a fingerprint of the ledger rows a period pays, so the on-chain
// record can be tied back to them.
func referenceOf(items []pendingRelease) [32]byte {
	ids := make([][]byte, len(items))
	for i, it := range items {
		id := it.EventID
		ids[i] = id[:]
	}
	sort.Slice(ids, func(a, b int) bool { return string(ids[a]) < string(ids[b]) })
	h := sha256.New()
	for _, id := range ids {
		_, _ = h.Write(id)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// reserve records a period and everything in it in one transaction.
func (s *store) reserve(ctx context.Context, r reservation) error {
	period, err := toInt64(r.Period)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("chainsettle: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO chain_periods (period, status, ref, total_micro) VALUES ($1, 'RESERVED', $2, $3)`,
		period, r.Ref[:], r.TotalMicro); err != nil {
		return fmt.Errorf("chainsettle: insert period: %w", err)
	}
	for _, it := range r.Items {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO chain_period_items (release_event_id, period, provider_id, amount_paise) VALUES ($1, $2, $3, $4)`,
			it.EventID, period, it.ProviderID, it.AmountPaise); err != nil {
			return fmt.Errorf("chainsettle: insert item: %w", err)
		}
	}
	for _, p := range r.Payouts {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO chain_period_payouts (period, wallet, amount_micro) VALUES ($1, $2, $3)`,
			period, p.Wallet[:], p.Micro); err != nil {
			return fmt.Errorf("chainsettle: insert payout: %w", err)
		}
	}
	for _, d := range r.Debits {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO chain_period_debits (period, wallet, amount_micro) VALUES ($1, $2, $3)`,
			period, d.Wallet[:], d.Micro); err != nil {
			return fmt.Errorf("chainsettle: insert debit: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("chainsettle: commit: %w", err)
	}
	return nil
}

type periodRow struct {
	Period      uint64
	TotalMicro  int64
	ClaimableAt sql.NullTime
}

func (s *store) periodsWithStatus(ctx context.Context, status string) ([]periodRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT period, total_micro, claimable_at FROM chain_periods WHERE status = $1 ORDER BY period`, status)
	if err != nil {
		return nil, fmt.Errorf("chainsettle: select periods: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []periodRow
	for rows.Next() {
		var p periodRow
		var n int64
		if err := rows.Scan(&n, &p.TotalMicro, &p.ClaimableAt); err != nil {
			return nil, fmt.Errorf("chainsettle: scan period: %w", err)
		}
		p.Period = uint64(n)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *store) payoutsOf(ctx context.Context, period uint64) ([]payout, error) {
	n, err := toInt64(period)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT wallet, amount_micro FROM chain_period_payouts WHERE period = $1 ORDER BY wallet`, n)
	if err != nil {
		return nil, fmt.Errorf("chainsettle: select payouts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []payout
	for rows.Next() {
		var p payout
		var w []byte
		if err := rows.Scan(&w, &p.Micro); err != nil {
			return nil, fmt.Errorf("chainsettle: scan payout: %w", err)
		}
		copy(p.Wallet[:], w)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *store) debitsOf(ctx context.Context, period uint64) ([]Debit, error) {
	n, err := toInt64(period)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT wallet, amount_micro FROM chain_period_debits WHERE period = $1 ORDER BY wallet`, n)
	if err != nil {
		return nil, fmt.Errorf("chainsettle: select debits: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Debit
	for rows.Next() {
		var d Debit
		var w []byte
		if err := rows.Scan(&w, &d.Micro); err != nil {
			return nil, fmt.Errorf("chainsettle: scan debit: %w", err)
		}
		copy(d.Wallet[:], w)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *store) refOf(ctx context.Context, period uint64) ([32]byte, error) {
	var out [32]byte
	n, err := toInt64(period)
	if err != nil {
		return out, err
	}
	var b []byte
	if err := s.db.QueryRowContext(ctx, `SELECT ref FROM chain_periods WHERE period = $1`, n).Scan(&b); err != nil {
		return out, fmt.Errorf("chainsettle: read ref: %w", err)
	}
	copy(out[:], b)
	return out, nil
}

func (s *store) markPosted(ctx context.Context, period uint64, txHash *[32]byte, claimableAt time.Time) error {
	n, err := toInt64(period)
	if err != nil {
		return err
	}
	// An untyped nil, not a nil []byte: the driver would send the latter as an
	// empty value rather than NULL.
	var h any
	if txHash != nil {
		h = txHash[:]
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE chain_periods SET status = 'POSTED', post_tx = COALESCE($2, post_tx), claimable_at = $3,
       last_error = NULL, updated_at = NOW()
WHERE period = $1`, n, h, claimableAt)
	if err != nil {
		return fmt.Errorf("chainsettle: mark posted: %w", err)
	}
	return nil
}

func (s *store) markClaimed(ctx context.Context, period uint64, txHash [32]byte) error {
	n, err := toInt64(period)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE chain_periods SET status = 'CLAIMED', claim_tx = $2, last_error = NULL, updated_at = NOW()
WHERE period = $1`, n, txHash[:])
	if err != nil {
		return fmt.Errorf("chainsettle: mark claimed: %w", err)
	}
	return nil
}

// failPeriod abandons a period and frees its ledger releases to be batched
// into a later one.
func (s *store) failPeriod(ctx context.Context, period uint64, reason string) error {
	n, err := toInt64(period)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("chainsettle: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
UPDATE chain_periods SET status = 'FAILED', last_error = $2, updated_at = NOW() WHERE period = $1`,
		n, clip(reason)); err != nil {
		return fmt.Errorf("chainsettle: fail period: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM chain_period_items WHERE period = $1`, n); err != nil {
		return fmt.Errorf("chainsettle: free releases: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("chainsettle: commit: %w", err)
	}
	return nil
}

func (s *store) recordError(ctx context.Context, period uint64, msg string) {
	n, err := toInt64(period)
	if err != nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE chain_periods SET last_error = $2, updated_at = NOW() WHERE period = $1`,
		n, clip(msg))
}
