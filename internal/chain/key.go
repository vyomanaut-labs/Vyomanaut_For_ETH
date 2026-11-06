package chain

import (
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

const (
	privKeyLen       = 32
	uncompressedLen  = 65
	groupReadWrite   = 0o077
	compactSigLen    = 65
	compactRecoverOf = 27 // recovery byte offset in a compact signature
)

// Key is a secp256k1 signing key and the address it controls.
type Key struct {
	priv *secp256k1.PrivateKey
	addr Address
}

// LoadKeyFile reads a hex private key (with or without 0x) from a file. On
// Unix the file must not be readable by group or others.
func LoadKeyFile(path string) (*Key, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&groupReadWrite != 0 {
		return nil, fmt.Errorf("%w: %s is accessible by other users; run chmod 600", ErrBadKey, path)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator configuration
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	return ParseKeyHex(strings.TrimSpace(string(raw)))
}

// ParseKeyHex parses a 32-byte hex private key.
func ParseKeyHex(s string) (*Key, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"))
	if err != nil || len(b) != privKeyLen {
		return nil, fmt.Errorf("%w: expected 32 hex-encoded bytes", ErrBadKey)
	}
	var scalar secp256k1.ModNScalar
	if overflow := scalar.SetByteSlice(b); overflow || scalar.IsZero() {
		return nil, fmt.Errorf("%w: not a valid secp256k1 scalar", ErrBadKey)
	}
	priv := secp256k1.NewPrivateKey(&scalar)
	return &Key{priv: priv, addr: addressOf(priv.PubKey())}, nil
}

func addressOf(pub *secp256k1.PublicKey) Address {
	ser := pub.SerializeUncompressed()
	h := Keccak256(ser[1:uncompressedLen])
	var a Address
	copy(a[:], h[hashLen-AddressLen:])
	return a
}

// Address is the account this key controls.
func (k *Key) Address() Address { return k.addr }

// String never reveals the key.
func (k *Key) String() string { return "chain.Key(" + k.addr.String() + ")" }

// GoString never reveals the key either, even under %#v.
func (k *Key) GoString() string { return k.String() }

// Signature is an Ethereum ECDSA signature.
type Signature struct {
	R, S [hashLen]byte
	// V is the recovery id, 0 or 1.
	V byte
}

// Sign signs a 32-byte digest. The signature is deterministic (RFC 6979) and
// uses the low-S form Ethereum requires.
func (k *Key) Sign(digest [hashLen]byte) Signature {
	compact := ecdsa.SignCompact(k.priv, digest[:], false)
	var sig Signature
	sig.V = compact[0] - compactRecoverOf
	copy(sig.R[:], compact[1:1+hashLen])
	copy(sig.S[:], compact[1+hashLen:compactSigLen])
	return sig
}

// RecoverAddress returns the address that produced sig over digest.
func RecoverAddress(digest [hashLen]byte, sig Signature) (Address, error) {
	compact := make([]byte, compactSigLen)
	compact[0] = sig.V + compactRecoverOf
	copy(compact[1:], sig.R[:])
	copy(compact[1+hashLen:], sig.S[:])
	pub, _, err := ecdsa.RecoverCompact(compact, digest[:])
	if err != nil {
		return Address{}, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	return addressOf(pub), nil
}

// PersonalMessageHash is the EIP-191 "personal_sign" digest of msg:
// keccak256("\x19Ethereum Signed Message:\n" + len(msg) + msg).
func PersonalMessageHash(msg []byte) [hashLen]byte {
	return Keccak256([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(msg))), msg)
}

// SignatureLen is the length of an encoded signature: r, s and v.
const SignatureLen = 2*hashLen + 1

// recoveryBase is the offset wallets add to the recovery id in personal_sign
// signatures (v is 27 or 28).
const recoveryBase = 27

// Bytes returns the 65-byte r || s || v form wallets produce, with v as 27 or 28.
func (s Signature) Bytes() []byte {
	out := make([]byte, 0, SignatureLen)
	out = append(out, s.R[:]...)
	out = append(out, s.S[:]...)
	return append(out, s.V+recoveryBase)
}

// ParseSignature reads the 65-byte r || s || v form. v may be 0, 1, 27 or 28,
// which covers the forms wallets and command-line tools emit.
func ParseSignature(b []byte) (Signature, error) {
	if len(b) != SignatureLen {
		return Signature{}, fmt.Errorf("%w: a signature is %d bytes, got %d", ErrBadKey, SignatureLen, len(b))
	}
	var sig Signature
	copy(sig.R[:], b[:hashLen])
	copy(sig.S[:], b[hashLen:2*hashLen])
	v := b[2*hashLen]
	if v >= recoveryBase {
		v -= recoveryBase
	}
	if v > 1 {
		return Signature{}, fmt.Errorf("%w: bad recovery id", ErrBadKey)
	}
	sig.V = v
	return sig, nil
}
