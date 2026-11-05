package chain

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeNode is a JSON-RPC server driven by per-method handlers.
type fakeNode struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	handlers map[string]func(params []json.RawMessage) (any, *RPCError)
	calls    map[string]int
}

func newFakeNode(t *testing.T) *fakeNode {
	t.Helper()
	n := &fakeNode{t: t, handlers: map[string]func([]json.RawMessage) (any, *RPCError){}, calls: map[string]int{}}
	n.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		n.mu.Lock()
		n.calls[req.Method]++
		h := n.handlers[req.Method]
		n.mu.Unlock()

		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if h == nil {
			resp["error"] = &RPCError{Code: -32601, Message: "method not found: " + req.Method}
		} else if res, rerr := h(req.Params); rerr != nil {
			resp["error"] = rerr
		} else {
			resp["result"] = res
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(n.srv.Close)
	return n
}

func (n *fakeNode) on(method string, h func(params []json.RawMessage) (any, *RPCError)) {
	n.mu.Lock()
	n.handlers[method] = h
	n.mu.Unlock()
}

func (n *fakeNode) result(method string, v any) {
	n.on(method, func([]json.RawMessage) (any, *RPCError) { return v, nil })
}

func (n *fakeNode) count(method string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[method]
}

func (n *fakeNode) rpc() *RPC { return NewRPC(n.srv.URL, 5*time.Second) }

// standardNode answers everything a Sender needs for a healthy chain.
func standardNode(t *testing.T) *fakeNode {
	n := newFakeNode(t)
	n.result("eth_chainId", "0x752")                                                 // 1874
	n.result("eth_getBlockByNumber", map[string]any{"baseFeePerGas": "0x12a05f200"}) // 5 gwei
	n.result("eth_maxPriorityFeePerGas", "0x3b9aca00")                               // 1 gwei
	n.result("eth_estimateGas", "0x186a0")                                           // 100000
	return n
}

func TestRPCErrorsNeverContainTheEndpoint(t *testing.T) {
	const secret = "SECRET-API-KEY-123"
	for name, endpoint := range map[string]string{
		"connection refused": "http://127.0.0.1:1/v3/" + secret,
		"bad url":            "http://[::1]:namedport/" + secret,
	} {
		_, err := NewRPC(endpoint, time.Second).ChainID(context.Background())
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: error leaks the endpoint: %v", name, err)
		}
		if !errors.Is(err, ErrRPC) {
			t.Errorf("%s: err = %v, want ErrRPC", name, err)
		}
	}
}

func TestRPCReportsHTTPFailuresWithoutTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream says: key abc-secret is invalid", http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := NewRPC(srv.URL, time.Second).BlockNumber(context.Background())
	if !errors.Is(err, ErrRPC) || strings.Contains(err.Error(), "abc-secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestRPCQueriesParseQuantities(t *testing.T) {
	n := newFakeNode(t)
	n.result("eth_chainId", "0x752")
	n.result("eth_blockNumber", "0xbc1c0c")
	n.result("eth_getBalance", "0xde0b6b3a7640000")
	n.result("eth_getTransactionCount", "0x2a")
	r := n.rpc()
	ctx := context.Background()

	if id, err := r.ChainID(ctx); err != nil || id != 1874 {
		t.Errorf("ChainID = %d, %v", id, err)
	}
	if b, err := r.BlockNumber(ctx); err != nil || b != 12327948 {
		t.Errorf("BlockNumber = %d, %v", b, err)
	}
	if bal, err := r.Balance(ctx, MustParseAddress(anvilAddr0)); err != nil || bal.String() != "1000000000000000000" {
		t.Errorf("Balance = %v, %v", bal, err)
	}
	if nonce, err := r.PendingNonce(ctx, MustParseAddress(anvilAddr0)); err != nil || nonce != 42 {
		t.Errorf("PendingNonce = %d, %v", nonce, err)
	}
	n.result("eth_chainId", "0xzz")
	if _, err := r.ChainID(ctx); !errors.Is(err, ErrRPC) {
		t.Errorf("a malformed quantity must be an ErrRPC, got %v", err)
	}
}

