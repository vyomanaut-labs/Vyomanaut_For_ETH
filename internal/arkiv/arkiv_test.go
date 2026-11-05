package arkiv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// ---------- helpers ----------

func mustAttr(a Attribute, err error) Attribute {
	if err != nil {
		panic(err)
	}
	return a
}

type fakeBackend struct {
	chainID       *big.Int
	baseFee       *big.Int // nil exercises the legacy-transaction path
	status        uint64
	logs          []*types.Log
	notFoundFirst int
	estimateErr   error

	receiptCalls int
	sent         []*types.Transaction
}

func (f *fakeBackend) ChainID(context.Context) (*big.Int, error) { return f.chainID, nil }
func (f *fakeBackend) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return uint64(len(f.sent)), nil
}
func (f *fakeBackend) HeaderByNumber(context.Context, *big.Int) (*types.Header, error) {
	return &types.Header{BaseFee: f.baseFee}, nil
}
func (f *fakeBackend) SuggestGasTipCap(context.Context) (*big.Int, error) { return big.NewInt(1), nil }
func (f *fakeBackend) SuggestGasPrice(context.Context) (*big.Int, error)  { return big.NewInt(1), nil }
func (f *fakeBackend) EstimateGas(context.Context, ethereum.CallMsg) (uint64, error) {
	if f.estimateErr != nil {
		return 0, f.estimateErr
	}
	return 100000, nil
}
func (f *fakeBackend) SendTransaction(_ context.Context, tx *types.Transaction) error {
	f.sent = append(f.sent, tx)
	return nil
}
func (f *fakeBackend) TransactionReceipt(_ context.Context, h common.Hash) (*types.Receipt, error) {
	f.receiptCalls++
	if f.receiptCalls <= f.notFoundFirst {
		return nil, ethereum.NotFound
	}
	return &types.Receipt{Status: f.status, GasUsed: 90000, EffectiveGasPrice: big.NewInt(1e9), Logs: f.logs, TxHash: h}, nil
}
func (f *fakeBackend) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	return big.NewInt(5), nil
}

type fakeQuerier struct {
	reply  string
	method string
	args   []any
}

func (f *fakeQuerier) CallContext(_ context.Context, result any, method string, args ...any) error {
	f.method, f.args = method, args
	return json.Unmarshal([]byte(f.reply), result)
}

func testAttrs() []Attribute {
	return []Attribute{
		mustAttr(NewStr("kind", "spike", true)),
		mustAttr(NewBytes("body", []byte("hello"))),
		mustAttr(NewStr("app", "vyomanaut-demo", true)),
		mustAttr(NewU64("seq", 2, true)),
	}
}

