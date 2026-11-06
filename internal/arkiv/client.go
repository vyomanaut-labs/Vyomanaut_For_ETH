package arkiv

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	// DefaultMinLifetimeBlocks is 30 days at the 2-second Rabdi block time.
	DefaultMinLifetimeBlocks uint64 = 1_296_000
	// MaxLifetimeBlocks is the protocol maximum (about 182.5 days).
	MaxLifetimeBlocks uint64 = 7_884_000

	defaultReceiptTimeout = 2 * time.Minute
	defaultPollInterval   = time.Second
)

var (
	// ErrReverted means the create transaction was mined but failed.
	ErrReverted = errors.New("arkiv: transaction reverted")
	// ErrNoEntityLog means the receipt had no EntityCreated log from Arkiv.
	ErrNoEntityLog = errors.New("arkiv: receipt has no EntityCreated log")

	topicEntityCreated = crypto.Keccak256Hash([]byte("EntityCreated(bytes32,address,uint64,uint8)"))
)

func keccak(b []byte) common.Hash { return crypto.Keccak256Hash(b) }

// Backend is the part of an Ethereum client that Client uses. *ethclient.Client
// satisfies it; tests use a fake.
type Backend interface {
	ChainID(ctx context.Context) (*big.Int, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}

// Querier makes raw JSON-RPC calls (used for arkiv_query). *rpc.Client satisfies it.
type Querier interface {
	CallContext(ctx context.Context, result any, method string, args ...any) error
}

// Options configures a Client.
type Options struct {
	// Key signs every transaction; its address owns every entity created.
	Key *ecdsa.PrivateKey
	// MinLifetimeBlocks is how long entities live; 0 means DefaultMinLifetimeBlocks.
	MinLifetimeBlocks uint64
	// ReceiptTimeout bounds the wait for a receipt; 0 means two minutes.
	ReceiptTimeout time.Duration
	// PollInterval is the receipt polling interval; 0 means one second.
	PollInterval time.Duration
}

// Client creates and queries Arkiv entities with one wallet.
type Client struct {
	be             Backend
	q              Querier
	key            *ecdsa.PrivateKey
	owner          common.Address
	minLifetime    uint64
	receiptTimeout time.Duration
	pollInterval   time.Duration
	closeFn        func()

	mu sync.Mutex // one writer: transactions leave in nonce order
}

// New builds a Client over an existing backend and querier.
func New(be Backend, q Querier, o Options) (*Client, error) {
	if be == nil || q == nil {
		return nil, errors.New("arkiv: backend and querier are required")
	}
	if o.Key == nil {
		return nil, errors.New("arkiv: a wallet key is required")
	}
	if o.MinLifetimeBlocks == 0 {
		o.MinLifetimeBlocks = DefaultMinLifetimeBlocks
	}
	if o.MinLifetimeBlocks > MaxLifetimeBlocks {
		return nil, fmt.Errorf("arkiv: lifetime %d blocks exceeds the protocol maximum %d", o.MinLifetimeBlocks, MaxLifetimeBlocks)
	}
	if o.ReceiptTimeout <= 0 {
		o.ReceiptTimeout = defaultReceiptTimeout
	}
	if o.PollInterval <= 0 {
		o.PollInterval = defaultPollInterval
	}
	return &Client{
		be:             be,
		q:              q,
		key:            o.Key,
		owner:          crypto.PubkeyToAddress(o.Key.PublicKey),
		minLifetime:    o.MinLifetimeBlocks,
		receiptTimeout: o.ReceiptTimeout,
		pollInterval:   o.PollInterval,
	}, nil
}

// Dial connects to an RPC endpoint. The access key, if any, is sent in the
// X-API-KEY header; prefer that over putting it in the URL path.
func Dial(ctx context.Context, rpcURL, accessKey string, o Options) (*Client, error) {
	var opts []rpc.ClientOption
	if accessKey != "" {
		opts = append(opts, rpc.WithHeader("X-API-KEY", accessKey))
	}
	rc, err := rpc.DialOptions(ctx, rpcURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("arkiv: dial: %w", err)
	}
	c, err := New(ethclient.NewClient(rc), rc, o)
	if err != nil {
		rc.Close()
		return nil, err
	}
	c.closeFn = rc.Close
	return c, nil
}

// DialReadOnly connects for queries only (no wallet). Create on the result
// fails; Query, ByKey and ChainID work.
func DialReadOnly(ctx context.Context, rpcURL, accessKey string) (*Client, error) {
	var opts []rpc.ClientOption
	if accessKey != "" {
		opts = append(opts, rpc.WithHeader("X-API-KEY", accessKey))
	}
	rc, err := rpc.DialOptions(ctx, rpcURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("arkiv: dial: %w", err)
	}
	return &Client{be: ethclient.NewClient(rc), q: rc, closeFn: rc.Close}, nil
}

// Close releases the connection made by Dial. It is a no-op for New.
func (c *Client) Close() {
	if c.closeFn != nil {
		c.closeFn()
	}
}

// Owner is the wallet address that owns every entity this client creates.
func (c *Client) Owner() common.Address { return c.owner }

// ChainID is the chain the node reports.
func (c *Client) ChainID(ctx context.Context) (*big.Int, error) { return c.be.ChainID(ctx) }

// Balance is the wallet's native balance in wei (tGLM on Rabdi).
func (c *Client) Balance(ctx context.Context) (*big.Int, error) {
	return c.be.BalanceAt(ctx, c.owner, nil)
}

// CreateResult describes one mined create.
type CreateResult struct {
	Key     common.Hash
	TxHash  common.Hash
	GasUsed uint64
	FeeWei  *big.Int
}

// Create writes one entity and waits for its receipt. It is safe to call from
// several goroutines; the calls are serialised.
func (c *Client) Create(ctx context.Context, attrs []Attribute) (CreateResult, error) {
	if c.key == nil {
		return CreateResult{}, errors.New("arkiv: this client is read-only")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	salt, err := NewSalt()
	if err != nil {
		return CreateResult{}, fmt.Errorf("arkiv: salt: %w", err)
	}
	data, err := EncodeCreate(attrs, salt, c.minLifetime, 0)
	if err != nil {
		return CreateResult{}, err
	}
	calldata, err := BuildExecuteCalldata(data)
	if err != nil {
		return CreateResult{}, err
	}
	chainID, err := c.be.ChainID(ctx)
	if err != nil {
		return CreateResult{}, fmt.Errorf("arkiv: chain id: %w", err)
	}
	to := Address
	gas, err := c.be.EstimateGas(ctx, ethereum.CallMsg{From: c.owner, To: &to, Value: big.NewInt(0), Data: calldata})
	if err != nil {
		return CreateResult{}, fmt.Errorf("arkiv: estimate gas: %w", err)
	}
	hash, err := c.send(ctx, chainID, calldata, gas+gas/5)
	if err != nil {
		return CreateResult{}, err
	}
	rcpt, err := c.waitReceipt(ctx, hash)
	if err != nil {
		return CreateResult{TxHash: hash}, err
	}
	res := CreateResult{TxHash: hash, GasUsed: rcpt.GasUsed, FeeWei: new(big.Int)}
	if rcpt.EffectiveGasPrice != nil {
		res.FeeWei.Mul(new(big.Int).SetUint64(rcpt.GasUsed), rcpt.EffectiveGasPrice)
	}
	if rcpt.Status != types.ReceiptStatusSuccessful {
		return res, fmt.Errorf("%w (tx %s)", ErrReverted, hash.Hex())
	}
	for _, lg := range rcpt.Logs {
		if lg.Address == Address && len(lg.Topics) >= 2 && lg.Topics[0] == topicEntityCreated {
			res.Key = lg.Topics[1]
			return res, nil
		}
	}
	return res, fmt.Errorf("%w (tx %s)", ErrNoEntityLog, hash.Hex())
}

func (c *Client) send(ctx context.Context, chainID *big.Int, calldata []byte, gas uint64) (common.Hash, error) {
	nonce, err := c.be.PendingNonceAt(ctx, c.owner)
	if err != nil {
		return common.Hash{}, fmt.Errorf("arkiv: nonce: %w", err)
	}
	head, err := c.be.HeaderByNumber(ctx, nil)
	if err != nil {
		return common.Hash{}, fmt.Errorf("arkiv: head: %w", err)
	}
	to := Address
	var tx *types.Transaction
	if head.BaseFee != nil {
		tip, err := c.be.SuggestGasTipCap(ctx)
		if err != nil {
			return common.Hash{}, fmt.Errorf("arkiv: gas tip: %w", err)
		}
		feeCap := new(big.Int).Add(new(big.Int).Mul(head.BaseFee, big.NewInt(2)), tip)
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID: chainID, Nonce: nonce, GasTipCap: tip, GasFeeCap: feeCap,
			Gas: gas, To: &to, Value: big.NewInt(0), Data: calldata,
		})
	} else {
		price, err := c.be.SuggestGasPrice(ctx)
		if err != nil {
			return common.Hash{}, fmt.Errorf("arkiv: gas price: %w", err)
		}
		tx = types.NewTx(&types.LegacyTx{
			Nonce: nonce, GasPrice: price, Gas: gas, To: &to, Value: big.NewInt(0), Data: calldata,
		})
	}
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), c.key)
	if err != nil {
		return common.Hash{}, fmt.Errorf("arkiv: sign: %w", err)
	}
	if err := c.be.SendTransaction(ctx, signed); err != nil {
		return common.Hash{}, fmt.Errorf("arkiv: send: %w", err)
	}
	return signed.Hash(), nil
}

