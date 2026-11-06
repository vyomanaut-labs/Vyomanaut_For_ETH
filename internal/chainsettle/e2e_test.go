package chainsettle

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// This test runs the whole money path: a deposit made on chain is credited to
// the ledger, ledger releases are posted to the real escrow contract on a local
// Anvil chain, and after the challenge window the providers hold the tokens.
// It is skipped unless Anvil, the built contracts and Postgres are available.

const tokenUnit = 1_000_000

type anvilChain struct {
	url string
	rpc *chain.RPC
}

func startAnvil(t *testing.T) *anvilChain {
	t.Helper()
	bin, err := exec.LookPath("anvil")
	if err != nil {
		t.Skip("anvil is not installed")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("expected a TCP listener")
	}
	port := addr.Port
	_ = l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "--port", fmt.Sprint(port), "--silent")
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })

	c := &anvilChain{url: fmt.Sprintf("http://127.0.0.1:%d", port)}
	c.rpc = chain.NewRPC(c.url, 10*time.Second)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := c.rpc.ChainID(context.Background()); err == nil {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatal("anvil did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// raw calls a node method this client does not wrap (test-only dev methods).
func (c *anvilChain) raw(t *testing.T, method string, params ...any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	resp, err := http.Post(c.url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Error != nil {
		t.Fatalf("%s failed: %v %+v", method, err, out.Error)
	}
}

func (c *anvilChain) fundedSender(t *testing.T) (*chain.Key, *chain.Sender) {
	t.Helper()
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	k, err := chain.ParseKeyHex(hex.EncodeToString(b[:]))
	if err != nil {
		t.Fatal(err)
	}
	c.raw(t, "anvil_setBalance", k.Address().Hex(), "0x56bc75e2d63100000")
	s, err := chain.NewSender(context.Background(), c.rpc, k)
	if err != nil {
		t.Fatal(err)
	}
	s.PollInterval = 20 * time.Millisecond
	return k, s
}

func artifact(t *testing.T, rel string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "out", rel)) //nolint:gosec // fixed test path
	if err != nil {
		t.Skipf("contract artifacts missing (%v); run `cd contracts && forge build`", err)
	}
	var art struct {
		Bytecode struct {
			Object string `json:"object"`
		} `json:"bytecode"`
	}
	if err := json.Unmarshal(raw, &art); err != nil {
		t.Fatal(err)
	}
	code, err := hex.DecodeString(strings.TrimPrefix(art.Bytecode.Object, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func addrBytes(a chain.Address) []byte { return a[:] }

func word(b []byte) []byte {
	w := make([]byte, 32)
	copy(w[32-len(b):], b)
	return w
}

func u64word(v uint64) []byte { return word(new(big.Int).SetUint64(v).Bytes()) }

func deploy(t *testing.T, s *chain.Sender, code []byte, args ...[]byte) chain.Address {
	t.Helper()
	data := append([]byte{}, code...)
	for _, a := range args {
		data = append(data, a...)
	}
	rc, err := s.Send(context.Background(), nil, data)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	return *rc.ContractAddress
}

func TestTheWholeMoneyPathAgainstALocalChain(t *testing.T) {
	ctx := context.Background()
	tokenCode := artifact(t, "Mocks.sol/MockUSDT.json")
	escrowCode := artifact(t, "VyomaEscrow.sol/VyomaEscrow.json")
	e := newTestEnv(t)
	c := startAnvil(t)

	coordKey, coord := c.fundedSender(t)
	guardKey, _ := c.fundedSender(t)
	ownerKey, owner := c.fundedSender(t)

	tokenAddr := deploy(t, coord, tokenCode)
	escrowAddr := deploy(t, coord, escrowCode,
		word(tokenAddr[:]), word(addrBytes(coordKey.Address())), word(addrBytes(guardKey.Address())), u64word(120), u64word(900))

	token := chain.NewERC20(tokenAddr, c.rpc, owner)
	escrowAsOwner := chain.NewEscrowContract(escrowAddr, c.rpc, owner)
	escrowAsCoord := chain.NewEscrowContract(escrowAddr, c.rpc, coord)

	// the mock token lets anyone mint
	mint := append([]byte{0x40, 0xc1, 0x0f, 0x19}, append(word(addrBytes(ownerKey.Address())), word(big.NewInt(100*tokenUnit).Bytes())...)...)
	if _, err := coord.Send(ctx, &tokenAddr, mint); err != nil {
		t.Fatal(err)
	}

	// ── the owner funds the escrow on chain ──
	ownerRow := e.newOwner(t)
	e.bindOwner(t, ownerRow, ownerKey.Address())
	if _, err := token.Approve(ctx, escrowAddr, big.NewInt(5*tokenUnit)); err != nil {
		t.Fatal(err)
	}
	if _, err := escrowAsOwner.Deposit(ctx, big.NewInt(5*tokenUnit)); err != nil {
		t.Fatal(err)
	}
	c.raw(t, "evm_mine") // one more block, so the deposit has a confirmation

	var offset time.Duration
	clock := func() time.Time { return time.Now().Add(offset) }
	cfg := Config{MicroPerPaise: 10, Confirmations: 1, LogChunk: 500, SettleLag: 5 * time.Second}
	svc, err := New(e.db, escrowAsCoord, c.rpc, cfg, WithoutLock(), WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	n, paise := e.ledgerDeposits(t, ownerRow)
	if n != 1 || paise != 500_000 { // 5 tokens = 5,000,000 micro / 10 = 500,000 paise
		t.Fatalf("ledger credited %d rows / %d paise for the on-chain deposit; want 1 / 500000", n, paise)
	}

	// ── the ledger charges the owner and releases earnings to two providers ──
	p1, p2 := e.newProvider(t), e.newProvider(t)
	w1, w2 := randWallet(), randWallet()
	e.bindProvider(t, p1, w1)
	e.bindProvider(t, p2, w2)
	e.charge(t, ownerRow, 1000) // 10,000 micro-units
	e.release(t, p1, 600)       // 6,000 micro-units
	e.release(t, p2, 400)       // 4,000 micro-units

	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("second cycle: %v", err)
	}
	if got := e.periodStatus(t, 1); got != statusPosted {
		t.Fatalf("period 1 is %s, want POSTED", got)
	}
	info, err := escrowAsCoord.PeriodInfo(ctx, 1)
	if err != nil || info.Total.Int64() != 10_000 {
		t.Fatalf("on-chain period = %+v, %v", info, err)
	}
	credit, _ := escrowAsCoord.Credit(ctx, ownerKey.Address())
	if credit.Int64() != 5*tokenUnit-10_000 {
		t.Fatalf("owner's on-chain credit = %v, want %d", credit, 5*tokenUnit-10_000)
	}

	// still inside the challenge window: nothing is claimed
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.periodStatus(t, 1); got != statusPosted {
		t.Fatalf("period 1 is %s; it must wait out the window", got)
	}

	// ── after the window, the coordinator pays on the providers' behalf ──
	c.raw(t, "evm_increaseTime", 130)
	c.raw(t, "evm_mine")
	offset = 140 * time.Second
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("claim cycle: %v", err)
	}
	if got := e.periodStatus(t, 1); got != statusClaimed {
		t.Fatalf("period 1 is %s, want CLAIMED", got)
	}

	readOnly := chain.NewERC20(tokenAddr, c.rpc, nil)
	b1, _ := readOnly.BalanceOf(ctx, w1)
	b2, _ := readOnly.BalanceOf(ctx, w2)
	if b1.Int64() != 6000 || b2.Int64() != 4000 {
		t.Fatalf("providers hold %v and %v tokens' micro-units; want 6000 and 4000", b1, b2)
	}
	if un, _ := escrowAsCoord.TotalUnclaimed(ctx); un.Sign() != 0 {
		t.Fatalf("unclaimed = %v", un)
	}
	// the coordinator's own wallet never received any of it
	if cb, _ := readOnly.BalanceOf(ctx, coordKey.Address()); cb.Sign() != 0 {
		t.Fatalf("the coordinator wallet holds %v", cb)
	}

	// nothing further happens on later cycles
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if e.count(t, `SELECT COUNT(*) FROM chain_periods`) != 1 {
		t.Fatal("a later cycle created another period")
	}

	// ── the coordinator falls silent; the owner takes the unspent credit back ──
	c.raw(t, "evm_increaseTime", 900)
	c.raw(t, "evm_mine")
	if _, err := escrowAsOwner.Refund(ctx, ownerKey.Address()); err != nil {
		t.Fatalf("refund: %v", err)
	}
	c.raw(t, "evm_mine") // a confirmation for the refund log
	if left, _ := escrowAsCoord.Credit(ctx, ownerKey.Address()); left.Sign() != 0 {
		t.Fatalf("credit after refund = %v, want 0", left)
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("refund cycle: %v", err)
	}
	// 5,000,000 deposited - 10,000 paid out = 4,990,000 micro refunded = 499,000 paise
	if n, paise := e.ledgerWithdrawals(t, ownerRow); n != 1 || paise != 499_000 {
		t.Fatalf("ledger debited %d rows / %d paise for the on-chain refund; want 1 / 499000", n, paise)
	}
}
