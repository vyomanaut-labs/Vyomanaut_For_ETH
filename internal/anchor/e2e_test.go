package anchor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// The end-to-end test: receipts in a real database are anchored on a real
// AuditAnchor contract (local Anvil chain), and the Verifier, reading the root
// straight from the contract, accepts the history, then rejects it after the
// operator edits a row. Skipped unless Anvil, the built contracts and Postgres
// are available.

func startAnvil(t *testing.T) (string, *chain.RPC) {
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

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	rpc := chain.NewRPC(url, 10*time.Second)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := rpc.ChainID(context.Background()); err == nil {
			return url, rpc
		}
		if time.Now().After(deadline) {
			t.Fatal("anvil did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func fundedSender(t *testing.T, url string, rpc *chain.RPC) (*chain.Key, *chain.Sender) {
	t.Helper()
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	k, err := chain.ParseKeyHex(hex.EncodeToString(b[:]))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "anvil_setBalance",
		"params": []any{k.Address().Hex(), "0x56bc75e2d63100000"}})
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	s, err := chain.NewSender(context.Background(), rpc, k)
	if err != nil {
		t.Fatal(err)
	}
	s.PollInterval = 20 * time.Millisecond
	return k, s
}

func artifactBytecode(t *testing.T, rel string) []byte {
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

func TestAnchorsAndVerifiesAgainstARealContract(t *testing.T) {
	ctx := context.Background()
	code := artifactBytecode(t, "AuditAnchor.sol/AuditAnchor.json")
	d := newTestDB(t)
	url, rpc := startAnvil(t)
	coordKey, coord := fundedSender(t, url, rpc)

	arg := make([]byte, 32)
	coordAddr := coordKey.Address()
	copy(arg[12:], coordAddr[:])
	rc, err := coord.Send(ctx, nil, append(append([]byte{}, code...), arg...))
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	contract := chain.NewAnchorContract(*rc.ContractAddress, rpc, coord)

	p := d.newProvider(t)
	var ids []string
	for i := 0; i < 9; i++ {
		ids = append(ids, d.addReceipt(t, p, old(900-i)).String())
	}

	svc := newService(t, d, contract)
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatalf("anchor: %v", err)
	}
	if last, err := contract.LastEpoch(ctx); err != nil || last != 1 {
		t.Fatalf("LastEpoch = %d, %v", last, err)
	}

	// The verifier reads the root from the contract, not from anything we hold.
	v := NewVerifier(d.app, contract, rpc)
	rep, err := v.VerifyEpoch(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictMatch || rep.ChainLeaves != 9 || rep.AnchorBlock == 0 {
		t.Fatalf("honest history: %+v", rep)
	}
	t.Logf("finality of the anchoring transaction: %s (block %d)", rep.Finality, rep.AnchorBlock)

	rr, err := v.VerifyReceipt(ctx, ids[4])
	if err != nil || !rr.Included {
		t.Fatalf("receipt proof: %+v, %v", rr, err)
	}

	// The operator alters one stored receipt.
	if _, err := d.admin.Exec(`UPDATE audit_receipts SET audit_result = 'FAIL' WHERE receipt_id = $1`, ids[4]); err != nil {
		t.Fatal(err)
	}
	rep, err = v.VerifyEpoch(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != VerdictMismatch {
		t.Fatalf("tampering was not detected: %+v", rep)
	}
	if rr, _ := v.VerifyReceipt(ctx, ids[4]); rr.Included {
		t.Fatal("the altered receipt was still proven included")
	}
}
