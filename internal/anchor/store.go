package anchor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// Row states of chain_anchors.
const (
	stateReserved = "RESERVED"
	stateAnchored = "ANCHORED"
	stateFailed   = "FAILED"
)

// position is a place in the canonical receipt order.
type position struct {
	TS time.Time
	ID string // receipt UUID text
}

// epochRow is one row of chain_anchors.
type epochRow struct {
	Epoch     uint64
	State     string
	Root      [32]byte
	Leaves    int
	First     time.Time
	Last      position
	AnchorTx  []byte // nil until anchored
	LastError string
}

const rowColumns = `epoch, status, root, leaf_count, first_countersign_ts, last_countersign_ts,
	last_receipt_id::text, anchor_tx, COALESCE(last_error, '')`

type rowScanner interface{ Scan(dest ...any) error }

func scanRow(s rowScanner) (epochRow, error) {
	var (
		r    epochRow
		epoc int64
		root []byte
		n    int64
	)
	if err := s.Scan(&epoc, &r.State, &root, &n, &r.First, &r.Last.TS, &r.Last.ID, &r.AnchorTx, &r.LastError); err != nil {
		return epochRow{}, err
	}
	if len(root) != len(r.Root) {
		return epochRow{}, fmt.Errorf("anchor: stored root of epoch %d is %d bytes", epoc, len(root))
	}
	r.Epoch = uint64(epoc)
	copy(r.Root[:], root)
	r.Leaves = int(n)
	return r, nil
}

// store is everything the package reads or writes in PostgreSQL.
type store struct{ db *sql.DB }

// cursor returns the end of the newest anchored epoch, or found=false when
// nothing has been anchored yet.
func (s *store) cursor(ctx context.Context) (p position, found bool, err error) {
	err = s.db.QueryRowContext(ctx, `
SELECT last_countersign_ts, last_receipt_id::text FROM chain_anchors
WHERE status = 'ANCHORED' ORDER BY epoch DESC LIMIT 1`).Scan(&p.TS, &p.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return position{}, false, nil
	}
	return p, err == nil, err
}

func (s *store) maxEpoch(ctx context.Context) (uint64, error) {
	var n sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(epoch) FROM chain_anchors`).Scan(&n); err != nil {
		return 0, err
	}
	return uint64(n.Int64), nil
}

// reserved returns the epoch waiting to be sent, if any.
func (s *store) reserved(ctx context.Context) (epochRow, bool, error) {
	r, err := scanRow(s.db.QueryRowContext(ctx,
		`SELECT `+rowColumns+` FROM chain_anchors WHERE status = 'RESERVED' ORDER BY epoch LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return epochRow{}, false, nil
	}
	return r, err == nil, err
}

// byEpoch returns one epoch's row.
func (s *store) byEpoch(ctx context.Context, epoch uint64) (epochRow, bool, error) {
	r, err := scanRow(s.db.QueryRowContext(ctx,
		`SELECT `+rowColumns+` FROM chain_anchors WHERE epoch = $1`, int64(epoch)))
	if errors.Is(err, sql.ErrNoRows) {
		return epochRow{}, false, nil
	}
	return r, err == nil, err
}

