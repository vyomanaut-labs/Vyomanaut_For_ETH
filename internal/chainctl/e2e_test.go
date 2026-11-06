package chainctl

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

// The on-chain test: an owner deposits through Ops, a period is posted, a
// stranger claims it for the provider before and after the window, and the
// owner takes back unspent credit after the coordinator goes silent. Skipped
// unless Anvil and the built contracts are available.

const usdt1 = 1_000_000

type anvil struct {
	url string
	rpc *chain.RPC
}

func startAnvil(t *testing.T) *anvil {
	t.Helper()
	bin, err := exec.LookPath("anvil")
	if err != nil {
		t.Skip("anvil is not installed")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tcp, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("expected a TCP listener")
	}
	port := tcp.Port
	_ = l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "--port", fmt.Sprint(port), "--silent")
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })

	a := &anvil{url: fmt.Sprintf("http://127.0.0.1:%d", port)}
	a.rpc = chain.NewRPC(a.url, 10*time.Second)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := a.rpc.ChainID(context.Background()); err == nil {
			return a
		}
		if time.Now().After(deadline) {
			t.Fatal("anvil did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (a *anvil) raw(t *testing.T, method string, params ...any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	resp, err := http.Post(a.url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	_ = resp.Body.Close()
}

func (a *anvil) funded(t *testing.T) *chain.Key {
	t.Helper()
	var b [32]byte
	_, _ = rand.Read(b[:])
	k, err := chain.ParseKeyHex(hex.EncodeToString(b[:]))
	if err != nil {
		t.Fatal(err)
	}
	a.raw(t, "anvil_setBalance", k.Address().Hex(), "0x56bc75e2d63100000")
	return k
}

func (a *anvil) sender(t *testing.T, k *chain.Key) *chain.Sender {
	t.Helper()
	s, err := chain.NewSender(context.Background(), a.rpc, k)
	if err != nil {
		t.Fatal(err)
	}
	s.PollInterval = 20 * time.Millisecond
	return s
}

func (a *anvil) wait(t *testing.T, seconds int) {
	t.Helper()
	a.raw(t, "evm_increaseTime", seconds)
	a.raw(t, "evm_mine")
}

func bytecode(t *testing.T, rel string) []byte {
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

func word(b []byte) []byte {
	w := make([]byte, 32)
	copy(w[32-len(b):], b)
	return w
}

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

func TestTheEscapeHatchesAgainstARealEscrow(t *testing.T) {
	ctx := context.Background()
	tokenCode := bytecode(t, "Mocks.sol/MockUSDT.json")
	escrowCode := bytecode(t, "VyomaEscrow.sol/VyomaEscrow.json")
	a := startAnvil(t)

	coordKey, guardKey, ownerKey, strangerKey := a.funded(t), a.funded(t), a.funded(t), a.funded(t)
	coord := a.sender(t, coordKey)
	coordAddr, guardAddr, ownerAddr := coordKey.Address(), guardKey.Address(), ownerKey.Address()
	tokenAddr := deploy(t, coord, tokenCode)
	escrowAddr := deploy(t, coord, escrowCode, word(tokenAddr[:]), word(coordAddr[:]), word(guardAddr[:]),
		word(big.NewInt(120).Bytes()), word(big.NewInt(900).Bytes()))

	// The owner gets 100 test USDT (the mock token lets anyone mint).
	mint := append([]byte{0x40, 0xc1, 0x0f, 0x19}, append(word(ownerAddr[:]), word(big.NewInt(100*usdt1).Bytes())...)...)
	if _, err := coord.Send(ctx, &tokenAddr, mint); err != nil {
		t.Fatal(err)
	}

	// ── the owner deposits 5 USDT through Ops ──
	ownerOps, err := NewOps(ctx, a.rpc, ownerKey, escrowAddr, chain.Address{}) // token read from the escrow
	if err != nil {
		t.Fatal(err)
	}
	ownerOps.Sender.PollInterval = 20 * time.Millisecond
	if _, err := ownerOps.Deposit(ctx, big.NewInt(1000*usdt1)); err == nil || !strings.Contains(err.Error(), "holds") {
		t.Fatalf("a deposit larger than the balance must fail before any gas is spent: %v", err)
	}
	res, err := ownerOps.Deposit(ctx, big.NewInt(5*usdt1))
	if err != nil || res.ApproveTx == nil || res.Block == 0 {
		t.Fatalf("deposit = %+v, %v", res, err)
	}
	if credit, _ := ownerOps.Escrow.Credit(ctx, ownerAddr); credit.Int64() != 5*usdt1 {
		t.Fatalf("credit = %v", credit)
	}
	// A second deposit within the existing allowance needs no new approval.
	if _, err := ownerOps.Token.Approve(ctx, escrowAddr, big.NewInt(10*usdt1)); err != nil {
		t.Fatal(err)
	}
	res, err = ownerOps.Deposit(ctx, big.NewInt(1*usdt1))
	if err != nil || res.ApproveTx != nil {
		t.Fatalf("second deposit = %+v, %v; want no approval", res, err)
	}

	// ── the coordinator posts a period paying the provider 2 USDT ──
	provider := chain.MustParseAddress("0x00000000000000000000000000000000000000a1")
	coordEscrow := chain.NewEscrowContract(escrowAddr, a.rpc, coord)
	if _, err := coordEscrow.PostPeriod(ctx, 1, [32]byte{1}, []chain.Address{ownerAddr}, []*big.Int{big.NewInt(2 * usdt1)},
		[]chain.Address{provider}, []*big.Int{big.NewInt(2 * usdt1)}); err != nil {
		t.Fatal(err)
	}

	// ── a stranger, with no link to the coordinator, claims for the provider ──
	strangerOps, err := NewOps(ctx, a.rpc, strangerKey, escrowAddr, tokenAddr)
	if err != nil {
		t.Fatal(err)
	}
	strangerOps.Sender.PollInterval = 20 * time.Millisecond

	view, err := strangerOps.Period(ctx, 1, provider)
	if err != nil || view.Total.Int64() != 2*usdt1 || view.Allocation.Int64() != 2*usdt1 || view.Vetoed {
		t.Fatalf("period view = %+v, %v", view, err)
	}
	if _, err := strangerOps.Claim(ctx, 1, provider); err == nil || !strings.Contains(err.Error(), "challenge window is still open") {
		t.Fatalf("an early claim must be explained: %v", err)
	}
	a.wait(t, 121)
	if _, err := strangerOps.Claim(ctx, 1, provider); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if bal, _ := strangerOps.Token.BalanceOf(ctx, provider); bal.Int64() != 2*usdt1 {
		t.Fatalf("the provider holds %v, want 2 USDT: the money must go to the account", bal)
	}
	if bal, _ := strangerOps.Token.BalanceOf(ctx, strangerKey.Address()); bal.Sign() != 0 {
		t.Fatal("the stranger who claimed must receive nothing")
	}
	if _, err := strangerOps.Claim(ctx, 1, provider); err == nil || !strings.Contains(err.Error(), "already paid") {
		t.Fatalf("a second claim must be explained: %v", err)
	}

	// ── the coordinator goes silent: the owner's unspent credit comes back ──
	if _, err := strangerOps.Refund(ctx, ownerAddr); err == nil || !strings.Contains(err.Error(), "refunds open at") {
		t.Fatalf("an early refund must be explained: %v", err)
	}
	a.wait(t, 901)
	before, _ := ownerOps.Token.BalanceOf(ctx, ownerAddr)
	if _, err := strangerOps.Refund(ctx, ownerAddr); err != nil {
		t.Fatalf("refund: %v", err)
	}
	after, _ := ownerOps.Token.BalanceOf(ctx, ownerAddr)
	// 6 deposited - 2 posted for the provider = 4 unspent
	if got := new(big.Int).Sub(after, before).Int64(); got != 4*usdt1 {
		t.Fatalf("refunded %d, want 4 USDT", got)
	}
	if _, err := strangerOps.Refund(ctx, ownerAddr); err == nil || !strings.Contains(err.Error(), "no unspent credit") {
		t.Fatalf("a second refund must be explained: %v", err)
	}
}