func TestFeeCapsLeaveRoomForTheBaseFeeToDouble(t *testing.T) {
	n := newFakeNode(t)
	n.result("eth_getBlockByNumber", map[string]any{"baseFeePerGas": "0x12a05f200"}) // 5 gwei
	// the node has no eth_maxPriorityFeePerGas: fall back to 1 gwei
	tip, feeCap, err := n.rpc().FeeCaps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tip.Int64() != 1_000_000_000 || feeCap.Int64() != 11_000_000_000 {
		t.Fatalf("tip %v feeCap %v, want 1 gwei and 11 gwei", tip, feeCap)
	}
	n.result("eth_maxPriorityFeePerGas", "0x77359400") // 2 gwei
	tip, feeCap, err = n.rpc().FeeCaps(context.Background())
	if err != nil || tip.Int64() != 2_000_000_000 || feeCap.Int64() != 12_000_000_000 {
		t.Fatalf("tip %v feeCap %v err %v", tip, feeCap, err)
	}
}

func revertingNode(t *testing.T, reasonData string) *fakeNode {
	n := newFakeNode(t)
	n.on("eth_estimateGas", func([]json.RawMessage) (any, *RPCError) {
		return nil, &RPCError{Code: 3, Message: "execution reverted", Data: json.RawMessage(`"` + reasonData + `"`)}
	})
	return n
}

func TestNodeRevertBecomesARevertErrorWithAReadableReason(t *testing.T) {
	notCoordinator, _ := encodeCall("NotCoordinator()")
	n := revertingNode(t, "0x"+hex.EncodeToString(notCoordinator))
	to := MustParseAddress(anvilAddr0)
	_, err := n.rpc().EstimateGas(context.Background(), to, &to, nil)

	var re *RevertError
	if !errors.Is(err, ErrReverted) || !errors.As(err, &re) {
		t.Fatalf("err = %v, want a RevertError", err)
	}
	if re.Reason != "NotCoordinator()" {
		t.Fatalf("reason = %q", re.Reason)
	}
}

func TestOtherNodeErrorsStayNodeErrors(t *testing.T) {
	n := newFakeNode(t) // no handlers: method not found
	_, err := n.rpc().BlockNumber(context.Background())
	var re *RPCError
	if !errors.As(err, &re) || re.Code != -32601 {
		t.Fatalf("err = %v", err)
	}
}

func TestReceiptNilMeansNotMinedYet(t *testing.T) {
	n := newFakeNode(t)
	n.result("eth_getTransactionReceipt", nil)
	rc, err := n.rpc().Receipt(context.Background(), [32]byte{1})
	if err != nil || rc != nil {
		t.Fatalf("receipt = %v, err = %v; want nil, nil", rc, err)
	}
}

func TestReceiptParsesLogsAndContractAddress(t *testing.T) {
	topic := "0x" + strings.Repeat("ab", 32)
	n := newFakeNode(t)
	n.result("eth_getTransactionReceipt", map[string]any{
		"transactionHash": "0x" + strings.Repeat("11", 32), "status": "0x1", "blockNumber": "0x10", "gasUsed": "0x5208",
		"contractAddress": anvilAddr0,
		"logs": []map[string]any{{
			"address": anvilAddr0, "topics": []string{topic}, "data": "0x" + strings.Repeat("00", 31) + "05",
			"blockNumber": "0x10", "transactionHash": "0x" + strings.Repeat("11", 32), "logIndex": "0x3",
		}},
	})
	rc, err := n.rpc().Receipt(context.Background(), [32]byte{})
	if err != nil || rc == nil {
		t.Fatalf("%v %v", rc, err)
	}
	if rc.Status != 1 || rc.BlockNumber != 16 || rc.GasUsed != 21000 || rc.ContractAddress == nil {
		t.Fatalf("receipt = %+v", rc)
	}
	if len(rc.Logs) != 1 || rc.Logs[0].LogIndex != 3 || len(rc.Logs[0].Topics) != 1 {
		t.Fatalf("logs = %+v", rc.Logs)
	}
}

func TestDepositEventsDecodeOwnerAndAmount(t *testing.T) {
	owner := MustParseAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	var topic [32]byte
	copy(topic[12:], owner[:])
	amount := make([]byte, 32)
	big.NewInt(110011323).FillBytes(amount)
	got, err := parseDeposits([]Log{{Topics: [][32]byte{depositedTopic, topic}, Data: amount, BlockNumber: 9, LogIndex: 2}})
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	if got[0].Owner != owner || got[0].Amount.Int64() != 110011323 || got[0].BlockNumber != 9 || got[0].LogIndex != 2 {
		t.Fatalf("event = %+v", got[0])
	}
	if _, err := parseDeposits([]Log{{Topics: [][32]byte{depositedTopic}}}); !errors.Is(err, ErrABI) {
		t.Errorf("a log without an owner topic must be rejected: %v", err)
	}
	if hex.EncodeToString(depositedTopic[:]) == "" {
		t.Fatal("topic unset")
	}
}

