// Command vyomachain is the chain side of Vyomanaut for data owners and for
// anyone who needs to move money without the coordinator: it makes a wallet,
// binds it to an owner, deposits USDT into the escrow, and claims a posted
// payout or takes back unspent credit directly on chain.
//
// It is not part of the provider daemon. Providers never run it: they need no
// wallet and no tokens.
//
//	vyomachain wallet new --out ~/vyoma-keys/me.key
//	vyomachain wallet show --key-file ~/vyoma-keys/me.key
//	vyomachain wallet bind --api http://10.x.x.x:8090 --data-dir ~/.vyomanaut-owner --key-file ~/vyoma-keys/me.key
//	vyomachain balance --address 0x...
//	vyomachain deposit --amount 1 --key-file ~/vyoma-keys/me.key
//	vyomachain period 3 --account 0x...
//	vyomachain claim --period 3 --account 0x... --key-file ~/vyoma-keys/any.key
//	vyomachain refund --owner 0x... --key-file ~/vyoma-keys/any.key
//
// Settings come from flags or the environment: VYOMANAUT_CHAIN_RPC_URL,
// VYOMANAUT_CHAIN_ESCROW_ADDRESS, and optionally VYOMANAUT_CHAIN_API_URL (the
// coordinator's chain API, which supplies the contract addresses).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chainctl"
)

const (
	exitOK      = 0
	exitFailed  = 1
	exitUsage   = 2
	rpcTimeout  = 20 * time.Second
	opTimeout   = 3 * time.Minute
	defaultHome = ".vyomanaut-owner"

	// The Whitechain Sepolia explorer, used when the coordinator names none.
	whitechainSepoliaID       = 1874
	whitechainSepoliaExplorer = "https://explorer.testnet.whitechain.io"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// settings are the connection details every command may need.
type settings struct {
	rpcURL   string
	escrow   string
	token    string
	api      string
	keyFile  string
	explorer string
}

func (s *settings) add(fs *flag.FlagSet, getenv func(string) string, withKey bool) {
	fs.StringVar(&s.rpcURL, "rpc-url", getenv("VYOMANAUT_CHAIN_RPC_URL"), "chain RPC endpoint (VYOMANAUT_CHAIN_RPC_URL)")
	fs.StringVar(&s.escrow, "escrow", getenv("VYOMANAUT_CHAIN_ESCROW_ADDRESS"), "escrow contract (VYOMANAUT_CHAIN_ESCROW_ADDRESS)")
	fs.StringVar(&s.token, "token", "", "token contract (read from the escrow when omitted)")
	fs.StringVar(&s.api, "api", getenv("VYOMANAUT_CHAIN_API_URL"), "the coordinator's chain API, e.g. http://10.1.2.3:8090 (VYOMANAUT_CHAIN_API_URL)")
	fs.StringVar(&s.explorer, "explorer", getenv("VYOMANAUT_CHAIN_EXPLORER_URL"), "block explorer base URL for links")
	if withKey {
		fs.StringVar(&s.keyFile, "key-file", "", "the wallet's key file (stays on this machine)")
	}
}

func usage(w io.Writer) {
	fprintln(w, `usage: vyomachain <command> [flags]

commands:
  wallet new --out FILE        make a new wallet key (never overwrites)
  wallet show --key-file FILE  print a wallet's address
  wallet bind                  prove you hold a wallet and bind it to your owner account
  balance --address 0x…        gas token, USDT and escrow credit of an address
  deposit --amount N           put N USDT into the escrow
  period N [--account 0x…]     look at a payout period (and what an account is owed in it)
  claim --period N --account 0x…   pay a posted payout to its account (anyone may do this)
  refund --owner 0x…           return an owner's unspent credit, once the coordinator has gone quiet

Run a command with -h for its flags.`)
}

func run(args []string, getenv func(string) string, out, errOut io.Writer) int {
	if len(args) == 0 {
		usage(errOut)
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "wallet":
		return runWallet(rest, getenv, out, errOut)
	case "balance":
		return runBalance(rest, getenv, out, errOut)
	case "deposit":
		return runDeposit(rest, getenv, out, errOut)
	case "period":
		return runPeriod(rest, getenv, out, errOut)
	case "claim":
		return runClaim(rest, getenv, out, errOut)
	case "refund":
		return runRefund(rest, getenv, out, errOut)
	case "-h", "--help", "help":
		usage(out)
		return exitOK
	default:
		fprintln(errOut, "vyomachain: unknown command", cmd)
		usage(errOut)
		return exitUsage
	}
}

func newFlags(name string, errOut io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	return fs
}

func parse(fs *flag.FlagSet, args []string) (ok bool, code int) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, exitOK
		}
		return false, exitUsage
	}
	return true, exitOK
}