func (c *Client) waitReceipt(ctx context.Context, h common.Hash) (*types.Receipt, error) {
	deadline := time.NewTimer(c.receiptTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(c.pollInterval)
	defer tick.Stop()
	for {
		r, err := c.be.TransactionReceipt(ctx, h)
		if err == nil {
			return r, nil
		}
		if !errors.Is(err, ethereum.NotFound) {
			return nil, fmt.Errorf("arkiv: receipt: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("arkiv: no receipt for %s within %s", h.Hex(), c.receiptTimeout)
		case <-tick.C:
		}
	}
}

// ---------- reading ----------

// Attr is one attribute as arkiv_query returns it. Value is raw JSON: a string
// for str, a hex string for u64 and bytes.
type Attr struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// Entity is one arkiv_query row.
type Entity struct {
	Key        string `json:"key"`
	Owner      string `json:"owner"`
	ExpiresAt  string `json:"expiresAt"`
	Attributes []Attr `json:"attributes"`
}

type queryResult struct {
	Data []Entity `json:"data"`
}

// Query runs an arkiv_query and returns up to limit rows (1 to 100) with the
// named attributes. The paging offset is sent as a hex string like limit; that
// form has not been exercised on chain yet (spike A0 only used limit), so the
// verifier session (A5) must confirm it before relying on it.
func (c *Client) Query(ctx context.Context, query string, attrNames []string, limit, offset int) ([]Entity, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("arkiv: query limit %d out of range 1..100", limit)
	}
	if offset < 0 {
		return nil, errors.New("arkiv: query offset must not be negative")
	}
	sel := map[string]any{"key": true, "owner": true, "expiresAt": true}
	if len(attrNames) > 0 {
		m := make(map[string]bool, len(attrNames))
		for _, n := range attrNames {
			m[n] = true
		}
		sel["attributes"] = m
	}
	opts := map[string]any{"select": sel, "limit": "0x" + strconv.FormatInt(int64(limit), 16)}
	if offset > 0 {
		opts["offset"] = "0x" + strconv.FormatInt(int64(offset), 16)
	}
	var res queryResult
	if err := c.q.CallContext(ctx, &res, "arkiv_query", query, opts); err != nil {
		return nil, fmt.Errorf("arkiv: query: %w", err)
	}
	return res.Data, nil
}

func (e Entity) attr(name string) (Attr, bool) {
	for _, a := range e.Attributes {
		if a.Name == name {
			return a, true
		}
	}
	return Attr{}, false
}

// Str returns a str attribute's value.
func (e Entity) Str(name string) (string, bool) {
	a, ok := e.attr(name)
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(a.Value, &s) != nil {
		return "", false
	}
	return s, true
}

// U64 returns a u64 attribute's value (returned as a hex string by the RPC).
func (e Entity) U64(name string) (uint64, error) {
	a, ok := e.attr(name)
	if !ok {
		return 0, fmt.Errorf("arkiv: attribute %q missing", name)
	}
	var s string
	if err := json.Unmarshal(a.Value, &s); err != nil {
		return 0, fmt.Errorf("arkiv: attribute %q: %w", name, err)
	}
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}

// Bytes returns a bytes attribute's value (returned as a hex string by the RPC).
func (e Entity) Bytes(name string) ([]byte, error) {
	a, ok := e.attr(name)
	if !ok {
		return nil, fmt.Errorf("arkiv: attribute %q missing", name)
	}
	var s string
	if err := json.Unmarshal(a.Value, &s); err != nil {
		return nil, fmt.Errorf("arkiv: attribute %q: %w", name, err)
	}
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}
