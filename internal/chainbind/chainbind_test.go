package chainbind

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

func testKey(t *testing.T) *chain.Key {
	t.Helper()
	k, err := chain.ParseKeyHex(hex32(randBytes(32)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func hex32(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0x0f])
	}
	return string(out)
}

func sign(k *chain.Key, chainID uint64, owner uuid.UUID) chain.Signature {
	return k.Sign(chain.PersonalMessageHash([]byte(BindingMessage(chainID, owner, k.Address()))))
}

func TestTheMessageNamesTheOwnerWalletAndChain(t *testing.T) {
	owner := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	w := chain.MustParseAddress("0x7CBEe871deaDF21C3f8506283a2b96544b27FEb1")
	got := BindingMessage(1874, owner, w)
	want := "Vyomanaut wallet binding v1\nowner: 11111111-2222-3333-4444-555555555555\nwallet: 0x7cbee871deadf21c3f8506283a2b96544b27feb1\nchain: 1874"
	if got != want {
		t.Fatalf("message =\n%q\nwant\n%q", got, want)
	}
}

func TestAWalletSignatureBindsOnlyForTheOwnerWalletAndChainItNames(t *testing.T) {
	k := testKey(t)
	owner := uuid.New()
	sig := sign(k, 1874, owner)

	if err := VerifyBinding(1874, owner, k.Address(), sig); err != nil {
		t.Fatalf("a correct signature was refused: %v", err)
	}
	other := testKey(t)
	for name, err := range map[string]error{
		"another owner":  VerifyBinding(1874, uuid.New(), k.Address(), sig),
		"another chain":  VerifyBinding(1, owner, k.Address(), sig),
		"another wallet": VerifyBinding(1874, owner, other.Address(), sig),
		"zero wallet":    VerifyBinding(1874, owner, chain.Address{}, sig),
	} {
		if err == nil {
			t.Errorf("%s: the signature must not be accepted", name)
		}
	}
}

func TestProviderWalletsAreDerivedDeterministicallyAndDifferPerProvider(t *testing.T) {
	secret := randBytes(32)
	a, b := uuid.New(), uuid.New()
	a1, err := DeriveProviderWallet(secret, a)
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := DeriveProviderWallet(secret, a)
	b1, _ := DeriveProviderWallet(secret, b)
	other, _ := DeriveProviderWallet(randBytes(32), a)
	if a1 != a2 {
		t.Fatal("the same secret and provider must give the same wallet")
	}
	if a1 == b1 || a1 == other || a1.IsZero() {
		t.Fatal("different providers, or a different secret, must give different wallets")
	}
	if _, err := DeriveProviderWallet([]byte("short"), a); !errors.Is(err, ErrSeedTooShort) {
		t.Fatalf("a short secret: %v", err)
	}
}

func TestOwnerBindingRules(t *testing.T) {
	d := newTestDB(t)
	b := NewBinder(d.app)
	ctx := context.Background()
	owner, other := d.newOwner(t), d.newOwner(t)
	w1, w2 := testKey(t).Address(), testKey(t).Address()

	if _, found, err := b.OwnerWallet(ctx, owner); err != nil || found {
		t.Fatalf("before binding: found=%v err=%v", found, err)
	}
	if already, err := b.BindOwner(ctx, owner, w1); err != nil || already {
		t.Fatalf("first bind: already=%v err=%v", already, err)
	}
	if got, found, _ := b.OwnerWallet(ctx, owner); !found || got != w1 {
		t.Fatalf("stored wallet = %v", got)
	}
	if already, err := b.BindOwner(ctx, owner, w1); err != nil || !already {
		t.Fatalf("binding the same pair again: already=%v err=%v", already, err)
	}
	if _, err := b.BindOwner(ctx, owner, w2); !errors.Is(err, ErrAlreadyBound) {
		t.Fatalf("a second wallet for the owner: %v", err)
	}
	if _, err := b.BindOwner(ctx, other, w1); !errors.Is(err, ErrWalletTaken) {
		t.Fatalf("a wallet another owner holds: %v", err)
	}
	if _, err := b.BindOwner(ctx, uuid.New(), w2); !errors.Is(err, ErrUnknownOwner) {
		t.Fatalf("an unregistered owner: %v", err)
	}
	if _, err := b.BindOwner(ctx, other, chain.Address{}); !errors.Is(err, ErrZeroWallet) {
		t.Fatalf("the zero address: %v", err)
	}
}

func TestProviderWalletsAreAssignedOnceAndSkipDepartedProviders(t *testing.T) {
	d := newTestDB(t)
	b := NewBinder(d.app)
	ctx := context.Background()
	secret := randBytes(32)
	derive := func(p uuid.UUID) (chain.Address, error) { return DeriveProviderWallet(secret, p) }

	active := d.newProvider(t, "ACTIVE")
	vetting := d.newProvider(t, "VETTING")
	departed := d.newProvider(t, "DEPARTED")

	n, err := b.AssignProviderWallets(ctx, derive)
	if err != nil || n != 2 {
		t.Fatalf("assigned %d, %v; want the 2 providers that have not left", n, err)
	}
	var have int
	_ = d.admin.QueryRow(`SELECT COUNT(*) FROM provider_payout_addresses WHERE provider_id = $1`, departed).Scan(&have)
	if have != 0 {
		t.Fatal("a departed provider must not get a wallet")
	}
	want, _ := DeriveProviderWallet(secret, active)
	var stored []byte
	if err := d.admin.QueryRow(`SELECT wallet FROM provider_payout_addresses WHERE provider_id = $1`, active).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(want[:]) {
		t.Fatal("the stored wallet is not the derived one")
	}

	// Running again changes nothing, and never rewrites an address already given.
	if n, err := b.AssignProviderWallets(ctx, func(uuid.UUID) (chain.Address, error) {
		return testKey(t).Address(), nil
	}); err != nil || n != 0 {
		t.Fatalf("second run assigned %d, %v; want 0", n, err)
	}
	_ = vetting

	// A provider that joins later is picked up.
	late := d.newProvider(t, "PENDING_ONBOARDING")
	if n, err := b.AssignProviderWallets(ctx, derive); err != nil || n != 1 {
		t.Fatalf("late provider: assigned %d, %v", n, err)
	}
	_ = late
}