// say is an error as a person should read it, without the internal package prefix.
func say(err error) string { return strings.TrimPrefix(err.Error(), "chainctl: ") }

func fail(errOut io.Writer, err error) int {
	fprintln(errOut, "vyomachain:", say(err))
	return exitFailed
}

func fprintln(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }

func fprintf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

// ── wallet ──────────────────────────────────────────────────────────────

func runWallet(args []string, getenv func(string) string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fprintln(errOut, "vyomachain: wallet needs new, show or bind")
		return exitUsage
	}
	switch args[0] {
	case "new":
		return walletNew(args[1:], out, errOut)
	case "show":
		return walletShow(args[1:], out, errOut)
	case "bind":
		return walletBind(args[1:], getenv, out, errOut)
	}
	fprintln(errOut, "vyomachain: unknown wallet command", args[0])
	return exitUsage
}

func walletNew(args []string, out, errOut io.Writer) int {
	fs := newFlags("wallet new", errOut)
	path := fs.String("out", "", "where to write the key file")
	if ok, code := parse(fs, args); !ok {
		return code
	}
	if *path == "" {
		fprintln(errOut, "vyomachain: --out is required")
		return exitUsage
	}
	addr, err := chainctl.NewKeyFile(*path)
	if err != nil {
		return fail(errOut, err)
	}
	fprintf(out, "wallet address: %s\nkey file:       %s (readable by you only; never share or upload it)\n", addr, *path)
	return exitOK
}

func walletShow(args []string, out, errOut io.Writer) int {
	fs := newFlags("wallet show", errOut)
	path := fs.String("key-file", "", "the wallet's key file")
	if ok, code := parse(fs, args); !ok {
		return code
	}
	key, err := chain.LoadKeyFile(*path)
	if err != nil {
		return fail(errOut, err)
	}
	fprintln(out, key.Address())
	return exitOK
}

