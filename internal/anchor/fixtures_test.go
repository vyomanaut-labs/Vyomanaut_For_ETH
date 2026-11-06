package anchor

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq" // registers the "postgres" driver

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// ── an isolated database per test ────────────────────────────────────────
//
// Every test builds its own throwaway database from the checked-in schema, so
// nothing here can disturb (or be disturbed by) other packages' tests that
// share the CI database.

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
	admin *sql.DB // a superuser: writes and tampers with receipts directly
}

func newTestDB(t *testing.T) *testDB {
	t.Helper()
	adminUser := envOr("PGVERIFY_USER", "postgres")
	adminPass := os.Getenv("PGVERIFY_PASSWORD")
	root := openOrSkip(t, dsn(adminUser, adminPass, "postgres"))

	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	name := fmt.Sprintf("anchor_test_%x", rnd)
	if _, err := root.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Skipf("cannot create a scratch database: %v", err)
	}
	t.Cleanup(func() { _, _ = root.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`) })

	admin := openOrSkip(t, dsn(adminUser, adminPass, name))
	if _, err := admin.Exec(`CREATE EXTENSION IF NOT EXISTS btree_gist`); err != nil {
		t.Skipf("btree_gist unavailable: %v", err)
	}
	for _, f := range []string{"001_initial_schema_demo.sql", "002_whitechain.sql"} {
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

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// addReceipt writes one receipt directly (as the superuser, so the append-only
// policies do not get in the way of building fixtures). A zero countersign time
// leaves the receipt PENDING.
func (d *testDB) addReceipt(t *testing.T, provider uuid.UUID, countersign time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	challenge := time.Now().UTC().Add(-time.Hour)
	if countersign.IsZero() {
		_, err := d.admin.Exec(`INSERT INTO audit_receipts (receipt_id, chunk_id, provider_id, challenge_nonce, server_challenge_ts)
			VALUES ($1,$2,$3,$4,$5)`, id, randBytes(32), provider, randBytes(33), challenge)
		if err != nil {
			t.Fatalf("insert pending receipt: %v", err)
		}
		return id
	}
	_, err := d.admin.Exec(`INSERT INTO audit_receipts (receipt_id, chunk_id, provider_id, challenge_nonce, server_challenge_ts,
		response_hash, response_latency_ms, audit_result, provider_sig, service_sig, service_countersign_ts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'PASS',$8,$9,$10)`,
		id, randBytes(32), provider, randBytes(33), challenge, randBytes(32), 120, randBytes(64), randBytes(64), countersign)
	if err != nil {
		t.Fatalf("insert receipt: %v", err)
	}
	return id
}

// ── a fake AuditAnchor ───────────────────────────────────────────────────

type fakeContract struct {
	mu        sync.Mutex
	records   map[uint64]chain.AnchorRecord
	last      uint64
	publishes int
	failNext  error // returned by the next Publish without recording anything
	loseReply bool  // the next Publish records the epoch but returns an error
	blockTime uint64
}

func newFakeContract() *fakeContract {
	return &fakeContract{records: map[uint64]chain.AnchorRecord{}, blockTime: 1_700_000_000}
}

func (f *fakeContract) LastEpoch(context.Context) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last, nil
}

func (f *fakeContract) Roots(_ context.Context, epoch uint64) (chain.AnchorRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records[epoch], nil
}

func (f *fakeContract) Publish(_ context.Context, epoch uint64, root [32]byte, n uint32) (*chain.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return nil, err
	}
	if epoch <= f.last {
		return nil, chain.ErrReverted
	}
	if root == ([32]byte{}) || n == 0 {
		return nil, chain.ErrReverted
	}
	f.publishes++
	f.last = epoch
	f.blockTime++
	f.records[epoch] = chain.AnchorRecord{Root: root, LeafCount: n, Timestamp: f.blockTime}
	if f.loseReply {
		f.loseReply = false
		return nil, errors.New("connection reset before the reply")
	}
	var tx [32]byte
	copy(tx[:], randBytes(32))
	return &chain.Receipt{TxHash: tx, Status: 1, BlockNumber: 100 + epoch}, nil
}

func (f *fakeContract) forceRecord(epoch uint64, rec chain.AnchorRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[epoch] = rec
	f.last = max(f.last, epoch)
}

// ── a recording observer ─────────────────────────────────────────────────

type recorder struct {
	mu       sync.Mutex
	reserved []int
	anchored []uint64
	failed   []string
	steps    []string
}

func (r *recorder) EpochReserved(n int) {
	r.mu.Lock()
	r.reserved = append(r.reserved, n)
	r.mu.Unlock()
}
func (r *recorder) EpochAnchored(e uint64, _ int) {
	r.mu.Lock()
	r.anchored = append(r.anchored, e)
	r.mu.Unlock()
}
func (r *recorder) EpochFailed(s string) { r.mu.Lock(); r.failed = append(r.failed, s); r.mu.Unlock() }
func (r *recorder) StepError(s string)   { r.mu.Lock(); r.steps = append(r.steps, s); r.mu.Unlock() }
