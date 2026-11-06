package chainctl

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chainapi"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chainbind"
)

func TestParseAmountIsExactAndStrict(t *testing.T) {
	good := map[string]int64{"1": 1_000_000, "2.5": 2_500_000, "0.000001": 1, "10.": 10_000_000, ".5": 500_000, " 3 ": 3_000_000, "0.100000": 100_000}
	for in, want := range good {
		got, err := ParseAmount(in)
		if err != nil || got.Int64() != want {
			t.Errorf("%q: got %v, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", ".", "0", "0.0", "-1", "+1", "1e3", "1,5", "0.0000001", "abc", "1.2.3", "1 000"} {
		if _, err := ParseAmount(bad); !errors.Is(err, ErrBadAmount) {
			t.Errorf("%q: err = %v, want ErrBadAmount", bad, err)
		}
	}
}

func TestFormatAmountRoundTrips(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 1: "0.000001", 1_000_000: "1", 2_500_000: "2.5", 110_011_323: "110.011323", 999_999: "0.999999"} {
		if got := FormatAmount(big.NewInt(in)); got != want {
			t.Errorf("%d: %q, want %q", in, got, want)
		}
		if in > 0 {
			if back, _ := ParseAmount(FormatAmount(big.NewInt(in))); back == nil || back.Int64() != in {
				t.Errorf("%d does not round-trip", in)
			}
		}
	}
	if FormatAmount(nil) != "0" {
		t.Error("nil must format as 0")
	}
}

func TestNewKeyFileWritesAUsableOwnerOnlyKeyAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "owner.key")
	addr, err := NewKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, %v; want 0600", info.Mode(), err)
		}
	}
	k, err := chain.LoadKeyFile(path)
	if err != nil || k.Address() != addr {
		t.Fatalf("the written key does not load back to %v: %v", addr, err)
	}
	before, _ := os.ReadFile(path)
	if _, err := NewKeyFile(path); !errors.Is(err, ErrKeyFileExists) {
		t.Fatalf("second call: %v", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("an existing key file was changed")
	}
	other, _ := NewKeyFile(filepath.Join(t.TempDir(), "b.key"))
	if other == addr {
		t.Fatal("two new keys must differ")
	}
}

func TestReadIdentityTakesOnlyWhatItNeeds(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadIdentity(dir); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("missing file: %v", err)
	}
	id := uuid.New()
	write := func(v any) {
		b, _ := json.Marshal(v)
		_ = os.WriteFile(filepath.Join(dir, "identity.json"), b, 0o600)
	}
	write(map[string]any{"owner_id": id.String(), "token": "tok", "has_keystore": true, "keystore_ciphertext_hex": "aa"})
	got, err := ReadIdentity(dir)
	if err != nil || got.OwnerID != id || got.Token != "tok" {
		t.Fatalf("identity = %+v, %v", got, err)
	}
	write(map[string]any{"owner_id": "nope", "token": "tok"})
	if _, err := ReadIdentity(dir); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("bad owner id: %v", err)
	}
	write(map[string]any{"owner_id": id.String(), "token": ""})
	if _, err := ReadIdentity(dir); !errors.Is(err, ErrNoIdentity) {
		t.Fatalf("no token: %v", err)
	}
}

// ── the client against the real chainapi handler ─────────────────────────

type memWallets struct {
	byOwner map[uuid.UUID]chain.Address
}

func (m *memWallets) BindOwner(_ context.Context, o uuid.UUID, w chain.Address) (bool, error) {
	if cur, ok := m.byOwner[o]; ok {
		if cur == w {
			return true, nil
		}
		return false, chainbind.ErrAlreadyBound
	}
	m.byOwner[o] = w
	return false, nil
}

func (m *memWallets) OwnerWallet(_ context.Context, o uuid.UUID) (chain.Address, bool, error) {
	w, ok := m.byOwner[o]
	return w, ok, nil
}

