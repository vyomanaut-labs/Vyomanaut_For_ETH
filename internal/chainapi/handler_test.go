package chainapi

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chainbind"
)

const testChainID = 1874

type memWallets struct {
	byOwner  map[uuid.UUID]chain.Address
	byWallet map[chain.Address]uuid.UUID
	failWith error
}

func newMem() *memWallets {
	return &memWallets{byOwner: map[uuid.UUID]chain.Address{}, byWallet: map[chain.Address]uuid.UUID{}}
}

func (m *memWallets) BindOwner(_ context.Context, o uuid.UUID, w chain.Address) (bool, error) {
	if m.failWith != nil {
		return false, m.failWith
	}
	if cur, ok := m.byOwner[o]; ok {
		if cur == w {
			return true, nil
		}
		return false, chainbind.ErrAlreadyBound
	}
	if _, taken := m.byWallet[w]; taken {
		return false, chainbind.ErrWalletTaken
	}
	m.byOwner[o], m.byWallet[w] = w, o
	return false, nil
}

func (m *memWallets) OwnerWallet(_ context.Context, o uuid.UUID) (chain.Address, bool, error) {
	w, ok := m.byOwner[o]
	return w, ok, nil
}

type fixture struct {
	srv     *httptest.Server
	wallets *memWallets
	owner   uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{wallets: newMem(), owner: uuid.New()}
	verify := func(token string) (uuid.UUID, string, error) {
		switch token {
		case "owner-token":
			return f.owner, "owner", nil
		case "provider-token":
			return uuid.New(), "provider", nil
		}
		return uuid.Nil, "", errors.New("bad token")
	}
	f.srv = httptest.NewServer(New(Deps{
		Wallets: f.wallets, Verify: verify,
		Info: Info{
			ChainID: testChainID, Escrow: chain.MustParseAddress("0xBcE841C78c6c40C551fc223Ba9415D4751Af5958"),
			Token: chain.MustParseAddress("0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7"),
		},
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) do(t *testing.T, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func signedBody(t *testing.T, k *chain.Key, owner uuid.UUID, chainID uint64) string {
	t.Helper()
	sig := k.Sign(chain.PersonalMessageHash([]byte(chainbind.BindingMessage(chainID, owner, k.Address()))))
	b, _ := json.Marshal(map[string]string{"wallet": k.Address().Hex(), "signature": "0x" + hex.EncodeToString(sig.Bytes())})
	return string(b)
}

// newKey returns a different valid key for each salt.
func newKey(t *testing.T, salt byte) *chain.Key {
	t.Helper()
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i*7+3) ^ salt
	}
	k, err := chain.ParseKeyHex(hex.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestInfoNeedsNoLogin(t *testing.T) {
	f := newFixture(t)
	code, out := f.do(t, "GET", "/chain/v1/info", "", "")
	escrow, _ := out["escrow"].(string)
	if code != 200 || out["chain_id"] != float64(testChainID) || !strings.HasPrefix(escrow, "0xBcE8") {
		t.Fatalf("info = %d %v", code, out)
	}
	if _, has := out["anchor"]; has {
		t.Fatal("anchoring is off: no anchor address should be shown")
	}
}

func TestWalletRoutesNeedAnOwnerSession(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct{ method, path, token string }{
		{"GET", "/chain/v1/wallet", ""},
		{"GET", "/chain/v1/wallet", "garbage"},
		{"GET", "/chain/v1/wallet", "provider-token"},
		{"POST", "/chain/v1/wallet", ""},
		{"GET", "/chain/v1/wallet/message?wallet=0x0000000000000000000000000000000000000001", "provider-token"},
	} {
		if code, _ := f.do(t, tc.method, tc.path, tc.token, "{}"); code != http.StatusUnauthorized {
			t.Errorf("%s %s with %q: %d, want 401", tc.method, tc.path, tc.token, code)
		}
	}
}

func TestAnOwnerBindsAWalletTheyProveTheyControl(t *testing.T) {
	f := newFixture(t)
	k := newKey(t, 1)

	code, out := f.do(t, "GET", "/chain/v1/wallet", "owner-token", "")
	if code != 200 || out["wallet"] != nil {
		t.Fatalf("before binding: %d %v", code, out)
	}
	code, out = f.do(t, "POST", "/chain/v1/wallet", "owner-token", signedBody(t, k, f.owner, testChainID))
	if code != 200 || out["status"] != "bound" || out["wallet"] != k.Address().String() {
		t.Fatalf("bind: %d %v", code, out)
	}
	if f.wallets.byOwner[f.owner] != k.Address() {
		t.Fatal("the wallet was not stored")
	}
	code, out = f.do(t, "GET", "/chain/v1/wallet", "owner-token", "")
	if code != 200 || out["wallet"] != k.Address().String() {
		t.Fatalf("after binding: %d %v", code, out)
	}
	// Doing it again is harmless.
	code, out = f.do(t, "POST", "/chain/v1/wallet", "owner-token", signedBody(t, k, f.owner, testChainID))
	if code != 200 || out["status"] != "already_bound" {
		t.Fatalf("repeat: %d %v", code, out)
	}
}

func TestBindingRefusesSignaturesThatDoNotProveControl(t *testing.T) {
	f := newFixture(t)
	k, other := newKey(t, 1), newKey(t, 2)

	// Signed for a different owner, a different chain, or by a different key.
	wrongOwner := signedBody(t, k, uuid.New(), testChainID)
	wrongChain := signedBody(t, k, f.owner, 1)
	sigByOther := func() string {
		sig := other.Sign(chain.PersonalMessageHash([]byte(chainbind.BindingMessage(testChainID, f.owner, k.Address()))))
		b, _ := json.Marshal(map[string]string{"wallet": k.Address().Hex(), "signature": hex.EncodeToString(sig.Bytes())})
		return string(b)
	}()
	for name, body := range map[string]string{"wrong owner": wrongOwner, "wrong chain": wrongChain, "wrong signer": sigByOther} {
		if code, _ := f.do(t, "POST", "/chain/v1/wallet", "owner-token", body); code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, code)
		}
	}
	if len(f.wallets.byOwner) != 0 {
		t.Fatal("nothing may be bound after refused signatures")
	}
}

func TestBadRequestsAreExplained(t *testing.T) {
	f := newFixture(t)
	for name, body := range map[string]string{
		"not json":   "nope",
		"bad wallet": `{"wallet":"0x12","signature":"0x00"}`,
		"bad hex":    `{"wallet":"0x0000000000000000000000000000000000000001","signature":"zz"}`,
		"short sig":  `{"wallet":"0x0000000000000000000000000000000000000001","signature":"0x1234"}`,
		"too large":  `{"wallet":"` + strings.Repeat("a", 10000) + `"}`,
	} {
		if code, out := f.do(t, "POST", "/chain/v1/wallet", "owner-token", body); code != http.StatusBadRequest || out["message"] == nil {
			t.Errorf("%s: %d %v, want 400 with a message", name, code, out)
		}
	}
}

func TestConflictsAndFailuresMapToStatusCodes(t *testing.T) {
	f := newFixture(t)
	k := newKey(t, 1)
	body := signedBody(t, k, f.owner, testChainID)
	for err, want := range map[error]int{
		chainbind.ErrAlreadyBound:  http.StatusConflict,
		chainbind.ErrWalletTaken:   http.StatusConflict,
		chainbind.ErrUnknownOwner:  http.StatusNotFound,
		errors.New("disk on fire"): http.StatusInternalServerError,
	} {
		f.wallets.failWith = err
		code, out := f.do(t, "POST", "/chain/v1/wallet", "owner-token", body)
		if code != want {
			t.Errorf("%v: %d, want %d", err, code, want)
		}
		if msg, _ := out["message"].(string); strings.Contains(msg, "disk on fire") {
			t.Errorf("an internal error must not reach the client: %q", msg)
		}
	}
}

func TestTheMessageEndpointReturnsExactlyWhatMustBeSigned(t *testing.T) {
	f := newFixture(t)
	k := newKey(t, 1)
	code, out := f.do(t, "GET", "/chain/v1/wallet/message?wallet="+k.Address().Hex(), "owner-token", "")
	if code != 200 || out["message"] != chainbind.BindingMessage(testChainID, f.owner, k.Address()) {
		t.Fatalf("message: %d %v", code, out)
	}
	if code, _ := f.do(t, "GET", "/chain/v1/wallet/message?wallet=nope", "owner-token", ""); code != http.StatusBadRequest {
		t.Fatalf("bad wallet: %d", code)
	}
}
