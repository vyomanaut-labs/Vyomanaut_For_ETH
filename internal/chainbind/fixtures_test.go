package chainbind

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq" // registers the "postgres" driver
)

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

// testDB is a throwaway database built from the checked-in schema, so these
// tests cannot disturb other packages' tests that share the CI database.
type testDB struct{ app, admin *sql.DB }

func newTestDB(t *testing.T) *testDB {
	t.Helper()
	adminUser := envOr("PGVERIFY_USER", "postgres")
	adminPass := os.Getenv("PGVERIFY_PASSWORD")
	root := openOrSkip(t, dsn(adminUser, adminPass, "postgres"))

	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	name := fmt.Sprintf("bind_test_%x", rnd)
	if _, err := root.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Skipf("cannot create a scratch database: %v", err)
	}
	t.Cleanup(func() { _, _ = root.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`) })

	admin := openOrSkip(t, dsn(adminUser, adminPass, name))
	if _, err := admin.Exec(`CREATE EXTENSION IF NOT EXISTS btree_gist`); err != nil {
		t.Skipf("btree_gist unavailable: %v", err)
	}
	for _, f := range []string{"001_initial_schema_demo.sql", "002_whitechain.sql"} {
		text, err := os.ReadFile(filepath.Join("..", "..", "migrations", f))
		if err != nil {
			t.Skipf("migration %s not found: %v", f, err)
		}
		if _, err := admin.Exec(string(text)); err != nil {
			t.Skipf("cannot apply %s: %v", f, err)
		}
	}
	return &testDB{
		app:   openOrSkip(t, dsn(envOr("PGUSER", "vyomanaut_app"), os.Getenv("PGPASSWORD"), name)),
		admin: admin,
	}
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func (d *testDB) newOwner(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := d.app.Exec(`INSERT INTO owners (owner_id, phone_number, ed25519_public_key, smart_collect_vpa)
		VALUES ($1, $2, $3, $4)`, id, fmt.Sprintf("+91%x", randBytes(5)), randBytes(32), fmt.Sprintf("v.%x@icici", randBytes(6)))
	if err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	return id
}

func (d *testDB) newProvider(t *testing.T, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := d.admin.Exec(`INSERT INTO providers (provider_id, phone_number, ed25519_public_key, status,
		declared_storage_gb, city, region, asn) VALUES ($1,$2,$3,$4,50,'TestCity','TestRegion','SIM-AS1')`,
		id, fmt.Sprintf("+91%x", randBytes(5)), randBytes(32), status)
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	return id
}