func newTestClient(t *testing.T, fb *fakeBackend, fq *fakeQuerier) *Client {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(fb, fq, Options{Key: key, PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func createdLog(key common.Hash) *types.Log {
	return &types.Log{Address: Address, Topics: []common.Hash{topicEntityCreated, key, {}}}
}

// ---------- protocol constants ----------

func TestExecuteSelectorMatchesSignature(t *testing.T) {
	got := crypto.Keccak256([]byte(executeSignature))[:4]
	if !bytes.Equal(got, executeSelector[:]) {
		t.Fatalf("selector for %s = %x, constant is %x", executeSignature, got, executeSelector)
	}
}

func TestEntityCreatedTopic(t *testing.T) {
	// Cross-checked with an independent keccak implementation.
	const want = "0xb282d7c494b8899aa8015cd07be621530beb03409eb8c5e8fdc1411ba64356a5"
	if topicEntityCreated.Hex() != want {
		t.Fatalf("EntityCreated topic = %s, want %s", topicEntityCreated.Hex(), want)
	}
}

func TestPredictKeyGoldenVector(t *testing.T) {
	// chain 7794095, owner 0xF711..., nonce 7, salt 0x1234; the expected value
	// was computed with an independent keccak implementation (not this code).
	owner := common.HexToAddress("0xF71112feB5EA989C95aF2348d99d5e9e42eDfAED")
	got := PredictKey(big.NewInt(7794095), owner, 7, big.NewInt(0x1234))
	const want = "0x93110a850c8497619ba2aff5e3d7bc0efdc36bb58470ef70eed6857e727b3c5c"
	if got.Hex() != want {
		t.Fatalf("PredictKey = %s, want %s", got.Hex(), want)
	}
}

// ---------- encoding ----------

func TestEncodeCreateLayoutAndSorting(t *testing.T) {
	salt := big.NewInt(0xabcdef)
	data, err := EncodeCreate(testAttrs(), salt, 1296000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data[:32], common.LeftPadBytes([]byte{0x20}, 32)) {
		t.Fatalf("first word must be the tuple offset 0x20, got %x", data[:32])
	}
	args := abi.Arguments{
		{Name: "salt", Type: tUint128}, {Name: "expiresAt", Type: tUint64}, {Name: "minLifetime", Type: tUint64},
		{Name: "flags", Type: tUint8}, {Name: "attrs", Type: tAttrs},
	}
	vals, err := args.Unpack(data[32:])
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if vals[0].(*big.Int).Cmp(salt) != 0 {
		t.Fatalf("salt = %v", vals[0])
	}
	if vals[1].(uint64) != 0 || vals[2].(uint64) != 1296000 || vals[3].(uint8) != 0 {
		t.Fatalf("expiresAt/minLifetime/flags = %v/%v/%v", vals[1], vals[2], vals[3])
	}
	rv := reflect.ValueOf(vals[4])
	if rv.Len() != 4 {
		t.Fatalf("attribute count = %d, want 4", rv.Len())
	}
	var names []string
	for i := 0; i < rv.Len(); i++ {
		n := rv.Index(i).FieldByName("Name").Interface().([32]byte)
		names = append(names, strings.TrimRight(string(n[:]), "\x00"))
	}
	if strings.Join(names, ",") != "app,body,kind,seq" {
		t.Fatalf("attributes must be sorted ascending by name, got %v", names)
	}
	body := rv.Index(1)
	if body.FieldByName("TypeId").Interface().(uint8) != TypeBytes {
		t.Fatal("body must be typeId 7 (bytes, not indexed)")
	}
	if rv.Index(0).FieldByName("TypeId").Interface().(uint8) != TypeStr|indexedBit {
		t.Fatal("app must be typeId 0x88 (indexed str)")
	}
}

func TestBuildExecuteCalldataStartsWithSelector(t *testing.T) {
	data, err := EncodeCreate(testAttrs(), big.NewInt(1), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := BuildExecuteCalldata(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(cd, executeSelector[:]) {
		t.Fatalf("calldata must start with %x, got %x", executeSelector, cd[:4])
	}
}

func TestAttributeValidation(t *testing.T) {
	big129 := strings.Repeat("x", 129)
	for _, name := range []string{"", "$reserved", strings.Repeat("n", 33), "café"} {
		if _, err := NewStr(name, "v", true); err == nil {
			t.Errorf("name %q must be rejected", name)
		}
	}
	if _, err := NewStr("ok", big129, true); err == nil {
		t.Error("a 129-byte string must be rejected")
	}

	dup := []Attribute{mustAttr(NewStr("a", "1", true)), mustAttr(NewStr("a", "2", true))}
	if _, err := EncodeCreate(dup, big.NewInt(1), 1, 0); err == nil {
		t.Error("duplicate names must be rejected")
	}

	var many []Attribute
	for i := 0; i < MaxAttributes+1; i++ {
		many = append(many, mustAttr(NewU64(fmt.Sprintf("n%02d", i), uint64(i), true)))
	}
	if _, err := EncodeCreate(many, big.NewInt(1), 1, 0); err == nil {
		t.Error("33 attributes must be rejected")
	}

	huge := []Attribute{mustAttr(NewBytes("body", make([]byte, MaxBytesTotal+1)))}
	if _, err := EncodeCreate(huge, big.NewInt(1), 1, 0); err == nil {
		t.Error("more than 128 KiB of bytes must be rejected")
	}

	indexedBytes := []Attribute{{Name: [32]byte{'b'}, TypeID: TypeBytes | indexedBit, Value: []byte{1}}}
	if _, err := EncodeCreate(indexedBytes, big.NewInt(1), 1, 0); err == nil {
		t.Error("an indexed bytes attribute must be rejected")
	}

	if _, err := EncodeCreate(nil, big.NewInt(1), 1, 0); err == nil {
		t.Error("an entity with no attributes must be rejected")
	}
	if _, err := EncodeCreate(testAttrs(), new(big.Int).Lsh(big.NewInt(1), 128), 1, 0); err == nil {
		t.Error("a salt wider than 128 bits must be rejected")
	}
}

// ---------- client ----------

func TestCreateSendsSignedExecuteTransaction(t *testing.T) {
	wantKey := common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ab")
	for _, baseFee := range []*big.Int{big.NewInt(1), nil} { // dynamic-fee and legacy paths
		fb := &fakeBackend{chainID: big.NewInt(7794095), baseFee: baseFee, status: types.ReceiptStatusSuccessful,
			notFoundFirst: 2, logs: []*types.Log{createdLog(wantKey)}}
		c := newTestClient(t, fb, &fakeQuerier{})

		res, err := c.Create(context.Background(), testAttrs())
		if err != nil {
			t.Fatalf("baseFee=%v: %v", baseFee, err)
		}
		if res.Key != wantKey {
			t.Fatalf("key = %s, want %s", res.Key, wantKey)
		}
		if len(fb.sent) != 1 {
			t.Fatalf("sent %d transactions, want 1", len(fb.sent))
		}
		tx := fb.sent[0]
		if tx.To() == nil || *tx.To() != Address || tx.Value().Sign() != 0 {
			t.Fatalf("tx must go to the Arkiv contract with value 0, got to=%v value=%v", tx.To(), tx.Value())
		}
		if !bytes.HasPrefix(tx.Data(), executeSelector[:]) {
			t.Fatal("tx data must start with the execute selector")
		}
		sender, err := types.Sender(types.LatestSignerForChainID(fb.chainID), tx)
		if err != nil || sender != c.Owner() {
			t.Fatalf("tx must be signed by the client's wallet: sender=%v err=%v", sender, err)
		}
		if tx.Gas() != 120000 {
			t.Fatalf("gas limit = %d, want estimate + 20%% = 120000", tx.Gas())
		}
		if fb.receiptCalls != 3 {
			t.Fatalf("receipt polled %d times, want 3 (two not-found, then success)", fb.receiptCalls)
		}
		if res.GasUsed != 90000 || res.FeeWei.Cmp(big.NewInt(90000*1e9)) != 0 {
			t.Fatalf("gas/fee = %d/%v", res.GasUsed, res.FeeWei)
		}
	}
}

func TestCreateReportsRevert(t *testing.T) {
	fb := &fakeBackend{chainID: big.NewInt(1), baseFee: big.NewInt(1), status: types.ReceiptStatusFailed}
	c := newTestClient(t, fb, &fakeQuerier{})
	if _, err := c.Create(context.Background(), testAttrs()); !errors.Is(err, ErrReverted) {
		t.Fatalf("err = %v, want ErrReverted", err)
	}
}

func TestCreateWithoutEntityLog(t *testing.T) {
	other := &types.Log{Address: common.HexToAddress("0x01"), Topics: []common.Hash{topicEntityCreated, {1}}}
	fb := &fakeBackend{chainID: big.NewInt(1), baseFee: big.NewInt(1), status: types.ReceiptStatusSuccessful, logs: []*types.Log{other}}
	c := newTestClient(t, fb, &fakeQuerier{})
	if _, err := c.Create(context.Background(), testAttrs()); !errors.Is(err, ErrNoEntityLog) {
		t.Fatalf("err = %v, want ErrNoEntityLog (log came from another address)", err)
	}
}

func TestCreateSendsNothingWhenEstimateFails(t *testing.T) {
	fb := &fakeBackend{chainID: big.NewInt(1), baseFee: big.NewInt(1), estimateErr: errors.New("execution reverted")}
	c := newTestClient(t, fb, &fakeQuerier{})
	if _, err := c.Create(context.Background(), testAttrs()); err == nil {
		t.Fatal("expected an error")
	}
	if len(fb.sent) != 0 {
		t.Fatal("no transaction may be sent when gas estimation fails")
	}
}

func TestCreateRejectsBadAttributesBeforeTouchingTheNetwork(t *testing.T) {
	fb := &fakeBackend{chainID: big.NewInt(1), baseFee: big.NewInt(1)}
	c := newTestClient(t, fb, &fakeQuerier{})
	if _, err := c.Create(context.Background(), nil); err == nil {
		t.Fatal("expected an error for no attributes")
	}
	if len(fb.sent) != 0 {
		t.Fatal("nothing may be sent for invalid input")
	}
}

func TestNewValidatesOptions(t *testing.T) {
	key, _ := crypto.GenerateKey()
	fb, fq := &fakeBackend{}, &fakeQuerier{}
	if _, err := New(fb, fq, Options{}); err == nil {
		t.Error("a missing key must be rejected")
	}
	if _, err := New(nil, fq, Options{Key: key}); err == nil {
		t.Error("a missing backend must be rejected")
	}
	if _, err := New(fb, fq, Options{Key: key, MinLifetimeBlocks: MaxLifetimeBlocks + 1}); err == nil {
		t.Error("a lifetime above the protocol maximum must be rejected")
	}
}

func TestQueryDecodesRows(t *testing.T) {
	fq := &fakeQuerier{reply: `{"data":[{"key":"0x01","owner":"0xabc","expiresAt":"0x10","attributes":[` +
		`{"name":"app","type":"str","value":"x"},{"name":"seq","type":"u64","value":"0x2"},` +
		`{"name":"body","type":"bytes","value":"0x68690a"}]}],"blockNumber":"0x5"}`}
	c := newTestClient(t, &fakeBackend{}, fq)

	rows, err := c.Query(context.Background(), "app = str('x')", []string{"app", "seq", "body"}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fq.method != "arkiv_query" {
		t.Fatalf("method = %s", fq.method)
	}
	opts := fq.args[1].(map[string]any)
	if opts["limit"] != "0xa" {
		t.Fatalf("limit = %v, want 0xa", opts["limit"])
	}
	if _, has := opts["offset"]; has {
		t.Fatal("offset must be omitted when zero")
	}
	if len(rows) != 1 || rows[0].Key != "0x01" {
		t.Fatalf("rows = %+v", rows)
	}
	if s, ok := rows[0].Str("app"); !ok || s != "x" {
		t.Fatalf("Str(app) = %q, %v", s, ok)
	}
	if n, err := rows[0].U64("seq"); err != nil || n != 2 {
		t.Fatalf("U64(seq) = %d, %v", n, err)
	}
	if b, err := rows[0].Bytes("body"); err != nil || string(b) != "hi\n" {
		t.Fatalf("Bytes(body) = %q, %v", b, err)
	}
	if _, err := rows[0].U64("missing"); err == nil {
		t.Fatal("a missing attribute must be an error")
	}
}

func TestQueryRejectsBadPaging(t *testing.T) {
	c := newTestClient(t, &fakeBackend{}, &fakeQuerier{reply: `{"data":[]}`})
	for _, limit := range []int{0, 101} {
		if _, err := c.Query(context.Background(), "q", nil, limit, 0); err == nil {
			t.Errorf("limit %d must be rejected", limit)
		}
	}
	if _, err := c.Query(context.Background(), "q", nil, 10, -1); err == nil {
		t.Error("a negative offset must be rejected")
	}
}
