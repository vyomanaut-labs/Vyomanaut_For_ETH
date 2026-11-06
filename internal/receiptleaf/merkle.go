package receiptleaf

import "crypto/sha256"

// hashNode returns SHA-256(0x01 || left || right).
func hashNode(left, right [sizeHash]byte) [sizeHash]byte {
	b := append([]byte{NodePrefix}, left[:]...)
	b = append(b, right[:]...)
	return sha256.Sum256(b)
}

// largestPowerOfTwoBelow returns the largest power of two strictly less than
// n. n must be at least 2.
func largestPowerOfTwoBelow(n int) int {
	k := 1
	for k<<1 < n {
		k <<= 1
	}
	return k
}

// MerkleRoot returns the RFC 6962 tree hash of leaf hashes (not of raw
// receipts: call LeafHashes first). An empty slice hashes the empty string.
func MerkleRoot(leaves [][sizeHash]byte) [sizeHash]byte {
	n := len(leaves)
	if n == 0 {
		return sha256.Sum256(nil)
	}
	if n == 1 {
		return leaves[0]
	}
	k := largestPowerOfTwoBelow(n)
	return hashNode(MerkleRoot(leaves[:k]), MerkleRoot(leaves[k:]))
}

// InclusionProof returns the RFC 6962 audit path for the leaf at index: the
// sibling hashes from the leaf level upward.
func InclusionProof(leaves [][sizeHash]byte, index int) ([][sizeHash]byte, error) {
	if index < 0 || index >= len(leaves) {
		return nil, ErrIndexOutOfRange
	}
	return auditPath(leaves, index), nil
}

func auditPath(leaves [][sizeHash]byte, m int) [][sizeHash]byte {
	n := len(leaves)
	if n <= 1 {
		return nil
	}
	k := largestPowerOfTwoBelow(n)
	if m < k {
		return append(auditPath(leaves[:k], m), MerkleRoot(leaves[k:]))
	}
	return append(auditPath(leaves[k:], m-k), MerkleRoot(leaves[:k]))
}

// VerifyInclusion reports whether proof shows that leafHash is the leaf at
// index in a tree of size leaves whose root is root (RFC 9162 section
// 2.1.3.2). It never panics on malformed input.
//
// size and root must come from one trusted source (for a chain anchor, the
// leaf count and root stored together). A verifier cannot detect a wrong size
// that differs only in a subtree the proof never visits.
func VerifyInclusion(leafHash [sizeHash]byte, index, size int, proof [][sizeHash]byte, root [sizeHash]byte) bool {
	if index < 0 || size <= 0 || index >= size {
		return false
	}
	fn := index
	sn := size - 1
	r := leafHash
	for _, p := range proof {
		if sn == 0 {
			return false
		}
		if fn&1 == 1 || fn == sn {
			r = hashNode(p, r)
			if fn&1 == 0 {
				for fn&1 == 0 && fn != 0 {
					fn >>= 1
					sn >>= 1
				}
			}
		} else {
			r = hashNode(r, p)
		}
		fn >>= 1
		sn >>= 1
	}
	return sn == 0 && r == root
}
