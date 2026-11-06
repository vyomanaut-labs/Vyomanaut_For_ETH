package arkiv

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// Verification, used by cmd/arkivcheck. Every check returns a plain-language
// reason on failure so the tool can print it as is.

// ErrMismatch means Arkiv and PostgreSQL disagree.
var ErrMismatch = errors.New("arkiv: mirror and database disagree")

// EntityAttrNames is every attribute a verifier asks arkiv_query for.
var EntityAttrNames = []string{
	AttrApp, AttrKind, AttrSchema, AttrProvider, AttrPeriod, AttrPeriodEnd, AttrSeq, AttrCount,
	AttrPass, AttrFail, AttrTimeout, AttrRoot, AttrCoordKey, AttrCoordSig, AttrBody,
	AttrFileID, AttrPointerHash, AttrShardRoot, AttrSegments, AttrShards, AttrRSK, AttrRSN,
	AttrRegistered, AttrDeleted,
}

// ByKey fetches one entity by its key, with every attribute. This is the
// query form verified on chain in spike A0.
func (c *Client) ByKey(ctx context.Context, key common.Hash) (Entity, error) {
	rows, err := c.Query(ctx, fmt.Sprintf("$key = key(%s)", key.Hex()), EntityAttrNames, 1, 0)
	if err != nil {
		return Entity{}, err
	}
	if len(rows) == 0 {
		return Entity{}, fmt.Errorf("arkiv: entity %s not found (expired, or not yet indexed)", key.Hex())
	}
	return rows[0], nil
}

// KindQuery is the arkiv_query string that lists one kind of entity for one
// app tag. NOT yet verified on chain (spike A0 only queried by key).
func KindQuery(appTag, kind string) string {
	return fmt.Sprintf("app = str('%s') AND kind = str('%s')", quoteFree(appTag), quoteFree(kind))
}

