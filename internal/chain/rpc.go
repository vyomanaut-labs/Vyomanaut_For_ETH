package chain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const (
	maxResponseBytes  = 8 << 20
	defaultRPCTimeout = 20 * time.Second
	hexPrefix         = "0x"
	gwei              = 1_000_000_000
)

// RPC is a JSON-RPC client for one endpoint. Errors it returns never contain
// the endpoint URL, because that URL may carry an API key.
type RPC struct {
	endpoint string
	hc       *http.Client
	nextID   atomic.Uint64
}

// NewRPC returns a client for endpoint. A zero timeout means 20 seconds.
func NewRPC(endpoint string, timeout time.Duration) *RPC {
	if timeout <= 0 {
		timeout = defaultRPCTimeout
	}
	return &RPC{endpoint: endpoint, hc: &http.Client{Timeout: timeout}}
}

// RPCError is a JSON-RPC error object returned by the node.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("chain: node error %d: %s", e.Code, e.Message) }

// revertData returns the raw revert bytes when the node attached them.
func (e *RPCError) revertData() []byte {
	var s string
	if err := json.Unmarshal(e.Data, &s); err != nil {
		return nil
	}
	b, err := hex.DecodeString(strings.TrimPrefix(s, hexPrefix))
	if err != nil {
		return nil
	}
	return b
}

func (e *RPCError) isRevert() bool {
	return strings.Contains(strings.ToLower(e.Message), "revert")
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

// sanitize drops the URL a net/http error carries.
func sanitize(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

func (r *RPC) call(ctx context.Context, method string, params []any, out any) error {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: r.nextID.Add(1), Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrRPC, method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: %s: bad endpoint", ErrRPC, method)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %s", ErrRPC, method, sanitize(err))
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: %s: reading reply: %s", ErrRPC, method, sanitize(err))
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("%w: %s: reply larger than %d bytes", ErrRPC, method, maxResponseBytes)
	}
	var env rpcResponse
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("%w: %s: HTTP %d, unreadable reply", ErrRPC, method, resp.StatusCode)
	}
	if env.Error != nil {
		return env.Error
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s: HTTP %d", ErrRPC, method, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("%w: %s: unexpected result shape", ErrRPC, method)
	}
	return nil
}

// ── hex helpers ──────────────────────────────────────────────────────────

func hexQuantity(s string) (*big.Int, error) {
	t := strings.TrimPrefix(s, hexPrefix)
	if t == "" {
		return nil, fmt.Errorf("%w: empty quantity", ErrRPC)
	}
	v, ok := new(big.Int).SetString(t, hexBase)
	if !ok {
		return nil, fmt.Errorf("%w: bad quantity %q", ErrRPC, s)
	}
	return v, nil
}

func hexUint64(s string) (uint64, error) {
	v, err := hexQuantity(s)
	if err != nil {
		return 0, err
	}
	if !v.IsUint64() {
		return 0, fmt.Errorf("%w: quantity %q does not fit in 64 bits", ErrRPC, s)
	}
	return v.Uint64(), nil
}

func hexData(s string) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(s, hexPrefix))
	if err != nil {
		return nil, fmt.Errorf("%w: bad hex data", ErrRPC)
	}
	return b, nil
}

func quantity(v uint64) string { return hexPrefix + new(big.Int).SetUint64(v).Text(hexBase) }

// ── queries ──────────────────────────────────────────────────────────────

// ChainID returns eth_chainId.
func (r *RPC) ChainID(ctx context.Context) (uint64, error) {
	var s string
	if err := r.call(ctx, "eth_chainId", nil, &s); err != nil {
		return 0, err
	}
	return hexUint64(s)
}

// BlockNumber returns the latest block number.
func (r *RPC) BlockNumber(ctx context.Context) (uint64, error) {
	var s string
	if err := r.call(ctx, "eth_blockNumber", nil, &s); err != nil {
		return 0, err
	}
	return hexUint64(s)
}

// Balance returns the native-token balance of a in wei.
func (r *RPC) Balance(ctx context.Context, a Address) (*big.Int, error) {
	var s string
	if err := r.call(ctx, "eth_getBalance", []any{a.Hex(), "latest"}, &s); err != nil {
		return nil, err
	}
	return hexQuantity(s)
}

