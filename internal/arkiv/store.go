package arkiv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// Cursor streams (arkiv_cursors.stream).
const (
	StreamAuditReceipts   = "audit_receipts"
	StreamFilesRegistered = "files_registered"
)

// Publication states (arkiv_publications.status).
const (
	StatusSent      = "SENT"
	StatusConfirmed = "CONFIRMED"
	StatusFailed    = "FAILED"
)

const lastErrorMax = 500

// publication is one row of arkiv_publications that still needs sending.
type publication struct {
	ID         string
	Kind       string
	Reference  string
	Status     string
	Attempts   int
	RangeStart sql.NullTime
	RangeEnd   sql.NullTime
	CreatedAt  time.Time
}

// store is everything the publisher reads or writes in PostgreSQL. It runs on
// the coordinator's vyomanaut_app pool and never touches audit_receipts or
// files except to read them.
type store struct{ db *sql.DB }

// cursor returns a stream's high-water mark.
func (s *store) cursor(ctx context.Context, stream string) (time.Time, bool, error) {
	var t time.Time
	err := s.db.QueryRowContext(ctx, `SELECT high_water FROM arkiv_cursors WHERE stream = $1`, stream).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	return t.UTC(), err == nil, err
}

// initCursor creates a stream's cursor if it does not exist yet and returns
// the stored value (which may be an older one written by another start).
func (s *store) initCursor(ctx context.Context, stream string, at time.Time) (time.Time, error) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO arkiv_cursors (stream, high_water) VALUES ($1, $2) ON CONFLICT (stream) DO NOTHING`,
		stream, at); err != nil {
		return time.Time{}, err
	}
	t, _, err := s.cursor(ctx, stream)
	return t, err
}

// receipts reads finished receipts with service_countersign_ts in
// [from, to), optionally for one provider, in the canonical order.
func (s *store) receipts(ctx context.Context, from, to time.Time, provider *[16]byte) ([]receiptleaf.Receipt, error) {
	q := `SELECT ` + receiptleaf.SelectColumns + ` FROM audit_receipts WHERE ` + receiptleaf.FinalFilter +
		` AND service_countersign_ts >= $1 AND service_countersign_ts < $2`
	args := []any{from, to}
	if provider != nil {
		q += ` AND provider_id = $3::uuid`
		args = append(args, receiptleaf.FormatUUID(*provider))
	}
	q += ` ORDER BY ` + receiptleaf.CanonicalOrder
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

// reserveBatches records the batches of one period as SENT and moves the
// audit cursor to the period's end, in one transaction: either both happen or
// neither does, so a period is never skipped and never reserved twice.
func (s *store) reserveBatches(ctx context.Context, batches []AuditBatch, periodEnd time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	n := 0
	for _, b := range batches {
		res, err := tx.ExecContext(ctx, `
INSERT INTO arkiv_publications (kind, reference, status, range_start, range_end, item_count)
VALUES ($1, $2, 'SENT', $3, $4, $5)
ON CONFLICT (kind, reference) DO NOTHING`,
			KindAuditBatch, b.Reference(), b.PeriodStart, b.PeriodEnd, len(b.Receipts))
		if err != nil {
			return 0, err
		}
		if k, _ := res.RowsAffected(); k > 0 {
			n++
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE arkiv_cursors SET high_water = $2, updated_at = NOW() WHERE stream = $1`,
		StreamAuditReceipts, periodEnd); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// registeredFiles returns registered files with uploaded_at in [from, to],
// oldest first.
func (s *store) registeredFiles(ctx context.Context, from, to time.Time, limit int) ([]string, time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT file_id::text, uploaded_at FROM files
WHERE octet_length(pointer_ciphertext) > 0 AND uploaded_at >= $1 AND uploaded_at <= $2
ORDER BY uploaded_at, file_id LIMIT $3`, from, to, limit)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	var last time.Time
	for rows.Next() {
		var id string
		if err := rows.Scan(&id, &last); err != nil {
			return nil, time.Time{}, err
		}
		ids = append(ids, id)
	}
	return ids, last, rows.Err()
}

// reserveManifests records manifests as SENT and moves the files cursor.
func (s *store) reserveManifests(ctx context.Context, fileIDs []string, highWater time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	n := 0
	for _, id := range fileIDs {
		res, err := tx.ExecContext(ctx, `
INSERT INTO arkiv_publications (kind, reference, status) VALUES ($1, $2, 'SENT')
ON CONFLICT (kind, reference) DO NOTHING`, KindFileManifest, id)
		if err != nil {
			return 0, err
		}
		if k, _ := res.RowsAffected(); k > 0 {
			n++
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE arkiv_cursors SET high_water = $2, updated_at = NOW() WHERE stream = $1`,
		StreamFilesRegistered, highWater); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// reserveTombstones records a tombstone for every deleted file that has a
