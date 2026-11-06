// Command verifyreceipt checks the audit database against the audit roots
// anchored on chain. It is an operator-side tool: it is not part of the
// provider daemon and providers never run it.
//
// It reads the receipts from PostgreSQL (the standard PG* environment
// variables) and the anchored root straight from the chain's RPC endpoint, never
// through the coordinator. A verifier that asked the coordinator for the root
// would prove nothing.
//
//	verifyreceipt list
//	verifyreceipt epoch <number|latest>
//	verifyreceipt receipt <receipt_id>
//
// Exit status: 0 the database matches the chain, 1 it does not, 2 usage or
// connection problems.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq" // registers the "postgres" driver

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/anchor"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

const (
	exitOK       = 0
	exitMismatch = 1
	exitUsage    = 2

	rpcTimeout   = 20 * time.Second
	defaultLimit = 20

	// A command with one argument is the command word plus that argument.
	oneArgument = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

type options struct {
	rpcURL     string
	anchorAddr string
	limit      int
}

func usage(w io.Writer) {
	fprintln(w, `usage: verifyreceipt [flags] <command>

commands:
  list                     the epochs this database says it anchored
  epoch <number|latest>    rebuild an epoch's root and compare it with the chain
  receipt <receipt_id>     prove one receipt is inside the root the chain holds

flags (each may also come from the environment):
  --rpc-url   chain RPC endpoint        (VYOMANAUT_CHAIN_RPC_URL)
  --anchor    AuditAnchor address       (VYOMANAUT_CHAIN_ANCHOR_ADDRESS)
  --limit     rows shown by list        (default 20)

The database is read with the usual PGHOST, PGPORT, PGUSER, PGPASSWORD and
PGDATABASE variables.`)
}

func run(args []string, getenv func(string) string, out, errOut io.Writer) int {
	var o options
	fs := flag.NewFlagSet("verifyreceipt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.rpcURL, "rpc-url", getenv("VYOMANAUT_CHAIN_RPC_URL"), "")
	fs.StringVar(&o.anchorAddr, "anchor", getenv("VYOMANAUT_CHAIN_ANCHOR_ADDRESS"), "")
	fs.IntVar(&o.limit, "limit", defaultLimit, "")
	if err := fs.Parse(args); err != nil {
		usage(errOut)
		return exitUsage
	}
	rest := fs.Args()
	if len(rest) == 0 {
		usage(errOut)
		return exitUsage
	}

	cmd, err := parseCommand(rest)
	if err != nil {
		fprintln(errOut, "verifyreceipt:", err)
		usage(errOut)
		return exitUsage
	}

	db, err := openDB(getenv)
	if err != nil {
		fprintln(errOut, "verifyreceipt: cannot reach the database:", err)
		return exitUsage
	}
	defer func() { _ = db.Close() }()

	reader, head, err := openChain(o)
	if err != nil {
		fprintln(errOut, "verifyreceipt:", err)
		return exitUsage
	}
	v := anchor.NewVerifier(db, reader, head)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return cmd(ctx, v, o, out, errOut)
}

type command func(ctx context.Context, v *anchor.Verifier, o options, out, errOut io.Writer) int

func parseCommand(args []string) (command, error) {
	switch args[0] {
	case "list":
		return cmdList, nil
	case "epoch":
		if len(args) != oneArgument {
			return nil, errors.New("epoch needs a number or the word latest")
		}
		which := args[1]
		if which != "latest" {
			if _, err := strconv.ParseUint(which, 10, 64); err != nil {
				return nil, fmt.Errorf("%q is not an epoch number", which)
			}
		}
		return func(ctx context.Context, v *anchor.Verifier, _ options, out, errOut io.Writer) int {
			return cmdEpoch(ctx, v, which, out, errOut)
		}, nil
	case "receipt":
		if len(args) != oneArgument {
			return nil, errors.New("receipt needs a receipt id")
		}
		id := args[1]
		return func(ctx context.Context, v *anchor.Verifier, _ options, out, errOut io.Writer) int {
			return cmdReceipt(ctx, v, id, out, errOut)
		}, nil
	default:
		return nil, fmt.Errorf("unknown command %q", args[0])
	}
}

func cmdList(ctx context.Context, v *anchor.Verifier, o options, out, errOut io.Writer) int {
	rows, err := v.List(ctx, o.limit)
	if err != nil {
		fprintln(errOut, "verifyreceipt:", err)
		return exitUsage
	}
	anchor.RenderList(out, rows)
	return exitOK
}

func cmdEpoch(ctx context.Context, v *anchor.Verifier, which string, out, errOut io.Writer) int {
	var epoch uint64
	if which == "latest" {
		var err error
		if epoch, err = v.Latest(ctx); err != nil {
			fprintln(errOut, "verifyreceipt:", err)
			return exitUsage
		}
	} else {
		epoch, _ = strconv.ParseUint(which, 10, 64) // validated in parseCommand
	}
	rep, err := v.VerifyEpoch(ctx, epoch)
	if err != nil {
		fprintln(errOut, "verifyreceipt:", err)
		return exitUsage
	}
	anchor.RenderEpoch(out, rep)
	if rep.Verdict.Failed() {
		return exitMismatch
	}
	return exitOK
}

func cmdReceipt(ctx context.Context, v *anchor.Verifier, id string, out, errOut io.Writer) int {
	rep, err := v.VerifyReceipt(ctx, id)
	if err != nil {
		fprintln(errOut, "verifyreceipt:", err)
		return exitUsage
	}
	anchor.RenderReceipt(out, rep)
	if !rep.Included {
		return exitMismatch
	}
	return exitOK
}

func openDB(getenv func(string) string) (*sql.DB, error) {
	pick := func(key, fallback string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return fallback
	}
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		pick("PGHOST", "localhost"), pick("PGPORT", "5432"), pick("PGUSER", "vyomanaut_app"),
		getenv("PGPASSWORD"), pick("PGDATABASE", "vyomanaut_demo"), pick("PGSSLMODE", "disable"))
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func openChain(o options) (anchor.Reader, anchor.Head, error) {
	if o.rpcURL == "" {
		return nil, nil, errors.New("no chain endpoint: pass --rpc-url or set VYOMANAUT_CHAIN_RPC_URL")
	}
	if o.anchorAddr == "" {
		return nil, nil, errors.New("no anchor contract: pass --anchor or set VYOMANAUT_CHAIN_ANCHOR_ADDRESS")
	}
	addr, err := chain.ParseAddress(o.anchorAddr)
	if err != nil {
		return nil, nil, errors.New("the anchor address is not an address")
	}
	rpc := chain.NewRPC(o.rpcURL, rpcTimeout)
	return chain.NewAnchorContract(addr, rpc, nil), rpc, nil
}

func fprintln(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }
