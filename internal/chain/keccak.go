package chain

import "golang.org/x/crypto/sha3"

const hashLen = 32

// Keccak256 returns the Ethereum Keccak-256 hash of the concatenated inputs.
// This is the original Keccak padding, not NIST SHA3-256.
func Keccak256(parts ...[]byte) [hashLen]byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		_, _ = h.Write(p)
	}
	var out [hashLen]byte
	copy(out[:], h.Sum(nil))
	return out
}