// manifest and no tombstone yet. The row's created_at is the time the
// deletion was seen.
func (s *store) reserveTombstones(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO arkiv_publications (kind, reference, status)
SELECT $1, f.file_id::text, 'SENT' FROM files f
WHERE f.status = 'DELETED'
  AND EXISTS (SELECT 1 FROM arkiv_publications m WHERE m.kind = $2 AND m.reference = f.file_id::text)
ON CONFLICT (kind, reference) DO NOTHING`, KindFileTombstone, KindFileManifest)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// manifest reads what a file manifest publishes.
func (s *store) manifest(ctx context.Context, fileID string, rsK, rsN uint8) (FileManifest, error) {
	var m FileManifest
	id, err := receiptleaf.ParseUUID(fileID)
	if err != nil {
		return m, err
	}
	m.FileID, m.RSK, m.RSN = id, rsK, rsN
	var nonce, ct, tag []byte
	if err := s.db.QueryRowContext(ctx,
		`SELECT pointer_nonce, pointer_ciphertext, pointer_tag, uploaded_at FROM files WHERE file_id = $1::uuid`,
		fileID).Scan(&nonce, &ct, &tag, &m.RegisteredAt); err != nil {
		return m, err
	}
	m.RegisteredAt = m.RegisteredAt.UTC()
	m.PointerSHA256 = PointerHash(nonce, ct, tag)

	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT ON (s.segment_index, ca.shard_index) s.segment_index, ca.shard_index, ca.chunk_id
FROM segments s JOIN chunk_assignments ca ON ca.segment_id = s.segment_id
WHERE s.file_id = $1::uuid AND NOT ca.is_vetting_chunk
ORDER BY s.segment_index, ca.shard_index, ca.created_at`, fileID)
	if err != nil {
		return m, err
	}
	defer func() { _ = rows.Close() }()
	var shards []Shard
	segs := map[uint32]bool{}
	for rows.Next() {
		var seg, shard int64
		var chunk []byte
		if err := rows.Scan(&seg, &shard, &chunk); err != nil {
			return m, err
		}
		if len(chunk) != len(Shard{}.ChunkID) || seg < 0 || shard < 0 {
			return m, fmt.Errorf("arkiv: file %s has a malformed shard row", fileID)
		}
		sh := Shard{SegmentIndex: uint32(seg), ShardIndex: uint16(shard)}
		copy(sh.ChunkID[:], chunk)
		shards = append(shards, sh)
		segs[sh.SegmentIndex] = true
	}
	if err := rows.Err(); err != nil {
		return m, err
	}
	m.Segments, m.Shards = uint32(len(segs)), uint32(len(shards))
	m.ShardRoot = ShardRoot(shards)
	return m, nil
}

// unfinished returns rows still to send: SENT rows (new, or left over by a
// crash) and FAILED rows whose backoff has passed and that have attempts left.
func (s *store) unfinished(ctx context.Context, maxAttempts, limit int) ([]publication, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT publication_id::text, kind, reference, status, attempts, range_start, range_end, created_at
FROM arkiv_publications
WHERE status = 'SENT'
   OR (status = 'FAILED' AND attempts < $1
       AND updated_at <= NOW() - make_interval(secs => LEAST(600, 30 * power(2, attempts - 1))))
ORDER BY created_at
LIMIT $2`, maxAttempts, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []publication
	for rows.Next() {
		var p publication
		if err := rows.Scan(&p.ID, &p.Kind, &p.Reference, &p.Status, &p.Attempts,
			&p.RangeStart, &p.RangeEnd, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.CreatedAt = p.CreatedAt.UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// markRetry flips a FAILED row back to SENT before it is sent again.
func (s *store) markRetry(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE arkiv_publications SET status = 'SENT', attempts = attempts + 1, updated_at = NOW()
WHERE publication_id = $1::uuid`, id)
	return err
}

func (s *store) markConfirmed(ctx context.Context, id string, r CreateResult, items, payload int) error {
	fee := "0"
	if r.FeeWei != nil {
		fee = new(big.Int).Set(r.FeeWei).String()
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE arkiv_publications
SET status = 'CONFIRMED', entity_key = $2, tx_hash = $3, gas_used = $4, fee_wei = $5::numeric,
    item_count = $6, payload_bytes = $7, last_error = NULL, updated_at = NOW()
WHERE publication_id = $1::uuid`,
		id, r.Key.Bytes(), r.TxHash.Bytes(), int64(r.GasUsed), fee, items, nullPositive(payload))
	return err
}

// markFailed records an error. giveUp sets attempts to the limit so the row is
// never retried (used when the item can no longer be rebuilt).
func (s *store) markFailed(ctx context.Context, id string, cause error, txHash []byte, giveUp bool, maxAttempts int) error {
	msg := cause.Error()
	if len(msg) > lastErrorMax {
		msg = msg[:lastErrorMax]
	}
	var txArg any // untyped nil: lib/pq would send a typed nil slice as '' and fail the length check
	if len(txHash) > 0 {
		txArg = txHash
	}
	q := `UPDATE arkiv_publications SET status = 'FAILED', last_error = $2, tx_hash = COALESCE($3, tx_hash), updated_at = NOW()`
	args := []any{id, msg, txArg}
	if giveUp {
		q += `, attempts = GREATEST(attempts, $4)`
		args = append(args, maxAttempts)
	}
	_, err := s.db.ExecContext(ctx, q+` WHERE publication_id = $1::uuid`, args...)
	return err
}

func nullPositive(n int) any {
	if n <= 0 {
		return nil
	}
	return n
}

// counts returns how many rows are not yet confirmed.
func (s *store) pendingCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM arkiv_publications WHERE status <> 'CONFIRMED'`).Scan(&n)
	return n, err
}
