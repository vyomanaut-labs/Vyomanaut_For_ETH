package chain

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// anvilKey0 is the first well-known development key (Foundry/Hardhat). It
// controls no real funds anywhere.
const (
	anvilKey0  = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	anvilAddr0 = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	testRoot   = "0x5dc9da79a70659a9ad559cb701ded9a2ab9d823aad2f4960cfe370eff4604328"
)

func mustHexBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		t.Fatalf("bad hex in test: %v", err)
	}
	return b
}

func mustRoot(t *testing.T) [32]byte {
	t.Helper()
	var r [32]byte
	copy(r[:], mustHexBytes(t, testRoot))
	return r
}

func mustKey(t *testing.T) *Key {
	t.Helper()
	k, err := ParseKeyHex(anvilKey0)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// ── hashing, addresses, keys ─────────────────────────────────────────────

func TestKeccakAndSelectorMatchKnownValues(t *testing.T) {
	empty := Keccak256()
	if got := hex.EncodeToString(empty[:]); got != "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470" {
		t.Fatalf("keccak256(empty) = %s", got)
	}
	// concatenation: hashing parts equals hashing the joined bytes
	a, b := Keccak256([]byte("ab"), []byte("cd")), Keccak256([]byte("abcd"))
	if a != b {
		t.Fatal("Keccak256 must hash the concatenation of its parts")
	}
	sel := selector("transfer(address,uint256)")
	if hex.EncodeToString(sel[:]) != "a9059cbb" {
		t.Fatalf("selector = %x", sel)
	}
}

func TestAddressDerivedFromKnownKey(t *testing.T) {
	if got := mustKey(t).Address().String(); got != anvilAddr0 {
		t.Fatalf("address = %s, want %s", got, anvilAddr0)
	}
}

func TestEIP55ChecksumVectors(t *testing.T) {
	// vectors from EIP-55
	for _, want := range []string{
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
		"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
		"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
	} {
		a, err := ParseAddress(strings.ToLower(want))
		if err != nil {
			t.Fatal(err)
		}
		if a.String() != want {
			t.Errorf("checksum of %s = %s", strings.ToLower(want), a.String())
		}
	}
}

func TestParseAddressRejectsBadInput(t *testing.T) {
	for _, s := range []string{"", "0x", "0x1234", "zz" + strings.Repeat("0", 38), strings.Repeat("0", 42)} {
		if _, err := ParseAddress(s); !errors.Is(err, ErrBadAddress) {
			t.Errorf("%q: err = %v, want ErrBadAddress", s, err)
		}
	}
	if _, err := ParseAddress(strings.TrimPrefix(anvilAddr0, "0x")); err != nil {
		t.Errorf("an address without 0x should parse: %v", err)
	}
}

func TestKeyFilePermissionsAndFormats(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}

	k, err := LoadKeyFile(write("ok.key", anvilKey0+"\n", 0o600))
	if err != nil || k.Address().String() != anvilAddr0 {
		t.Fatalf("0600 key with trailing newline: %v", err)
	}
	if _, err := LoadKeyFile(write("noprefix.key", strings.TrimPrefix(anvilKey0, "0x"), 0o600)); err != nil {
		t.Errorf("key without 0x should load: %v", err)
	}
	if _, err := LoadKeyFile(write("loose.key", anvilKey0, 0o644)); !errors.Is(err, ErrBadKey) {
		t.Errorf("group-readable key: err = %v, want ErrBadKey", err)
	}
	for name, content := range map[string]string{
		"short.key": "0x1234", "nothex.key": strings.Repeat("zz", 32),
		"zero.key": strings.Repeat("00", 32), "overflow.key": strings.Repeat("ff", 32),
	} {
		if _, err := LoadKeyFile(write(name, content, 0o600)); !errors.Is(err, ErrBadKey) {
			t.Errorf("%s: err = %v, want ErrBadKey", name, err)
		}
	}
	if _, err := LoadKeyFile(filepath.Join(dir, "missing.key")); !errors.Is(err, ErrBadKey) {
		t.Errorf("missing file: err = %v", err)
	}
}

func TestKeyNeverPrintsItsSecret(t *testing.T) {
	k := mustKey(t)
	secret := strings.TrimPrefix(anvilKey0, "0x")
	for _, out := range []string{
		fmt.Sprintf("%v", k), fmt.Sprintf("%+v", k), fmt.Sprintf("%#v", k), k.String(),
		fmt.Sprintf("%v", *k), fmt.Sprintf("%+v", *k),
	} {
		if strings.Contains(strings.ToLower(out), secret) {
			t.Fatalf("formatted key leaked its secret: %s", out)
		}
	}
}

