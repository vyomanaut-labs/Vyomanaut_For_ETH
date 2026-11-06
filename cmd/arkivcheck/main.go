// Command arkivcheck checks the Arkiv mirror. It is an operator- and
// judge-side tool: it is not part of the provider daemon and providers never
// run it.
//
// It reads entities straight from the Arkiv RPC endpoint, never through the
// coordinator, verifies each one on its own (receipt order, Merkle root,
// coordinator signature, attributes), and, when it can reach PostgreSQL,
// rebuilds the same batch from the database and compares the two. Editing an
// audit receipt in PostgreSQL after it was mirrored turns its batch red.
//
//	arkivcheck audits           confirmed audit batches recorded in the database
//	arkivcheck files            confirmed file manifests and tombstones
//	arkivcheck entity <key>     one entity, by key
//	arkivcheck public           find batches by attribute query, without the database
//
// Exit status: 0 everything verified, 1 a mismatch or invalid entity, 2 usage
// or connection problems.
package main

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	_ "github.com/lib/pq" // registers the "postgres" driver

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/arkiv"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

const (
	exitOK       = 0
	exitMismatch = 1
	exitUsage    = 2

	rpcTimeout    = 2 * time.Minute
	defaultLimit  = 20
	maxQueryLimit = 100
	oneArgument   = 2
	entityKeyLen  = 2 + 64 // "0x" and 64 hex digits

	defaultRPCURL = "https://rpc.rabdi.db-chain.testnet.arkiv.network"
	explorerBase  = "https://rabdi.explorer.arkiv.network"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

type options struct {
	rpcURL        string
	accessKeyFile string
	coordKey      string
	appTag        string
	limit         int
}

func usage(w io.Writer) {
	fprintln(w, `usage: arkivcheck [flags] <command>

commands:
  audits             verify the newest confirmed audit batches against PostgreSQL
  files              verify the newest file manifests and tombstones against PostgreSQL
  entity <key>       fetch and verify one entity by its key
  public             find audit batches by attribute query and verify them
                     (no database needed: anyone can run it)

flags (each may also come from the environment):
  --rpc-url          Arkiv RPC endpoint            (VYOMANAUT_ARKIV_RPC_URL, default Rabdi)
  --access-key-file  file holding an RPC access key (VYOMANAUT_ARKIV_ACCESS_KEY_FILE)
  --coord-key        trusted coordinator Ed25519 public key, hex (VYOMANAUT_ARKIV_COORD_KEY)
  --app              app tag                       (VYOMANAUT_ARKIV_APP_TAG, default vyomanaut-demo)
  --limit            entities checked              (default 20, at most 100)

Without --coord-key, signatures are checked against the key each entity
names, and that key is printed so you can compare it with the coordinator's.
The database is read with the usual PGHOST, PGPORT, PGUSER, PGPASSWORD and
PGDATABASE variables.`)
}

func run(args []string, getenv func(string) string, out, errOut io.Writer) int {
	var o options
	fs := flag.NewFlagSet("arkivcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.rpcURL, "rpc-url", or(getenv("VYOMANAUT_ARKIV_RPC_URL"), defaultRPCURL), "")
	fs.StringVar(&o.accessKeyFile, "access-key-file", getenv("VYOMANAUT_ARKIV_ACCESS_KEY_FILE"), "")
	fs.StringVar(&o.coordKey, "coord-key", getenv("VYOMANAUT_ARKIV_COORD_KEY"), "")
	fs.StringVar(&o.appTag, "app", or(getenv("VYOMANAUT_ARKIV_APP_TAG"), arkiv.DefaultAppTag), "")
	fs.IntVar(&o.limit, "limit", defaultLimit, "")
	if err := fs.Parse(args); err != nil || fs.NArg() == 0 {
		usage(errOut)
		return exitUsage
	}
	if o.limit < 1 || o.limit > maxQueryLimit {
		fprintln(errOut, "arkivcheck: --limit must be 1 to 100")
		return exitUsage
	}
	trusted, err := parseCoordKey(o.coordKey)
	if err != nil {
		fprintln(errOut, "arkivcheck:", err)
		return exitUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	access := ""
	if o.accessKeyFile != "" {
		if access, err = arkiv.ReadSecretFile(o.accessKeyFile); err != nil {
			fprintln(errOut, "arkivcheck:", err)
			return exitUsage
		}
	}
	client, err := arkiv.DialReadOnly(ctx, o.rpcURL, access)
	if err != nil {
		fprintln(errOut, "arkivcheck: cannot reach the Arkiv RPC endpoint")
		return exitUsage
	}
	defer client.Close()
	c := checker{client: client, trusted: trusted, out: out, errOut: errOut, opt: o}

	cmd := fs.Arg(0)
	switch {
	case cmd == "audits" && fs.NArg() == 1:
		return c.withDB(ctx, c.audits)
	case cmd == "files" && fs.NArg() == 1:
		return c.withDB(ctx, c.files)
	case cmd == "entity" && fs.NArg() == oneArgument:
		return c.entity(ctx, fs.Arg(1))
	case cmd == "public" && fs.NArg() == 1:
		return c.public(ctx)
	default:
		usage(errOut)
		return exitUsage
	}
}

func or(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func parseCoordKey(s string) (ed25519.PublicKey, error) {
	if s == "" {
		return nil, nil
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("--coord-key must be 32 bytes of hex")
	}
	return ed25519.PublicKey(raw), nil
}

type checker struct {
	client  *arkiv.Client
	trusted ed25519.PublicKey
	out     io.Writer
	errOut  io.Writer
	opt     options
	keys    map[string]bool // coordinator keys seen when none is trusted
}

func (c *checker) withDB(ctx context.Context, f func(context.Context, *sql.DB) int) int {
	db, err := sql.Open("postgres", "")
	if err != nil {
		fprintln(c.errOut, "arkivcheck: cannot open the database:", err)
		return exitUsage
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		fprintln(c.errOut, "arkivcheck: cannot reach the database (set PGHOST, PGUSER, PGPASSWORD, PGDATABASE):", err)
		return exitUsage
	}
	return f(ctx, db)
}

func (c *checker) noteKey(pub ed25519.PublicKey) {
	if c.trusted != nil || pub == nil {
		return
	}
	if c.keys == nil {
		c.keys = map[string]bool{}
	}
	c.keys[hex.EncodeToString(pub)] = true
}

func (c *checker) summary(ok, bad int) int {
	for k := range c.keys {
		fprintf(c.out, "signed by coordinator key %s (pass --coord-key to pin it)\n", k)
	}
	fprintf(c.out, "\n%d verified, %d failed\n", ok, bad)
	if bad > 0 {
		return exitMismatch
	}
	return exitOK
}

// audits verifies the newest confirmed audit batches.
func (c *checker) audits(ctx context.Context, db *sql.DB) int {
	rows, err := arkiv.ConfirmedPublications(ctx, db, arkiv.KindAuditBatch, c.opt.limit)
	if err != nil {
		fprintln(c.errOut, "arkivcheck: cannot read arkiv_publications:", err)
		return exitUsage
	}
	if len(rows) == 0 {
		fprintln(c.out, "no confirmed audit batches yet")
		return exitOK
	}
	ok, bad := 0, 0
	for _, r := range rows {
		e, err := c.client.ByKey(ctx, r.EntityKey)
		if err != nil {
			bad++
			fprintf(c.out, "FAIL  %s  %v\n", r.Reference, err)
			continue
		}
		b, pub, err := arkiv.CheckAuditEntity(e, c.trusted)
		c.noteKey(pub)
		if err == nil {
			err = arkiv.CompareBatchWithDB(ctx, db, b)
		}
		line := fmt.Sprintf("%s  provider %s  period %s  seq %d  %d receipts (%d pass, %d fail, %d timeout)  entity %s",
			"%s", short(b.ProviderID), b.PeriodStart.Format("15:04:05"), b.Seq, len(b.Receipts), b.Pass, b.Fail, b.Timeout, short32(r.EntityKey))
		if err != nil {
			bad++
			fprintf(c.out, line+"\n      %v\n", "FAIL", err)
			continue
		}
		ok++
		fprintf(c.out, line+"\n", "OK  ")
	}
	fprintf(c.out, "explorer: %s\n", explorerBase)
	return c.summary(ok, bad)
}

// files verifies manifests and tombstones.
func (c *checker) files(ctx context.Context, db *sql.DB) int {
	rows, err := arkiv.ConfirmedPublications(ctx, db, "", maxQueryLimit)
	if err != nil {
		fprintln(c.errOut, "arkivcheck: cannot read arkiv_publications:", err)
		return exitUsage
	}
	ok, bad, n := 0, 0, 0
	for _, r := range rows {
		if r.Kind == arkiv.KindAuditBatch {
			continue
		}
		if n++; n > c.opt.limit {
			break
		}
		e, err := c.client.ByKey(ctx, r.EntityKey)
		if err == nil {
			err = c.checkFileEntity(ctx, db, r.Kind, e)
		}
		if err != nil {
			bad++
			fprintf(c.out, "FAIL  %-14s %s\n      %v\n", r.Kind, r.Reference, err)
			continue
		}
		ok++
		fprintf(c.out, "OK    %-14s %s  entity %s\n", r.Kind, r.Reference, short32(r.EntityKey))
	}
	if n == 0 {
		fprintln(c.out, "no confirmed file manifests yet")
		return exitOK
	}
	return c.summary(ok, bad)
}

func (c *checker) checkFileEntity(ctx context.Context, db *sql.DB, kind string, e arkiv.Entity) error {
	body, err := e.Bytes(arkiv.AttrBody)
	if err != nil {
		return err
	}
	switch kind {
	case arkiv.KindFileManifest:
		m, err := arkiv.CheckManifestEntity(e, c.trusted)
		if err != nil {
			return err
		}
		_, pub, _ := arkiv.DecodeFileManifest(body)
		c.noteKey(pub)
		return arkiv.CompareManifestWithDB(ctx, db, m)
	case arkiv.KindFileTombstone:
		id, _, pub, err := arkiv.DecodeTombstone(body)
		if err != nil {
			return err
		}
		if c.trusted != nil && !pub.Equal(c.trusted) {
			return errors.New("tombstone not signed by the trusted coordinator key")
		}
		c.noteKey(pub)
		deleted, err := arkiv.FileDeleted(ctx, db, id)
		if err != nil {
			return err
		}
		if !deleted {
			return fmt.Errorf("%w: Arkiv says deleted, the database says it is not", arkiv.ErrMismatch)
		}
		return nil
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
}

// entity fetches and verifies one entity.
func (c *checker) entity(ctx context.Context, key string) int {
	if !strings.HasPrefix(key, "0x") || len(key) != entityKeyLen {
		fprintln(c.errOut, "arkivcheck: an entity key is 0x followed by 64 hex digits")
		return exitUsage
	}
	e, err := c.client.ByKey(ctx, common.HexToHash(key))
	if err != nil {
		fprintln(c.out, "FAIL ", err)
		return exitMismatch
	}
	kind, _ := e.Str(arkiv.AttrKind)
	fprintf(c.out, "entity %s\nowner  %s\nkind   %s\nexpires at block %s\n", e.Key, e.Owner, kind, e.ExpiresAt)
	switch kind {
	case arkiv.KindAuditBatch:
		b, pub, err := arkiv.CheckAuditEntity(e, c.trusted)
		if err != nil {
			fprintln(c.out, "FAIL ", err)
			return exitMismatch
		}
		fprintf(c.out, "provider %s, period %s to %s, seq %d\n%d receipts: %d pass, %d fail, %d timeout\nmerkle root %x\nsigned by %x\n",
			receiptleaf.FormatUUID(b.ProviderID), b.PeriodStart.Format(time.RFC3339), b.PeriodEnd.Format(time.RFC3339),
			b.Seq, len(b.Receipts), b.Pass, b.Fail, b.Timeout, b.Root, []byte(pub))
	case arkiv.KindFileManifest:
		m, err := arkiv.CheckManifestEntity(e, c.trusted)
		if err != nil {
			fprintln(c.out, "FAIL ", err)
			return exitMismatch
		}
		fprintf(c.out, "file %s: %d segments, %d shards, RS(%d,%d), shard root %x\n",
			receiptleaf.FormatUUID(m.FileID), m.Segments, m.Shards, m.RSK, m.RSN, m.ShardRoot)
	case arkiv.KindFileTombstone:
		body, _ := e.Bytes(arkiv.AttrBody)
		id, seen, _, err := arkiv.DecodeTombstone(body)
		if err != nil {
			fprintln(c.out, "FAIL ", err)
			return exitMismatch
		}
		fprintf(c.out, "file %s deleted (seen %s)\n", receiptleaf.FormatUUID(id), seen.Format(time.RFC3339))
	default:
		fprintln(c.out, "FAIL  not a Vyomanaut entity")
		return exitMismatch
	}
	fprintln(c.out, "OK    the entity verifies on its own")
	return exitOK
}

// public lists audit batches by attribute query and verifies each one. It
// needs no database, so anyone can run it.
func (c *checker) public(ctx context.Context) int {
	rows, err := c.client.Query(ctx, arkiv.KindQuery(c.opt.appTag, arkiv.KindAuditBatch), arkiv.EntityAttrNames, c.opt.limit, 0)
	if err != nil {
		fprintln(c.errOut, "arkivcheck: query failed:", err)
		return exitUsage
	}
	if len(rows) == 0 {
		fprintln(c.out, "no audit batches found for app", c.opt.appTag)
		return exitOK
	}
	ok, bad := 0, 0
	for _, e := range rows {
		b, pub, err := arkiv.CheckAuditEntity(e, c.trusted)
		c.noteKey(pub)
		if err != nil {
			bad++
			fprintf(c.out, "FAIL  %s  %v\n", e.Key, err)
			continue
		}
		ok++
		fprintf(c.out, "OK    %s  provider %s  period %s  %d receipts\n",
			e.Key, short(b.ProviderID), b.PeriodStart.Format(time.RFC3339), len(b.Receipts))
	}
	return c.summary(ok, bad)
}

func short(id [16]byte) string { return receiptleaf.FormatUUID(id)[:8] }

func short32(h common.Hash) string { return h.Hex()[:10] + "…" }

func fprintln(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

func fprintf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