func testKey(t *testing.T) *chain.Key {
	t.Helper()
	path := filepath.Join(t.TempDir(), "k.key")
	if _, err := NewKeyFile(path); err != nil {
		t.Fatal(err)
	}
	k, err := chain.LoadKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestTheClientBindsAWalletThroughTheRealHandler(t *testing.T) {
	owner := uuid.New()
	wallets := &memWallets{byOwner: map[uuid.UUID]chain.Address{}}
	escrow := chain.MustParseAddress("0xBcE841C78c6c40C551fc223Ba9415D4751Af5958")
	token := chain.MustParseAddress("0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7")
	srv := httptest.NewServer(chainapi.New(chainapi.Deps{
		Wallets: wallets,
		Verify: func(tok string) (uuid.UUID, string, error) {
			if tok == "good" {
				return owner, "owner", nil
			}
			return uuid.Nil, "", errors.New("bad")
		},
		Info: chainapi.Info{ChainID: 1874, Escrow: escrow, Token: token},
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL + "/"}
	ctx := context.Background()

	info, err := c.FetchInfo(ctx)
	if err != nil || info.ChainID != 1874 || info.Escrow != escrow || info.Token != token || !info.Anchor.IsZero() {
		t.Fatalf("info = %+v, %v", info, err)
	}

	key := testKey(t)
	res, err := c.BindWallet(ctx, Identity{OwnerID: owner, Token: "good"}, info.ChainID, key)
	if err != nil || res.Wallet != key.Address() || res.Already {
		t.Fatalf("bind = %+v, %v", res, err)
	}
	if wallets.byOwner[owner] != key.Address() {
		t.Fatal("the coordinator did not store the wallet")
	}
	if res, err = c.BindWallet(ctx, Identity{OwnerID: owner, Token: "good"}, info.ChainID, key); err != nil || !res.Already {
		t.Fatalf("repeat bind = %+v, %v", res, err)
	}
	if w, found, err := c.BoundWallet(ctx, Identity{OwnerID: owner, Token: "good"}); err != nil || !found || w != key.Address() {
		t.Fatalf("BoundWallet = %v %v %v", w, found, err)
	}

	// A different wallet for the same owner is refused, with the server's reason.
	_, err = c.BindWallet(ctx, Identity{OwnerID: owner, Token: "good"}, info.ChainID, testKey(t))
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict || ae.Code != "already_bound" {
		t.Fatalf("second wallet: %v", err)
	}
	// A bad session token is refused.
	if _, err := c.BindWallet(ctx, Identity{OwnerID: owner, Token: "bad"}, info.ChainID, key); !errors.As(err, &ae) || ae.Status != http.StatusUnauthorized {
		t.Fatalf("bad token: %v", err)
	}
	// Signing for the wrong chain is refused.
	if _, err := c.BindWallet(ctx, Identity{OwnerID: owner, Token: "good"}, 1, testKey(t)); !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
		t.Fatalf("wrong chain: %v", err)
	}
}

func TestAnUnreachableCoordinatorIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	_, err := (&Client{Base: base}).FetchInfo(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cannot reach the coordinator") {
		t.Fatalf("err = %v", err)
	}
}

func TestExplainTurnsRevertsIntoSentencesAndPassesOtherErrorsThrough(t *testing.T) {
	word := func(n int64) string {
		s := big.NewInt(n).Text(16)
		return strings.Repeat("0", 64-len(s)) + s
	}
	cases := map[string]string{
		"WindowStillOpen(uint64,uint64) args=0x" + word(1) + word(1_700_000_000): "ends at 22:13:20 UTC",
		"CoordinatorStillActive(uint256) args=0x" + word(1_700_000_000):          "refunds open at 22:13:20 UTC",
		"NothingToClaim(uint64,address) args=0x" + word(1) + word(2):             "already paid",
		"NothingToRefund(address) args=0x" + word(2):                             "no unspent credit",
		"UnknownPeriod(uint64) args=0x" + word(9):                                "never posted",
		"PeriodIsVetoed(uint64) args=0x" + word(9):                               "cancelled by the guardian",
		"SomethingNew()": "the contract refused: SomethingNew()",
	}
	for reason, want := range cases {
		got := Explain(&chain.RevertError{Reason: reason})
		if !strings.Contains(got, want) {
			t.Errorf("%q: %q does not contain %q", reason, got, want)
		}
	}
	if got := Explain(errors.New("rpc down")); got != "rpc down" {
		t.Errorf("a plain error changed: %q", got)
	}
}

func TestFormatUnitsAndExplorerLinks(t *testing.T) {
	wei, _ := new(big.Int).SetString("290000000000000000", 10)
	if got := FormatUnits(wei, GasDecimals); got != "0.29" {
		t.Fatalf("0.29 WBT shown as %q", got)
	}
	var h [32]byte
	h[31] = 0xab
	if got := ExplorerTx("https://x.example/", h); got != "https://x.example/tx/0x00000000000000000000000000000000000000000000000000000000000000ab" {
		t.Fatalf("tx link = %q", got)
	}
	if ExplorerTx("", h) != "" || ExplorerAddress("", [20]byte{}) != "" {
		t.Fatal("no base means no link")
	}
}