func walletBind(args []string, getenv func(string) string, out, errOut io.Writer) int {
	fs := newFlags("wallet bind", errOut)
	var s settings
	s.add(fs, getenv, true)
	home, _ := os.UserHomeDir()
	dataDir := fs.String("data-dir", home+"/"+defaultHome, "the owner client's data directory (holds identity.json)")
	if ok, code := parse(fs, args); !ok {
		return code
	}
	if s.api == "" || s.keyFile == "" {
		fprintln(errOut, "vyomachain: wallet bind needs --api and --key-file")
		return exitUsage
	}
	key, err := chain.LoadKeyFile(s.keyFile)
	if err != nil {
		return fail(errOut, err)
	}
	id, err := chainctl.ReadIdentity(*dataDir)
	if err != nil {
		return fail(errOut, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	client := &chainctl.Client{Base: s.api, HTTP: &http.Client{Timeout: rpcTimeout}}
	info, err := client.FetchInfo(ctx)
	if err != nil {
		return fail(errOut, err)
	}
	res, err := client.BindWallet(ctx, id, info.ChainID, key)
	if err != nil {
		return fail(errOut, err)
	}
	if res.Already {
		fprintf(out, "already bound: %s is your wallet\n", res.Wallet)
	} else {
		fprintf(out, "bound: %s is now your wallet. USDT you deposit from it is credited to your account.\n", res.Wallet)
	}
	fprintf(out, "next: vyomachain deposit --amount 1 --key-file %s\n", s.keyFile)
	return exitOK
}

// ── connecting ──────────────────────────────────────────────────────────

type conn struct {
	ops      *chainctl.Ops
	explorer string
}

// connect resolves the contracts (from flags, or from the coordinator's chain
// API) and opens the chain. key may be nil for read-only commands, in which case
// a throwaway key signs nothing.
func connect(ctx context.Context, s settings, key *chain.Key) (*conn, error) {
	if s.rpcURL == "" {
		return nil, errors.New("no chain endpoint: pass --rpc-url or set VYOMANAUT_CHAIN_RPC_URL")
	}
	escrowText, tokenText, explorer := s.escrow, s.token, s.explorer
	if s.api != "" && (escrowText == "" || tokenText == "" || explorer == "") {
		client := &chainctl.Client{Base: s.api, HTTP: &http.Client{Timeout: rpcTimeout}}
		info, err := client.FetchInfo(ctx)
		if err != nil {
			return nil, err
		}
		if escrowText == "" {
			escrowText = info.Escrow.Hex()
		}
		if tokenText == "" {
			tokenText = info.Token.Hex()
		}
		if explorer == "" {
			explorer = info.Explorer
		}
	}
	if escrowText == "" {
		return nil, errors.New("no escrow contract: pass --escrow, set VYOMANAUT_CHAIN_ESCROW_ADDRESS, or pass --api")
	}
	escrow, err := chain.ParseAddress(escrowText)
	if err != nil {
		return nil, errors.New("the escrow address is not an address")
	}
	var token chain.Address
	if tokenText != "" {
		if token, err = chain.ParseAddress(tokenText); err != nil {
			return nil, errors.New("the token address is not an address")
		}
	}
	if key == nil {
		throwaway, kerr := chain.ParseKeyHex("0000000000000000000000000000000000000000000000000000000000000001")
		if kerr != nil {
			return nil, kerr
		}
		key = throwaway
	}
	rpc := chain.NewRPC(s.rpcURL, rpcTimeout)
	ops, err := chainctl.NewOps(ctx, rpc, key, escrow, token)
	if err != nil {
		return nil, err
	}
	if explorer == "" && ops.Sender.ChainID() == whitechainSepoliaID {
		explorer = whitechainSepoliaExplorer
	}
	return &conn{ops: ops, explorer: explorer}, nil
}

func (c *conn) link(out io.Writer, label string, hash [32]byte) {
	if l := chainctl.ExplorerTx(c.explorer, hash); l != "" {
		fprintf(out, "  %-9s %s\n", label, l)
		return
	}
	fprintf(out, "  %-9s 0x%x\n", label, hash[:])
}

func loadKey(s settings) (*chain.Key, error) {
	if s.keyFile == "" {
		return nil, errors.New("--key-file is required: the wallet that signs (and pays the small gas fee)")
	}
	return chain.LoadKeyFile(s.keyFile)
}

// ── balance ─────────────────────────────────────────────────────────────

func runBalance(args []string, getenv func(string) string, out, errOut io.Writer) int {
	fs := newFlags("balance", errOut)
	var s settings
	s.add(fs, getenv, false)
	addrText := fs.String("address", "", "the address to look up")
	if ok, code := parse(fs, args); !ok {
		return code
	}
	addr, err := chain.ParseAddress(*addrText)
	if err != nil {
		fprintln(errOut, "vyomachain: --address must be a 0x address")
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	c, err := connect(ctx, s, nil)
	if err != nil {
		return fail(errOut, err)
	}
	gas, err := c.ops.RPC.Balance(ctx, addr)
	if err != nil {
		return fail(errOut, err)
	}
	usdt, err := c.ops.Token.BalanceOf(ctx, addr)
	if err != nil {
		return fail(errOut, err)
	}
	credit, err := c.ops.Escrow.Credit(ctx, addr)
	if err != nil {
		return fail(errOut, err)
	}
	fprintf(out, "address        %s\n", addr)
	fprintf(out, "gas (WBT)      %s\n", chainctl.FormatUnits(gas, chainctl.GasDecimals))
	fprintf(out, "USDT           %s\n", chainctl.FormatAmount(usdt))
	fprintf(out, "escrow credit  %s USDT (unspent, inside the contract)\n", chainctl.FormatAmount(credit))
	if l := chainctl.ExplorerAddress(c.explorer, addr); l != "" {
		fprintf(out, "explorer       %s\n", l)
	}
	return exitOK
}

// ── deposit ─────────────────────────────────────────────────────────────

func runDeposit(args []string, getenv func(string) string, out, errOut io.Writer) int {
	fs := newFlags("deposit", errOut)
	var s settings
	s.add(fs, getenv, true)
	amountText := fs.String("amount", "", "USDT to deposit, e.g. 1 or 2.5")
	if ok, code := parse(fs, args); !ok {
		return code
	}
	amount, err := chainctl.ParseAmount(*amountText)
	if err != nil {
		fprintln(errOut, "vyomachain:", say(err))
		return exitUsage
	}
	key, err := loadKey(s)
	if err != nil {
		fprintln(errOut, "vyomachain:", say(err))
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	c, err := connect(ctx, s, key)
	if err != nil {
		return fail(errOut, err)
	}
	fprintf(out, "depositing %s USDT from %s into the escrow\n", chainctl.FormatAmount(amount), key.Address())
	res, err := c.ops.Deposit(ctx, amount)
	if err != nil {
		return fail(errOut, err)
	}
	if res.ApproveTx != nil {
		c.link(out, "approved", *res.ApproveTx)
	}
	c.link(out, "deposited", res.DepositTx)
	fprintln(out, "the coordinator credits your account within about a minute, once it has seen the deposit")
	return exitOK
}

// ── period / claim / refund ─────────────────────────────────────────────

func runPeriod(args []string, getenv func(string) string, out, errOut io.Writer) int {
	fs := newFlags("period", errOut)
	var s settings
	s.add(fs, getenv, false)
	accountText := fs.String("account", "", "also show what this account is owed in the period")
	// The period number may come first: vyomachain period 3 --account 0x…
	var first string
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		first, args = args[0], args[1:]
	}
	periodFlag := fs.String("period", first, "the period number")
	if ok, code := parse(fs, args); !ok {
		return code
	}
	var period uint64
	if _, err := fmt.Sscanf(*periodFlag, "%d", &period); err != nil || period == 0 {
		fprintln(errOut, "vyomachain: give a period number, for example: vyomachain period 3")
		return exitUsage
	}
	var account chain.Address
	if *accountText != "" {
		a, err := chain.ParseAddress(*accountText)
		if err != nil {
			fprintln(errOut, "vyomachain: --account must be a 0x address")
			return exitUsage
		}
		account = a
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	c, err := connect(ctx, s, nil)
	if err != nil {
		return fail(errOut, err)
	}
	v, err := c.ops.Period(ctx, period, account)
	if err != nil {
		return fail(errOut, err)
	}
	if v.Total.Sign() == 0 {
		fprintf(out, "period %d was never posted\n", period)
		return exitOK
	}
	state := "claimable now"
	switch {
	case v.Vetoed:
		state = "cancelled by the guardian"
	case time.Now().Before(v.ClaimableAt):
		state = fmt.Sprintf("challenge window open until %s (by this computer's clock)", v.ClaimableAt.Format("15:04:05 UTC"))
	case v.Unclaimed.Sign() == 0:
		state = "fully claimed"
	}
	fprintf(out, "period %d: %s\n  total %s USDT, still unclaimed %s USDT\n", period, state,
		chainctl.FormatAmount(v.Total), chainctl.FormatAmount(v.Unclaimed))
	if v.Allocation != nil {
		fprintf(out, "  owed to %s: %s USDT\n", account, chainctl.FormatAmount(v.Allocation))
	}
	return exitOK
}

func runClaim(args []string, getenv func(string) string, out, errOut io.Writer) int {
	fs := newFlags("claim", errOut)
	var s settings
	s.add(fs, getenv, true)
	period := fs.Uint64("period", 0, "the period to claim from")
	accountText := fs.String("account", "", "the account to pay (the money always goes to it, not to you)")
	if ok, code := parse(fs, args); !ok {
		return code
	}
	account, err := chain.ParseAddress(*accountText)
	if err != nil || *period == 0 {
		fprintln(errOut, "vyomachain: claim needs --period and --account 0x…")
		return exitUsage
	}
	key, err := loadKey(s)
	if err != nil {
		fprintln(errOut, "vyomachain:", say(err))
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	c, err := connect(ctx, s, key)
	if err != nil {
		return fail(errOut, err)
	}
	rc, err := c.ops.Claim(ctx, *period, account)
	if err != nil {
		return fail(errOut, err)
	}
	fprintf(out, "claimed period %d for %s, paid by %s\n", *period, account, key.Address())
	c.link(out, "claim", rc.TxHash)
	return exitOK
}

func runRefund(args []string, getenv func(string) string, out, errOut io.Writer) int {
	fs := newFlags("refund", errOut)
	var s settings
	s.add(fs, getenv, true)
	ownerText := fs.String("owner", "", "the owner whose unspent credit to return (it goes to the owner)")
	if ok, code := parse(fs, args); !ok {
		return code
	}
	owner, err := chain.ParseAddress(*ownerText)
	if err != nil {
		fprintln(errOut, "vyomachain: refund needs --owner 0x…")
		return exitUsage
	}
	key, err := loadKey(s)
	if err != nil {
		fprintln(errOut, "vyomachain:", say(err))
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	c, err := connect(ctx, s, key)
	if err != nil {
		return fail(errOut, err)
	}
	rc, err := c.ops.Refund(ctx, owner)
	if err != nil {
		return fail(errOut, err)
	}
	fprintf(out, "returned the unspent credit to %s\n", owner)
	c.link(out, "refund", rc.TxHash)
	return exitOK
}