func TestFinalizedBlockReportsMissingFinality(t *testing.T) {
	n := newFakeNode(t) // eth_getBlockByNumber answers "method not found"
	if _, err := n.rpc().FinalizedBlock(context.Background()); !errors.Is(err, ErrNoFinality) {
		t.Fatalf("err = %v, want ErrNoFinality", err)
	}
	n.result("eth_getBlockByNumber", map[string]any{"number": "0x64"})
	if b, err := n.rpc().FinalizedBlock(context.Background()); err != nil || b != 100 {
		t.Fatalf("%d %v", b, err)
	}
}

// ── Sender ───────────────────────────────────────────────────────────────

func newTestSender(t *testing.T, n *fakeNode) *Sender {
	t.Helper()
	s, err := NewSender(context.Background(), n.rpc(), mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	s.PollInterval = 5 * time.Millisecond
	s.ReceiptTimeout = 2 * time.Second
	return s
}

func minedReceipt(status string) map[string]any {
	return map[string]any{
		"transactionHash": "0x" + strings.Repeat("22", 32), "status": status, "blockNumber": "0x1",
		"gasUsed": "0x5208", "logs": []any{},
	}
}

func TestSenderPadsGasAndNeverReusesANonce(t *testing.T) {
	n := standardNode(t)
	n.result("eth_getTransactionCount", "0x7") // a lagging node keeps answering 7
	var mu sync.Mutex
	var sent [][]byte
	n.on("eth_sendRawTransaction", func(p []json.RawMessage) (any, *RPCError) {
		var s string
		_ = json.Unmarshal(p[0], &s)
		raw, _ := hex.DecodeString(strings.TrimPrefix(s, "0x"))
		mu.Lock()
		sent = append(sent, raw)
		mu.Unlock()
		h := Keccak256(raw)
		return "0x" + hex.EncodeToString(h[:]), nil
	})
	n.result("eth_getTransactionReceipt", minedReceipt("0x1"))

	s := newTestSender(t, n)
	to := MustParseAddress("0x5FbDB2315678afecb367f032d93F642f64180aa3")
	data := []byte{0xde, 0xad}
	for i := 0; i < 2; i++ {
		if _, err := s.Send(context.Background(), &to, data); err != nil {
			t.Fatal(err)
		}
	}

	// the expected transactions: nonces 7 then 8, gas 100000 + 30% + 25000
	for i, wantNonce := range []uint64{7, 8} {
		tx := &Tx{
			ChainID: 1874, Nonce: wantNonce, GasTipCap: big.NewInt(1_000_000_000),
			GasFeeCap: big.NewInt(11_000_000_000), Gas: 155_000, To: &to, Value: new(big.Int), Data: data,
		}
		want, _, err := tx.Sign(mustKey(t))
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(sent[i]) != hex.EncodeToString(want) {
			t.Errorf("transaction %d differs from the expected nonce %d / gas 155000 transaction", i, wantNonce)
		}
	}
}

func TestSenderReturnsRevertWithoutSendingAnything(t *testing.T) {
	n := standardNode(t)
	n.on("eth_estimateGas", func([]json.RawMessage) (any, *RPCError) {
		return nil, &RPCError{Code: 3, Message: "execution reverted: nope"}
	})
	n.result("eth_getTransactionCount", "0x0")
	s := newTestSender(t, n)
	to := MustParseAddress(anvilAddr0)
	_, err := s.Send(context.Background(), &to, nil)
	if !errors.Is(err, ErrReverted) {
		t.Fatalf("err = %v, want ErrReverted", err)
	}
	if n.count("eth_sendRawTransaction") != 0 {
		t.Fatal("a reverting call was sent to the chain")
	}
}

func TestSenderReportsAFailedTransaction(t *testing.T) {
	n := standardNode(t)
	n.result("eth_getTransactionCount", "0x0")
	n.on("eth_sendRawTransaction", func([]json.RawMessage) (any, *RPCError) { return "0x" + strings.Repeat("22", 32), nil })
	n.result("eth_getTransactionReceipt", minedReceipt("0x0"))
	to := MustParseAddress(anvilAddr0)
	rc, err := newTestSender(t, n).Send(context.Background(), &to, nil)
	if !errors.Is(err, ErrTxFailed) || rc == nil {
		t.Fatalf("rc = %v, err = %v", rc, err)
	}
	if !strings.Contains(err.Error(), strings.Repeat("22", 32)) {
		t.Errorf("the error should name the transaction hash: %v", err)
	}
}

func TestSenderTimesOutWhenNothingIsMined(t *testing.T) {
	n := standardNode(t)
	n.result("eth_getTransactionCount", "0x0")
	n.on("eth_sendRawTransaction", func([]json.RawMessage) (any, *RPCError) { return "0x" + strings.Repeat("33", 32), nil })
	n.result("eth_getTransactionReceipt", nil)
	s := newTestSender(t, n)
	s.ReceiptTimeout = 60 * time.Millisecond
	to := MustParseAddress(anvilAddr0)
	_, err := s.Send(context.Background(), &to, nil)
	if !errors.Is(err, ErrReceiptTimeout) || !strings.Contains(err.Error(), strings.Repeat("33", 32)) {
		t.Fatalf("err = %v", err)
	}
}

func TestSenderStopsWaitingWhenTheContextEnds(t *testing.T) {
	n := standardNode(t)
	n.result("eth_getTransactionCount", "0x0")
	n.on("eth_sendRawTransaction", func([]json.RawMessage) (any, *RPCError) { return "0x" + strings.Repeat("44", 32), nil })
	n.result("eth_getTransactionReceipt", nil)
	s := newTestSender(t, n)
	s.ReceiptTimeout = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	to := MustParseAddress(anvilAddr0)
	if _, err := s.Send(ctx, &to, nil); err == nil {
		t.Fatal("expected an error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("Send ignored its context")
	}
}

func TestSenderRetriesOnceWithAFreshNonceWhenTheNodeSaysNonceTooLow(t *testing.T) {
	n := standardNode(t)
	var mu sync.Mutex
	nodeNonce := uint64(3)
	n.on("eth_getTransactionCount", func([]json.RawMessage) (any, *RPCError) {
		mu.Lock()
		defer mu.Unlock()
		return quantity(nodeNonce), nil
	})
	attempts := 0
	n.on("eth_sendRawTransaction", func(p []json.RawMessage) (any, *RPCError) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			nodeNonce = 9 // the chain moved on while we were deciding
			return nil, &RPCError{Code: -32000, Message: "nonce too low"}
		}
		var s string
		_ = json.Unmarshal(p[0], &s)
		raw, _ := hex.DecodeString(strings.TrimPrefix(s, "0x"))
		h := Keccak256(raw)
		return "0x" + hex.EncodeToString(h[:]), nil
	})
	n.result("eth_getTransactionReceipt", minedReceipt("0x1"))

	to := MustParseAddress(anvilAddr0)
	if _, err := newTestSender(t, n).Send(context.Background(), &to, nil); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestSenderGivesUpAfterOneRetry(t *testing.T) {
	n := standardNode(t)
	n.result("eth_getTransactionCount", "0x1")
	n.on("eth_sendRawTransaction", func([]json.RawMessage) (any, *RPCError) {
		return nil, &RPCError{Code: -32000, Message: "nonce too low"}
	})
	to := MustParseAddress(anvilAddr0)
	_, err := newTestSender(t, n).Send(context.Background(), &to, nil)
	var re *RPCError
	if !errors.As(err, &re) || n.count("eth_sendRawTransaction") != 2 {
		t.Fatalf("err = %v, sends = %d (want 2)", err, n.count("eth_sendRawTransaction"))
	}
}

func TestReadOnlyBindingsRefuseToSend(t *testing.T) {
	n := newFakeNode(t)
	e := NewEscrowContract(MustParseAddress(anvilAddr0), n.rpc(), nil)
	if _, err := e.Deposit(context.Background(), big.NewInt(1)); !errors.Is(err, ErrABI) {
		t.Fatalf("err = %v", err)
	}
}

func TestPostPeriodRejectsMismatchedListsBeforeSending(t *testing.T) {
	n := standardNode(t)
	s := newTestSender(t, n)
	e := NewEscrowContract(MustParseAddress(anvilAddr0), n.rpc(), s)
	_, err := e.PostPeriod(context.Background(), 1, [32]byte{},
		[]Address{{}}, nil, []Address{{}}, []*big.Int{big.NewInt(1)})
	if !errors.Is(err, ErrABI) || n.count("eth_estimateGas") != 0 {
		t.Fatalf("err = %v, estimates = %d", err, n.count("eth_estimateGas"))
	}
}