// PendingNonce returns the next nonce for a, counting pending transactions.
func (r *RPC) PendingNonce(ctx context.Context, a Address) (uint64, error) {
	var s string
	if err := r.call(ctx, "eth_getTransactionCount", []any{a.Hex(), "pending"}, &s); err != nil {
		return 0, err
	}
	return hexUint64(s)
}

// FeeCaps returns a priority fee and a fee cap that leaves room for the base
// fee to double before the transaction would stall.
func (r *RPC) FeeCaps(ctx context.Context) (tip, feeCap *big.Int, err error) {
	var blk struct {
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	if err = r.call(ctx, "eth_getBlockByNumber", []any{"latest", false}, &blk); err != nil {
		return nil, nil, err
	}
	base := new(big.Int)
	if blk.BaseFeePerGas != "" {
		if base, err = hexQuantity(blk.BaseFeePerGas); err != nil {
			return nil, nil, err
		}
	}
	tip = big.NewInt(gwei)
	var tipHex string
	if err := r.call(ctx, "eth_maxPriorityFeePerGas", nil, &tipHex); err == nil {
		if v, perr := hexQuantity(tipHex); perr == nil && v.Sign() > 0 {
			tip = v
		}
	}
	feeCap = new(big.Int).Lsh(base, 1)
	feeCap.Add(feeCap, tip)
	return tip, feeCap, nil
}

type callObject struct {
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Data  string `json:"data,omitempty"`
	Value string `json:"value,omitempty"`
}

func newCallObject(from *Address, to *Address, data []byte) callObject {
	c := callObject{Data: hexPrefix + hex.EncodeToString(data)}
	if from != nil {
		c.From = from.Hex()
	}
	if to != nil {
		c.To = to.Hex()
	}
	return c
}

// asRevert converts a node error into a RevertError when it is one.
func asRevert(err error) error {
	var re *RPCError
	if errors.As(err, &re) && re.isRevert() {
		data := re.revertData()
		reason := re.Message
		if len(data) > 0 {
			reason = DescribeRevert(data)
		}
		return &RevertError{Reason: reason, Data: data}
	}
	return err
}

// EstimateGas simulates the call and returns the gas it needs. A call that
// would revert returns a *RevertError.
func (r *RPC) EstimateGas(ctx context.Context, from Address, to *Address, data []byte) (uint64, error) {
	var s string
	if err := r.call(ctx, "eth_estimateGas", []any{newCallObject(&from, to, data)}, &s); err != nil {
		return 0, asRevert(err)
	}
	return hexUint64(s)
}

// Call runs a read-only call at the latest block. from may be nil.
func (r *RPC) Call(ctx context.Context, from *Address, to Address, data []byte) ([]byte, error) {
	var s string
	if err := r.call(ctx, "eth_call", []any{newCallObject(from, &to, data), "latest"}, &s); err != nil {
		return nil, asRevert(err)
	}
	return hexData(s)
}

// SendRaw submits a signed transaction and returns its hash.
func (r *RPC) SendRaw(ctx context.Context, raw []byte) ([hashLen]byte, error) {
	var out [hashLen]byte
	var s string
	if err := r.call(ctx, "eth_sendRawTransaction", []any{hexPrefix + hex.EncodeToString(raw)}, &s); err != nil {
		return out, err
	}
	b, err := hexData(s)
	if err != nil || len(b) != hashLen {
		return out, fmt.Errorf("%w: unexpected transaction hash", ErrRPC)
	}
	copy(out[:], b)
	return out, nil
}

// Log is one event log.
type Log struct {
	Address     Address
	Topics      [][hashLen]byte
	Data        []byte
	BlockNumber uint64
	TxHash      [hashLen]byte
	LogIndex    uint64
}

// Receipt is the result of a mined transaction.
type Receipt struct {
	TxHash      [hashLen]byte
	Status      uint64
	BlockNumber uint64
	GasUsed     uint64
	Logs        []Log
	// ContractAddress is set when the transaction created a contract.
	ContractAddress *Address
}

type rawLog struct {
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	BlockNumber string   `json:"blockNumber"`
	TxHash      string   `json:"transactionHash"`
	LogIndex    string   `json:"logIndex"`
}

func (l rawLog) parse() (Log, error) {
	var out Log
	a, err := ParseAddress(l.Address)
	if err != nil {
		return out, fmt.Errorf("%w: log address", ErrRPC)
	}
	out.Address = a
	for _, t := range l.Topics {
		b, herr := hexData(t)
		if herr != nil || len(b) != hashLen {
			return out, fmt.Errorf("%w: log topic", ErrRPC)
		}
		var topic [hashLen]byte
		copy(topic[:], b)
		out.Topics = append(out.Topics, topic)
	}
	if out.Data, err = hexData(l.Data); err != nil {
		return out, err
	}
	if out.BlockNumber, err = hexUint64(l.BlockNumber); err != nil {
		return out, err
	}
	if out.LogIndex, err = hexUint64(l.LogIndex); err != nil {
		return out, err
	}
	h, err := hexData(l.TxHash)
	if err != nil || len(h) != hashLen {
		return out, fmt.Errorf("%w: log transaction hash", ErrRPC)
	}
	copy(out.TxHash[:], h)
	return out, nil
}

// Receipt returns the receipt of a transaction, or nil if it is not mined yet.
func (r *RPC) Receipt(ctx context.Context, hash [hashLen]byte) (*Receipt, error) {
	var raw *struct {
		TxHash          string   `json:"transactionHash"`
		Status          string   `json:"status"`
		BlockNumber     string   `json:"blockNumber"`
		GasUsed         string   `json:"gasUsed"`
		ContractAddress *string  `json:"contractAddress"`
		Logs            []rawLog `json:"logs"`
	}
	if err := r.call(ctx, "eth_getTransactionReceipt", []any{hexPrefix + hex.EncodeToString(hash[:])}, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil //nolint:nilnil // nil means "not mined yet"
	}
	rc := &Receipt{TxHash: hash}
	var err error
	if rc.Status, err = hexUint64(raw.Status); err != nil {
		return nil, err
	}
	if rc.BlockNumber, err = hexUint64(raw.BlockNumber); err != nil {
		return nil, err
	}
	if rc.GasUsed, err = hexUint64(raw.GasUsed); err != nil {
		return nil, err
	}
	if raw.ContractAddress != nil {
		a, perr := ParseAddress(*raw.ContractAddress)
		if perr != nil {
			return nil, perr
		}
		rc.ContractAddress = &a
	}
	for _, l := range raw.Logs {
		pl, perr := l.parse()
		if perr != nil {
			return nil, perr
		}
		rc.Logs = append(rc.Logs, pl)
	}
	return rc, nil
}

// Logs returns logs emitted by addr whose first topic is topic0, in blocks
// from..to inclusive.
func (r *RPC) Logs(ctx context.Context, addr Address, topic0 [hashLen]byte, from, to uint64) ([]Log, error) {
	filter := map[string]any{
		"address":   addr.Hex(),
		"fromBlock": quantity(from),
		"toBlock":   quantity(to),
		"topics":    []any{hexPrefix + hex.EncodeToString(topic0[:])},
	}
	var raw []rawLog
	if err := r.call(ctx, "eth_getLogs", []any{filter}, &raw); err != nil {
		return nil, err
	}
	out := make([]Log, 0, len(raw))
	for _, l := range raw {
		pl, err := l.parse()
		if err != nil {
			return nil, err
		}
		out = append(out, pl)
	}
	return out, nil
}

// FinalizedBlock returns the number of the node's finalized block, or
// ErrNoFinality if it does not report one.
func (r *RPC) FinalizedBlock(ctx context.Context) (uint64, error) {
	var blk *struct {
		Number string `json:"number"`
	}
	if err := r.call(ctx, "eth_getBlockByNumber", []any{"finalized", false}, &blk); err != nil {
		var re *RPCError
		if errors.As(err, &re) {
			return 0, ErrNoFinality
		}
		return 0, err
	}
	if blk == nil {
		return 0, ErrNoFinality
	}
	return hexUint64(blk.Number)
}