func TestSignaturesRecoverTheSignerAndAreLowS(t *testing.T) {
	k := mustKey(t)
	halfN, _ := new(big.Int).SetString("7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0", 16)
	for i := 0; i < 200; i++ {
		digest := Keccak256([]byte(fmt.Sprintf("message %d", i)))
		sig := k.Sign(digest)
		if sig.V > 1 {
			t.Fatalf("recovery id %d", sig.V)
		}
		if new(big.Int).SetBytes(sig.S[:]).Cmp(halfN) > 0 {
			t.Fatalf("signature %d is not low-S", i)
		}
		got, err := RecoverAddress(digest, sig)
		if err != nil || got != k.Address() {
			t.Fatalf("recovered %v (%v), want %v", got, err, k.Address())
		}
	}
}

func TestPersonalSignMatchesCast(t *testing.T) {
	// `cast wallet sign --private-key <anvil key 0> "hello vyoma"`
	const want = "620b5aec8fe508d9c524843fb2e681592b35eb8497e834b625eae11d35592c1466bdf18a1c700a7d80766e98d512bd26368bf6cd59c73683b955813c8d4d2d381b"
	sig := mustKey(t).Sign(PersonalMessageHash([]byte("hello vyoma")))
	got := hex.EncodeToString(sig.R[:]) + hex.EncodeToString(sig.S[:]) + fmt.Sprintf("%02x", sig.V+27)
	if got != want {
		t.Fatalf("personal_sign signature\n got %s\nwant %s", got, want)
	}
	rec, err := RecoverAddress(PersonalMessageHash([]byte("hello vyoma")), sig)
	if err != nil || rec != mustKey(t).Address() {
		t.Fatalf("recover: %v %v", rec, err)
	}
}

// ── RLP ──────────────────────────────────────────────────────────────────

