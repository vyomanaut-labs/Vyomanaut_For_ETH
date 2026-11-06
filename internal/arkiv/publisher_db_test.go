package arkiv

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	_ "github.com/lib/pq" // registers the "postgres" driver
)

// ── an isolated database per test (same pattern as internal/anchor) ───────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func dsn(user, pass, dbname string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		envOr("PGHOST", "localhost"), envOr("PGPORT", "5432"), user, pass, dbname, envOr("PGSSLMODE", "disable"))
}

func openOrSkip(t *testing.T, conn string) *sql.DB {
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

type testDB struct {
	app   *sql.DB // the coordinator's role
	admin *sql.DB // a superuser: builds fixtures directly
}

func newTestDB(t *testing.T) *testDB {
	t.Helper()
	adminUser := envOr("PGVERIFY_USER", "postgres")
	adminPass := os.Getenv("PGVERIFY_PASSWORD")
	root := openOrSkip(t, dsn(adminUser, adminPass, "postgres"))

	var r [6]byte
	rnd(r[:])
	name := fmt.Sprintf("arkiv_test_%x", r)
	if _, err := root.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Skipf("cannot create a scratch database: %v", err)
	}
	t.Cleanup(func() { _, _ = root.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`) })

	admin := openOrSkip(t, dsn(adminUser, adminPass, name))
	if _, err := admin.Exec(`CREATE EXTENSION IF NOT EXISTS btree_gist`); err != nil {
		t.Skipf("btree_gist unavailable: %v", err)
	}
	for _, f := range []string{"001_initial_schema_demo.sql", "003_arkiv.sql"} {
		sqlText, err := os.ReadFile(filepath.Join("..", "..", "migrations", f))
		if err != nil {
			t.Skipf("migration %s not found: %v", f, err)
		}
		if _, err := admin.Exec(string(sqlText)); err != nil {
			t.Skipf("cannot apply %s: %v", f, err)
		}
	}
	app := openOrSkip(t, dsn(envOr("PGUSER", "vyomanaut_app"), os.Getenv("PGPASSWORD"), name))
	return &testDB{app: app, admin: admin}
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func (d *testDB) newProvider(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := d.admin.Exec(`INSERT INTO providers (provider_id, phone_number, ed25519_public_key, status,
		declared_storage_gb, city, region, asn) VALUES ($1,$2,$3,'ACTIVE',50,'TestCity','TestRegion','SIM-AS1')`,
		id, fmt.Sprintf("+91%x", randBytes(5)), randBytes(32))
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	return id
}

func (d *testDB) addReceipt(t *testing.T, provider uuid.UUID, countersign time.Time, result string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	// A TIMEOUT has no response: no hash, latency or provider signature.
	var respHash, provSig, latency any
	if result != "TIMEOUT" {
		respHash, provSig, latency = randBytes(32), randBytes(64), 120
	}
	_, err := d.admin.Exec(`INSERT INTO audit_receipts (receipt_id, chunk_id, provider_id, challenge_nonce, server_challenge_ts,
		response_hash, response_latency_ms, audit_result, provider_sig, service_sig, service_countersign_ts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		id, randBytes(32), provider, randBytes(33), countersign.Add(-time.Second), respHash, latency,
		result, provSig, randBytes(64), countersign)
	if err != nil {
		t.Fatalf("insert receipt: %v", err)
	}
	return id
}

// ── a fake Arkiv ────────────────────────────────────────────────────────

type fakeCreator struct {
	mu       sync.Mutex
	created  [][]Attribute
	failNext error
}

func (f *fakeCreator) Create(_ context.Context, attrs []Attribute) (CreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return CreateResult{}, err
	}
	f.created = append(f.created, attrs)
	var key, tx common.Hash
	copy(key[:], randBytes(32))
	copy(tx[:], randBytes(32))
	return CreateResult{Key: key, TxHash: tx, GasUsed: 200_000, FeeWei: big.NewInt(2e14)}, nil
}

func (f *fakeCreator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created)
}

func statusCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	rows, err := db.Query(`SELECT status, COUNT(*) FROM arkiv_publications GROUP BY status`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			t.Fatal(err)
		}
		out[s] = n
	}
	return out
}

// ── tests ───────────────────────────────────────────────────────────────