func quoteFree(s string) string { return strings.NewReplacer("'", "", `\`, "").Replace(s) }

// CheckAuditEntity checks one audit batch entity on its own: the body
// verifies (order, root, signature), the signature is by the trusted key when
// one is given, and the indexed attributes say the same as the body.
func CheckAuditEntity(e Entity, trusted ed25519.PublicKey) (AuditBatch, ed25519.PublicKey, error) {
	if kind, _ := e.Str(AttrKind); kind != KindAuditBatch {
		return AuditBatch{}, nil, fmt.Errorf("%w: kind is %q", ErrBadBatch, kind)
	}
	body, err := e.Bytes(AttrBody)
	if err != nil {
		return AuditBatch{}, nil, fmt.Errorf("%w: %v", ErrBadBatch, err)
	}
	b, pub, err := DecodeAuditBatch(body)
	if err != nil {
		return b, nil, err
	}
	if trusted != nil && !pub.Equal(trusted) {
		return b, pub, fmt.Errorf("%w: signed by %x, not by the trusted coordinator key", ErrBadBatch, []byte(pub))
	}
	want := map[string]uint64{
		AttrPeriod: uint64(b.PeriodStart.Unix()), AttrSeq: uint64(b.Seq), AttrCount: uint64(len(b.Receipts)),
		AttrPass: uint64(b.Pass), AttrFail: uint64(b.Fail), AttrTimeout: uint64(b.Timeout),
	}
	for name, v := range want {
		got, err := e.U64(name)
		if err != nil || got != v {
			return b, pub, fmt.Errorf("%w: attribute %s says %d, body says %d", ErrBadBatch, name, got, v)
		}
	}
	if s, _ := e.Str(AttrRoot); s != hex.EncodeToString(b.Root[:]) {
		return b, pub, fmt.Errorf("%w: merkle_root attribute differs from the body", ErrBadBatch)
	}
	if s, _ := e.Str(AttrProvider); s != receiptleaf.FormatUUID(b.ProviderID) {
		return b, pub, fmt.Errorf("%w: provider_id attribute differs from the body", ErrBadBatch)
	}
	return b, pub, nil
}

// CompareBatchWithDB rebuilds the batch from PostgreSQL and compares it with
// the published one. On a mismatch the error names the receipts that differ.
func CompareBatchWithDB(ctx context.Context, db *sql.DB, published AuditBatch) error {
	st := store{db: db}
	rs, err := st.receipts(ctx, published.PeriodStart, published.PeriodEnd, &published.ProviderID)
	if err != nil {
		return err
	}
	batches, err := BuildAuditBatches(rs, published.PeriodStart, published.PeriodEnd)
	if err != nil {
		return err
	}
	var local *AuditBatch
	for i := range batches {
		if batches[i].Seq == published.Seq {
			local = &batches[i]
		}
	}
	if local == nil {
		return fmt.Errorf("%w: the database has no receipts for this batch any more", ErrMismatch)
	}
	if local.Root == published.Root {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrMismatch, describeReceiptDiff(published.Receipts, local.Receipts))
}

func describeReceiptDiff(published, local []receiptleaf.Receipt) string {
	pub := map[[16]byte][]byte{}
	for _, r := range published {
		enc, _ := r.Encode()
		pub[r.ReceiptID] = enc
	}
	var changed, added []string
	seen := map[[16]byte]bool{}
	for _, r := range local {
		seen[r.ReceiptID] = true
		enc, _ := r.Encode()
		old, ok := pub[r.ReceiptID]
		switch {
		case !ok:
			added = append(added, receiptleaf.FormatUUID(r.ReceiptID))
		case !bytes.Equal(old, enc):
			changed = append(changed, receiptleaf.FormatUUID(r.ReceiptID)+fieldDiff(old, enc))
		}
	}
	var removed []string
	for _, r := range published {
		if !seen[r.ReceiptID] {
			removed = append(removed, receiptleaf.FormatUUID(r.ReceiptID))
		}
	}
	var parts []string
	if len(changed) > 0 {
		parts = append(parts, "changed in Postgres: "+strings.Join(changed, ", "))
	}
	if len(added) > 0 {
		parts = append(parts, "only in Postgres: "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		parts = append(parts, "missing from Postgres: "+strings.Join(removed, ", "))
	}
	if len(parts) == 0 {
		return "the Merkle roots differ"
	}
	return strings.Join(parts, "; ")
}

// fieldDiff names the receipt fields that differ between two encodings.
func fieldDiff(a, b []byte) string {
	fields := []struct {
		name     string
		from, to int
	}{
		{"chunk_id", 17, 49}, {"provider_id", 49, 65}, {"challenge_nonce", 65, 98},
		{"server_challenge_ts", 98, 106}, {"service_countersign_ts", 106, 114}, {"audit_result", 114, 115},
		{"response_hash", 115, 147}, {"flags", 147, 148}, {"response_latency_ms", 148, 152},
		{"provider_sig", 152, 216}, {"service_sig", 216, 280},
	}
	var names []string
	for _, f := range fields {
		if f.to <= len(a) && f.to <= len(b) && !bytes.Equal(a[f.from:f.to], b[f.from:f.to]) {
			names = append(names, f.name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " (" + strings.Join(names, ", ") + ")"
}

// CheckManifestEntity checks a manifest entity on its own and returns it.
func CheckManifestEntity(e Entity, trusted ed25519.PublicKey) (FileManifest, error) {
	body, err := e.Bytes(AttrBody)
	if err != nil {
		return FileManifest{}, fmt.Errorf("%w: %v", ErrBadManifest, err)
	}
	m, pub, err := DecodeFileManifest(body)
	if err != nil {
		return m, err
	}
	if trusted != nil && !pub.Equal(trusted) {
		return m, fmt.Errorf("%w: not signed by the trusted coordinator key", ErrBadManifest)
	}
	if s, _ := e.Str(AttrShardRoot); s != hex.EncodeToString(m.ShardRoot[:]) {
		return m, fmt.Errorf("%w: shard_root attribute differs from the body", ErrBadManifest)
	}
	return m, nil
}

// CompareManifestWithDB rebuilds a manifest from PostgreSQL and compares.
func CompareManifestWithDB(ctx context.Context, db *sql.DB, published FileManifest) error {
	local, err := (&store{db: db}).manifest(ctx, receiptleaf.FormatUUID(published.FileID), published.RSK, published.RSN)
	if err != nil {
		return err
	}
	switch {
	case local.PointerSHA256 != published.PointerSHA256:
		return fmt.Errorf("%w: the stored pointer changed", ErrMismatch)
	case local.ShardRoot != published.ShardRoot || local.Shards != published.Shards || local.Segments != published.Segments:
		return fmt.Errorf("%w: the file's shards changed", ErrMismatch)
	case !local.RegisteredAt.Equal(published.RegisteredAt):
		return fmt.Errorf("%w: the registration time changed", ErrMismatch)
	}
	return nil
}

// PublishedRow is one confirmed publication recorded in PostgreSQL.
type PublishedRow struct {
	Kind      string
	Reference string
	EntityKey common.Hash
	TxHash    common.Hash
	CreatedAt time.Time
}

// ConfirmedPublications lists confirmed publications, newest first. kind ""
// means every kind.
func ConfirmedPublications(ctx context.Context, db *sql.DB, kind string, limit int) ([]PublishedRow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT kind, reference, entity_key, tx_hash, created_at FROM arkiv_publications
WHERE status = 'CONFIRMED' AND ($1 = '' OR kind = $1)
ORDER BY created_at DESC LIMIT $2`, kind, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PublishedRow
	for rows.Next() {
		var r PublishedRow
		var key, tx []byte
		if err := rows.Scan(&r.Kind, &r.Reference, &key, &tx, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.EntityKey, r.TxHash = common.BytesToHash(key), common.BytesToHash(tx)
		out = append(out, r)
	}
	return out, rows.Err()
}

// FileDeleted reports whether PostgreSQL has the file as DELETED.
func FileDeleted(ctx context.Context, db *sql.DB, fileID [16]byte) (bool, error) {
	var status string
	err := db.QueryRowContext(ctx, `SELECT status::text FROM files WHERE file_id = $1::uuid`,
		receiptleaf.FormatUUID(fileID)).Scan(&status)
	return status == "DELETED", err
}