// latestAnchored returns the newest anchored epoch's row.
func (s *store) latestAnchored(ctx context.Context) (epochRow, bool, error) {
	r, err := scanRow(s.db.QueryRowContext(ctx,
		`SELECT `+rowColumns+` FROM chain_anchors WHERE status = 'ANCHORED' ORDER BY epoch DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return epochRow{}, false, nil
	}
	return r, err == nil, err
}

// list returns the newest rows first.
func (s *store) list(ctx context.Context, limit int) ([]epochRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+rowColumns+` FROM chain_anchors ORDER BY epoch DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []epochRow
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// previousEnd returns where the epoch before this one ended: the end of the
// newest anchored epoch with a smaller number.
func (s *store) previousEnd(ctx context.Context, epoch uint64) (position, bool, error) {
	var p position
	err := s.db.QueryRowContext(ctx, `
SELECT last_countersign_ts, last_receipt_id::text FROM chain_anchors
WHERE status = 'ANCHORED' AND epoch < $1 ORDER BY epoch DESC LIMIT 1`, int64(epoch)).Scan(&p.TS, &p.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return position{}, false, nil
	}
	return p, err == nil, err
}

// receipts reads finished receipts after `after` (when given), up to and
// including `upTo` (when given), no newer than `olderThan` (when non-zero), in
// the canonical order. limit 0 means no limit.
func (s *store) receipts(ctx context.Context, after *position, upTo *position, olderThan time.Time, limit int) ([]receiptleaf.Receipt, error) {
	q := `SELECT ` + receiptleaf.SelectColumns + ` FROM audit_receipts WHERE ` + receiptleaf.FinalFilter
	var args []any
	add := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if after != nil {
		q += " AND (service_countersign_ts, receipt_id) > (" + add(after.TS) + ", " + add(after.ID) + "::uuid)"
	}
	if upTo != nil {
		q += " AND (service_countersign_ts, receipt_id) <= (" + add(upTo.TS) + ", " + add(upTo.ID) + "::uuid)"
	}
	if !olderThan.IsZero() {
		q += " AND service_countersign_ts <= " + add(olderThan)
	}
	q += " ORDER BY " + receiptleaf.CanonicalOrder
	if limit > 0 {
		q += " LIMIT " + add(limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []receiptleaf.Receipt
	for rows.Next() {
		r, err := receiptleaf.ScanReceipt(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *store) reserve(ctx context.Context, r epochRow) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO chain_anchors
  (epoch, status, root, leaf_count, first_countersign_ts, last_countersign_ts, last_receipt_id)
VALUES ($1, 'RESERVED', $2, $3, $4, $5, $6::uuid)`,
		int64(r.Epoch), r.Root[:], int64(r.Leaves), r.First, r.Last.TS, r.Last.ID)
	return err
}

func (s *store) markAnchored(ctx context.Context, epoch uint64, tx []byte) error {
	// An untyped nil, not a typed nil slice: lib/pq sends the latter as an empty
	// value instead of NULL, which the length check on anchor_tx rejects.
	var txArg any
	if len(tx) > 0 {
		txArg = tx
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE chain_anchors SET status = 'ANCHORED', anchor_tx = $2, last_error = NULL, updated_at = NOW()
WHERE epoch = $1 AND status = 'RESERVED'`, int64(epoch), txArg)
	return err
}

func (s *store) markFailed(ctx context.Context, epoch uint64, reason string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE chain_anchors SET status = 'FAILED', last_error = $2, updated_at = NOW()
WHERE epoch = $1 AND status = 'RESERVED'`, int64(epoch), reason)
	return err
}

func (s *store) recordError(ctx context.Context, epoch uint64, msg string) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE chain_anchors SET last_error = $2, updated_at = NOW() WHERE epoch = $1 AND status = 'RESERVED'`,
		int64(epoch), msg)
}

// receiptPosition returns where a finished receipt sits in the canonical order.
func (s *store) receiptPosition(ctx context.Context, id string) (position, bool, error) {
	p := position{ID: id}
	err := s.db.QueryRowContext(ctx,
		`SELECT service_countersign_ts FROM audit_receipts WHERE receipt_id = $1::uuid AND `+receiptleaf.FinalFilter+` LIMIT 1`,
		id).Scan(&p.TS)
	if errors.Is(err, sql.ErrNoRows) {
		return position{}, false, nil
	}
	return p, err == nil, err
}

// epochContaining returns the first anchored epoch that ends at or after p.
func (s *store) epochContaining(ctx context.Context, p position) (epochRow, bool, error) {
	r, err := scanRow(s.db.QueryRowContext(ctx, `SELECT `+rowColumns+` FROM chain_anchors
WHERE status = 'ANCHORED' AND (last_countersign_ts, last_receipt_id) >= ($1, $2::uuid)
ORDER BY epoch LIMIT 1`, p.TS, p.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return epochRow{}, false, nil
	}
	return r, err == nil, err
}