func TestRLPMatchesTheSpecExamples(t *testing.T) {
	long := strings.Repeat("a", 56)
	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"dog", rlpString([]byte("dog")), "83646f67"},
		{"empty string", rlpString(nil), "80"},
		{"empty list", rlpList(), "c0"},
		{"cat dog list", rlpList(rlpString([]byte("cat")), rlpString([]byte("dog"))), "c88363617483646f67"},
		{"zero", rlpUint(big.NewInt(0)), "80"},
		{"fifteen", rlpUint(big.NewInt(15)), "0f"},
		{"1024", rlpUint(big.NewInt(1024)), "820400"},
		{"single byte 0x80", rlpString([]byte{0x80}), "8180"},
		{"56-byte string", rlpString([]byte(long)), "b838" + hex.EncodeToString([]byte(long))},
	}
	for _, c := range cases {
		if got := hex.EncodeToString(c.got); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

// ── transactions ─────────────────────────────────────────────────────────

func TestSignedTransactionMatchesCastMktx(t *testing.T) {
	// cast mktx --private-key <anvil key 0> --chain 1874 --nonce 5 --gas-limit 120000
	//   --gas-price 6000000000 --priority-gas-price 1000000000 --value 0
	//   0x5FbDB2315678afecb367f032d93F642f64180aa3 "anchor(uint64,bytes32,uint32)" 7 <root> 8
	const want = "02f8d382075205843b9aca00850165a0bc008301d4c0945fbdb2315678afecb367f032d93f642f64180aa380b864a149a21400000000000000000000000000000000000000000000000000000000000000075dc9da79a70659a9ad559cb701ded9a2ab9d823aad2f4960cfe370eff46043280000000000000000000000000000000000000000000000000000000000000008c001a0b416ffb7c0e16896164d64e34ef60e2008c0554fa8951ccc4930fee81290d568a02b662b2e83b860c89daa1a3fd956091e15c123a83d5d1176e75e30ab43d7a4e3"
	to := MustParseAddress("0x5FbDB2315678afecb367f032d93F642f64180aa3")
	data, err := encodeCall("anchor(uint64,bytes32,uint32)", uint64Arg(7), bytes32Arg(mustRoot(t)), uint64Arg(8))
	if err != nil {
		t.Fatal(err)
	}
	tx := &Tx{
		ChainID: 1874, Nonce: 5, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(6_000_000_000),
		Gas: 120_000, To: &to, Value: new(big.Int), Data: data,
	}
	raw, hash, err := tx.Sign(mustKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(raw); got != want {
		t.Fatalf("signed transaction differs from cast\n got %s\nwant %s", got, want)
	}
	if hash != Keccak256(raw) {
		t.Fatal("transaction hash must be keccak256 of the raw bytes")
	}
}

func TestTransactionRequiresFeeFields(t *testing.T) {
	if _, _, err := (&Tx{}).Sign(mustKey(t)); err == nil {
		t.Fatal("a transaction without fees must not sign")
	}
}

func TestContractCreationLeavesTheToFieldEmpty(t *testing.T) {
	tx := &Tx{ChainID: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2), Gas: 3, Value: new(big.Int), Data: []byte{0xaa}}
	fields, err := tx.payload()
	if err != nil {
		t.Fatal(err)
	}
	const toIndex = 5
	if hex.EncodeToString(fields[toIndex]) != "80" {
		t.Fatalf("to field = %x, want 80 (empty)", fields[toIndex])
	}
}

// ── ABI ──────────────────────────────────────────────────────────────────

func TestABIEncodingMatchesCastCalldata(t *testing.T) {
	a1 := MustParseAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	a2 := MustParseAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC")
	a3 := MustParseAddress("0x90F79bf6EB2c4f870365E785982E1f101E93b906")
	root := mustRoot(t)
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	cases := []struct {
		name string
		sig  string
		args []abiArg
		want string
	}{
		{"anchor", "anchor(uint64,bytes32,uint32)", []abiArg{uint64Arg(7), bytes32Arg(root), uint64Arg(8)},
			"a149a21400000000000000000000000000000000000000000000000000000000000000075dc9da79a70659a9ad559cb701ded9a2ab9d823aad2f4960cfe370eff46043280000000000000000000000000000000000000000000000000000000000000008"},
		{"postPeriod", "postPeriod(uint64,bytes32,address[],uint256[],address[],uint256[])",
			[]abiArg{uint64Arg(3), bytes32Arg(root), addrArrayArg([]Address{a1, a2}),
				uintArrayArg([]*big.Int{big.NewInt(100000000), big.NewInt(50000000)}),
				addrArrayArg([]Address{a3}), uintArrayArg([]*big.Int{big.NewInt(150000000)})},
			"989b72ee00000000000000000000000000000000000000000000000000000000000000035dc9da79a70659a9ad559cb701ded9a2ab9d823aad2f4960cfe370eff460432800000000000000000000000000000000000000000000000000000000000000c00000000000000000000000000000000000000000000000000000000000000120000000000000000000000000000000000000000000000000000000000000018000000000000000000000000000000000000000000000000000000000000001c0000000000000000000000000000000000000000000000000000000000000000200000000000000000000000070997970c51812dc3a010c7d01b50e0d17dc79c80000000000000000000000003c44cdddb6a900fa2b585dd299e03d12fa4293bc00000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000005f5e1000000000000000000000000000000000000000000000000000000000002faf080000000000000000000000000000000000000000000000000000000000000000100000000000000000000000090f79bf6eb2c4f870365e785982e1f101e93b90600000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000008f0d180"},
		{"postPeriod with empty lists", "postPeriod(uint64,bytes32,address[],uint256[],address[],uint256[])",
			[]abiArg{uint64Arg(1), bytes32Arg([32]byte{}), addrArrayArg(nil), uintArrayArg(nil), addrArrayArg(nil), uintArrayArg(nil)},
			"989b72ee0000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000c000000000000000000000000000000000000000000000000000000000000000e0000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000001200000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"},
		{"claimMany", "claimMany(uint64,address[])", []abiArg{uint64Arg(3), addrArrayArg([]Address{a1, a3})},
			"9a8877e900000000000000000000000000000000000000000000000000000000000000030000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000200000000000000000000000070997970c51812dc3a010c7d01b50e0d17dc79c800000000000000000000000090f79bf6eb2c4f870365e785982e1f101e93b906"},
		{"deposit", "deposit(uint256)", []abiArg{uintArg(big.NewInt(110011323))},
			"b6b55f2500000000000000000000000000000000000000000000000000000000068ea3bb"},
		{"claim", "claim(uint64,address)", []abiArg{uint64Arg(9), addrArg(a2)},
			"881a1ce000000000000000000000000000000000000000000000000000000000000000090000000000000000000000003c44cdddb6a900fa2b585dd299e03d12fa4293bc"},
		{"approve max", "approve(address,uint256)", []abiArg{addrArg(a1), uintArg(maxUint)},
			"095ea7b300000000000000000000000070997970c51812dc3a010c7d01b50e0d17dc79c8ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		{"balanceOf", "balanceOf(address)", []abiArg{addrArg(a1)},
			"70a0823100000000000000000000000070997970c51812dc3a010c7d01b50e0d17dc79c8"},
	}
	for _, c := range cases {
		got, err := encodeCall(c.sig, c.args...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if hex.EncodeToString(got) != c.want {
			t.Errorf("%s calldata differs from cast\n got %x\nwant %s", c.name, got, c.want)
		}
	}
}

func TestABIRejectsOutOfRangeIntegers(t *testing.T) {
	tooBig := new(big.Int).Lsh(big.NewInt(1), 256)
	for name, arg := range map[string]abiArg{
		"negative": uintArg(big.NewInt(-1)), "2^256": uintArg(tooBig), "nil": uintArg(nil),
		"array element": uintArrayArg([]*big.Int{big.NewInt(1), big.NewInt(-5)}),
	} {
		if _, err := encodeCall("f(uint256)", arg); !errors.Is(err, ErrABI) {
			t.Errorf("%s: err = %v, want ErrABI", name, err)
		}
	}
}

func TestABIReaderBoundsAndPadding(t *testing.T) {
	r := abiReader{data: make([]byte, wordLen)}
	if _, err := r.word(1); !errors.Is(err, ErrABI) {
		t.Errorf("reading past the end: %v", err)
	}
	if _, err := (abiReader{}).uintBits(0, 64); !errors.Is(err, ErrABI) {
		t.Errorf("empty return data: %v", err)
	}
	big64 := make([]byte, wordLen)
	big64[wordLen-9] = 1 // 2^64
	if _, err := (abiReader{big64}).uintBits(0, 64); !errors.Is(err, ErrABI) {
		t.Errorf("a value that overflows uint64 must be rejected: %v", err)
	}
	dirty := make([]byte, wordLen)
	dirty[0] = 1
	if _, err := (abiReader{dirty}).address(0); !errors.Is(err, ErrABI) {
		t.Errorf("an address word with padding bits set must be rejected: %v", err)
	}
	two := make([]byte, wordLen)
	two[wordLen-1] = 2
	if _, err := (abiReader{two}).boolean(0); !errors.Is(err, ErrABI) {
		t.Errorf("a bool of 2 must be rejected: %v", err)
	}
}

// ── reverts ──────────────────────────────────────────────────────────────

func TestDescribeRevert(t *testing.T) {
	errString := append(append([]byte{0x08, 0xc3, 0x79, 0xa0}, make([]byte, 31)...), 0x20) // offset 32
	errString = append(errString, make([]byte, 31)...)
	errString = append(errString, 4) // length 4
	errString = append(errString, []byte("boom")...)
	errString = append(errString, make([]byte, 28)...)

	credit, err := encodeCall("InsufficientCredit(address,uint256,uint256)",
		addrArg(MustParseAddress(anvilAddr0)), uintArg(big.NewInt(1)), uintArg(big.NewInt(2)))
	if err != nil {
		t.Fatal(err)
	}
	notCoord, _ := encodeCall("NotCoordinator()")
	panicData := append([]byte{0x4e, 0x48, 0x7b, 0x71}, make([]byte, 31)...)
	panicData = append(panicData, 0x11)

	cases := map[string]struct {
		data []byte
		want string
	}{
		"Error(string)":    {errString, "boom"},
		"custom with args": {credit, "InsufficientCredit(address,uint256,uint256) args=0x"},
		"custom no args":   {notCoord, "NotCoordinator()"},
		"panic":            {panicData, "panic code 0x11"},
		"unknown":          {[]byte{1, 2, 3, 4, 5}, "0x0102030405"},
		"empty":            {nil, "no reason given"},
	}
	for name, c := range cases {
		if got := DescribeRevert(c.data); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: got %q, want prefix %q", name, got, c.want)
		}
	}
}

func TestEveryCustomErrorHasADistinctSelector(t *testing.T) {
	seen := map[[4]byte]string{}
	for _, sig := range customErrorSignatures {
		s := selector(sig)
		if prev, dup := seen[s]; dup {
			t.Fatalf("%s and %s share selector %x", sig, prev, s)
		}
		seen[s] = sig
	}
}