func TestPublisherPublishesClosedPeriodsOnce(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()
	period := 2 * time.Minute
	start := PeriodStart(time.Now().UTC().Add(-time.Hour), period)

	p1, p2 := d.newProvider(t), d.newProvider(t)
	d.addReceipt(t, p1, start.Add(10*time.Second), "PASS")
	d.addReceipt(t, p1, start.Add(20*time.Second), "FAIL")
	d.addReceipt(t, p2, start.Add(30*time.Second), "TIMEOUT")
	d.addReceipt(t, p1, start.Add(period+5*time.Second), "PASS") // the next period

	now := start.Add(2*period + receiptleafSettle + time.Second)
	fc := &fakeCreator{}
	key := newKey(t)
	pub, err := NewPublisher(d.app, fc, key, PublisherConfig{
		Period: period, Now: func() time.Time { return now },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Pin the cursor at the first period (a fresh one would start at now).
	if _, err := d.admin.Exec(`INSERT INTO arkiv_cursors (stream, high_water) VALUES ('audit_receipts', $1)`, start); err != nil {
		t.Fatal(err)
	}

	pub.Step(ctx)
	if fc.count() != 3 {
		t.Fatalf("published %d entities, want 3 (p1 and p2 in period 0, p1 in period 1)", fc.count())
	}
	if got := statusCounts(t, d.app); got[StatusConfirmed] != 3 || len(got) != 1 {
		t.Fatalf("publication states: %v", got)
	}

	// Every published body verifies and names the coordinator key.
	for _, attrs := range fc.created {
		b, k, err := DecodeAuditBatch(bodyOf(t, attrs))
		if err != nil {
			t.Fatalf("published batch does not verify: %v", err)
		}
		if !k.Equal(key.Public()) || len(b.Receipts) == 0 {
			t.Fatal("wrong key or empty batch")
		}
	}

	// A second step finds nothing new.
	pub.Step(ctx)
	if fc.count() != 3 {
		t.Fatalf("a second step re-published: %d", fc.count())
	}
	var hw time.Time
	if err := d.app.QueryRow(`SELECT high_water FROM arkiv_cursors WHERE stream = 'audit_receipts'`).Scan(&hw); err != nil {
		t.Fatal(err)
	}
	if !hw.Equal(start.Add(2 * period)) {
		t.Fatalf("cursor at %s, want %s", hw, start.Add(2*period))
	}
}

func TestPublisherRetriesAfterFailure(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()
	period := 2 * time.Minute
	start := PeriodStart(time.Now().UTC().Add(-time.Hour), period)
	p := d.newProvider(t)
	d.addReceipt(t, p, start.Add(time.Second), "PASS")

	now := start.Add(period + receiptleafSettle + time.Second)
	fc := &fakeCreator{failNext: errors.New("rpc down")}
	pub, err := NewPublisher(d.app, fc, newKey(t), PublisherConfig{Period: period, Now: func() time.Time { return now }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.admin.Exec(`INSERT INTO arkiv_cursors (stream, high_water) VALUES ('audit_receipts', $1)`, start); err != nil {
		t.Fatal(err)
	}

	pub.Step(ctx)
	if got := statusCounts(t, d.app); got[StatusFailed] != 1 {
		t.Fatalf("after a failed send: %v", got)
	}
	// Still inside the backoff: nothing is sent.
	pub.Step(ctx)
	if fc.count() != 0 {
		t.Fatal("retried inside the backoff window")
	}
	// Age the row past its backoff, then it is sent once.
	if _, err := d.admin.Exec(`UPDATE arkiv_publications SET updated_at = NOW() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	pub.Step(ctx)
	if fc.count() != 1 {
		t.Fatalf("published %d after the backoff, want 1", fc.count())
	}
	var status string
	var attempts int
	if err := d.app.QueryRow(`SELECT status, attempts FROM arkiv_publications`).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != StatusConfirmed || attempts != 2 {
		t.Fatalf("row is %s after %d attempts, want CONFIRMED after 2", status, attempts)
	}
}

func TestPublisherSkipsEmptyPeriods(t *testing.T) {
	d := newTestDB(t)
	period := 2 * time.Minute
	start := PeriodStart(time.Now().UTC().Add(-time.Hour), period)
	now := start.Add(5*period + receiptleafSettle)
	fc := &fakeCreator{}
	pub, err := NewPublisher(d.app, fc, newKey(t), PublisherConfig{Period: period, Now: func() time.Time { return now }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.admin.Exec(`INSERT INTO arkiv_cursors (stream, high_water) VALUES ('audit_receipts', $1)`, start); err != nil {
		t.Fatal(err)
	}
	pub.Step(context.Background())
	if fc.count() != 0 || len(statusCounts(t, d.app)) != 0 {
		t.Fatal("empty periods must not create entities or rows")
	}
	var hw time.Time
	_ = d.app.QueryRow(`SELECT high_water FROM arkiv_cursors WHERE stream = 'audit_receipts'`).Scan(&hw)
	if !hw.Equal(start.Add(5 * period)) {
		t.Fatalf("cursor at %s, want %s", hw, start.Add(5*period))
	}
}

const receiptleafSettle = 30 * time.Second

func TestTamperedReceiptIsDetected(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()
	period := 2 * time.Minute
	start := PeriodStart(time.Now().UTC().Add(-time.Hour), period)
	p := d.newProvider(t)
	d.addReceipt(t, p, start.Add(time.Second), "PASS")
	victim := d.addReceipt(t, p, start.Add(2*time.Second), "PASS")

	now := start.Add(period + receiptleafSettle + time.Second)
	fc := &fakeCreator{}
	key := newKey(t)
	pub, err := NewPublisher(d.app, fc, key, PublisherConfig{Period: period, Now: func() time.Time { return now }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.admin.Exec(`INSERT INTO arkiv_cursors (stream, high_water) VALUES ('audit_receipts', $1)`, start); err != nil {
		t.Fatal(err)
	}
	pub.Step(ctx)
	if fc.count() != 1 {
		t.Fatalf("published %d", fc.count())
	}
	published, _, err := DecodeAuditBatch(bodyOf(t, fc.created[0]))
	if err != nil {
		t.Fatal(err)
	}
	if err := CompareBatchWithDB(ctx, d.app, published); err != nil {
		t.Fatalf("an untouched database must match: %v", err)
	}

	if _, err := d.admin.Exec(`UPDATE audit_receipts SET audit_result = 'FAIL' WHERE receipt_id = $1`, victim); err != nil {
		t.Fatal(err)
	}
	err = CompareBatchWithDB(ctx, d.app, published)
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("tampering was not detected: %v", err)
	}
	if !strings.Contains(err.Error(), victim.String()) || !strings.Contains(err.Error(), "audit_result") {
		t.Fatalf("the error should name the receipt and the field: %v", err)
	}
}

func (d *testDB) newFile(t *testing.T, segments, shards int) uuid.UUID {
	t.Helper()
	owner := uuid.New()
	if _, err := d.admin.Exec(`INSERT INTO owners (owner_id, phone_number, ed25519_public_key) VALUES ($1,$2,$3)`,
		owner, fmt.Sprintf("+91%x", randBytes(5)), randBytes(32)); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	file := uuid.New()
	if _, err := d.admin.Exec(`INSERT INTO files (file_id, owner_id, pointer_ciphertext, pointer_nonce, pointer_tag, original_size_bytes, uploaded_at)
		VALUES ($1,$2,$3,$4,$5,1000, NOW() - interval '10 minutes')`, file, owner, randBytes(200), randBytes(12), randBytes(16)); err != nil {
		t.Fatalf("insert file: %v", err)
	}
	prov := d.newProvider(t)
	for s := 0; s < segments; s++ {
		var seg uuid.UUID
		if err := d.admin.QueryRow(`INSERT INTO segments (file_id, segment_index) VALUES ($1,$2) RETURNING segment_id`, file, s).Scan(&seg); err != nil {
			t.Fatalf("insert segment: %v", err)
		}
		for sh := 0; sh < shards; sh++ {
			if _, err := d.admin.Exec(`INSERT INTO chunk_assignments (chunk_id, segment_id, shard_index, provider_id) VALUES ($1,$2,$3,$4)`,
				randBytes(32), seg, sh, prov); err != nil {
				t.Fatalf("insert chunk: %v", err)
			}
		}
	}
	return file
}

func TestManifestsAndTombstones(t *testing.T) {
	d := newTestDB(t)
	ctx := context.Background()
	file := d.newFile(t, 2, 5)

	fc := &fakeCreator{}
	key := newKey(t)
	pub, err := NewPublisher(d.app, fc, key, PublisherConfig{
		Manifests: true, RSK: 3, RSN: 5, Backfill: time.Hour,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pub.Step(ctx)
	var manifest []Attribute
	for _, a := range fc.created {
		if s, _ := attrStr(a, AttrKind); s == KindFileManifest {
			manifest = a
		}
	}
	if manifest == nil {
		t.Fatalf("no manifest among %d entities", fc.count())
	}
	m, pk, err := DecodeFileManifest(bodyOf(t, manifest))
	if err != nil || !pk.Equal(key.Public()) {
		t.Fatalf("manifest does not verify: %v", err)
	}
	if m.FileID != [16]byte(file) || m.Segments != 2 || m.Shards != 10 || m.RSK != 3 || m.RSN != 5 {
		t.Fatalf("manifest = %+v", m)
	}
	if err := CompareManifestWithDB(ctx, d.app, m); err != nil {
		t.Fatalf("an untouched database must match: %v", err)
	}

	// Deleting the file queues a tombstone, once.
	if _, err := d.admin.Exec(`UPDATE files SET status = 'DELETED' WHERE file_id = $1`, file); err != nil {
		t.Fatal(err)
	}
	before := fc.count()
	pub.Step(ctx)
	pub.Step(ctx)
	if fc.count() != before+1 {
		t.Fatalf("deleting a file published %d entities, want 1", fc.count()-before)
	}
	id, _, _, err := DecodeTombstone(bodyOf(t, fc.created[fc.count()-1]))
	if err != nil || id != [16]byte(file) {
		t.Fatalf("tombstone: %v", err)
	}
}

func attrStr(attrs []Attribute, name string) (string, bool) {
	for _, a := range attrs {
		if a.NameString() == name {
			return string(a.Value), true
		}
	}
	return "", false
}
