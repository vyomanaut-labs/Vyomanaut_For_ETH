package receiptleaf

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func leafOf(data []byte) [32]byte { return sha256.Sum256(append([]byte{LeafPrefix}, data...)) }

// rfcData is the eight-leaf input used by the Certificate Transparency
// reference tests for RFC 6962 trees.
func rfcData() [][]byte {
	return [][]byte{
		{},
		{0x00},
		{0x10},
		{0x20, 0x21},
		{0x30, 0x31},
		{0x40, 0x41, 0x42, 0x43},
		seq(0x50, 8),
		seq(0x60, 16),
	}
}

func TestMerkleRootMatchesRFC6962ReferenceVectors(t *testing.T) {
	want := []string{
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", // 0 leaves
		"6e340b9cffb37a989ca544e6bb780a2c78901d3fb33738768511a30617afa01d", // 1
		"fac54203e7cc696cf0dfcb42c92a1d9dbaf70ad9e621f4bd8d98662f00e3c125", // 2
		"aeb6bcfe274b70a14fb067a5e5578264db0fa9b51af5e0ba159158f329e06e77", // 3
		"d37ee418976dd95753c1c73862b9398fa2a2cf9b4ff0fdfe8b30cd95209614b7", // 4
		"4e3bbb1f7b478dcfe71fb631631519a3bca12c9aefca1612bfce4c13a86264d4", // 5
		"76e67dadbcdf1e10e1b74ddc608abd2f98dfb16fbce75277b5232a127f2087ef", // 6
		"ddb89be403809e325750d3d263cd78929c2942b7942a34b77e122c9594a74c8c", // 7
		"5dc9da79a70659a9ad559cb701ded9a2ab9d823aad2f4960cfe370eff4604328", // 8
	}
	var leaves [][32]byte
	for _, d := range rfcData() {
		leaves = append(leaves, leafOf(d))
	}
	for n := 0; n <= len(leaves); n++ {
		root := MerkleRoot(leaves[:n])
		if got := hex.EncodeToString(root[:]); got != want[n] {
			t.Errorf("root of %d leaves = %s, want %s", n, got, want[n])
		}
	}
}

func syntheticLeaves(n int) [][32]byte {
	out := make([][32]byte, n)
	for i := range out {
		out[i] = leafOf([]byte(fmt.Sprintf("leaf-%d", i)))
	}
	return out
}

func TestEveryInclusionProofVerifies(t *testing.T) {
	const maxSize = 70
	for n := 1; n <= maxSize; n++ {
		leaves := syntheticLeaves(n)
		root := MerkleRoot(leaves)
		for i := 0; i < n; i++ {
			proof, err := InclusionProof(leaves, i)
			if err != nil {
				t.Fatalf("n=%d i=%d: %v", n, i, err)
			}
			if !VerifyInclusion(leaves[i], i, n, proof, root) {
				t.Fatalf("valid proof rejected: n=%d i=%d", n, i)
			}
		}
	}
}

func TestInclusionProofLengthIsLogarithmic(t *testing.T) {
	leaves := syntheticLeaves(1000)
	proof, err := InclusionProof(leaves, 417)
	if err != nil {
		t.Fatal(err)
	}
	const maxHashes = 10 // ceil(log2(1000))
	if len(proof) > maxHashes {
		t.Fatalf("proof has %d hashes for 1000 leaves, want at most %d", len(proof), maxHashes)
	}
}

func TestVerifyInclusionRejectsTampering(t *testing.T) {
	const n = 13
	leaves := syntheticLeaves(n)
	root := MerkleRoot(leaves)
	const idx = 6
	proof, err := InclusionProof(leaves, idx)
	if err != nil {
		t.Fatal(err)
	}

	if VerifyInclusion(leaves[idx+1], idx, n, proof, root) {
		t.Error("accepted the wrong leaf")
	}
	if VerifyInclusion(leaves[idx], idx+1, n, proof, root) {
		t.Error("accepted the wrong index")
	}
	// A size that contradicts the proof's length is rejected. (A size that
	// differs only in a subtree the proof never touches cannot be detected by
	// any RFC 6962 verifier, which is why size and root must come from the
	// same trusted source; see VerifyInclusion.)
	for _, wrongSize := range []int{idx, 8, 20} {
		if VerifyInclusion(leaves[idx], idx, wrongSize, proof, root) {
			t.Errorf("accepted tree size %d for a proof built for %d", wrongSize, n)
		}
	}
	if VerifyInclusion(leaves[idx], idx, n, proof[:len(proof)-1], root) {
		t.Error("accepted a truncated proof")
	}
	if VerifyInclusion(leaves[idx], idx, n, append(append([][32]byte(nil), proof...), root), root) {
		t.Error("accepted an over-long proof")
	}
	for i := range proof {
		bad := append([][32]byte(nil), proof...)
		bad[i][0] ^= 1
		if VerifyInclusion(leaves[idx], idx, n, bad, root) {
			t.Errorf("accepted a proof with hash %d altered", i)
		}
	}
	otherRoot := root
	otherRoot[0] ^= 1
	if VerifyInclusion(leaves[idx], idx, n, proof, otherRoot) {
		t.Error("accepted the wrong root")
	}
}

func TestVerifyInclusionNeverPanicsOnBadSizes(t *testing.T) {
	var h [32]byte
	for _, c := range []struct{ index, size int }{{-1, 5}, {0, 0}, {5, 5}, {0, -3}, {1 << 30, 1 << 31}} {
		if VerifyInclusion(h, c.index, c.size, nil, h) {
			t.Errorf("accepted index=%d size=%d", c.index, c.size)
		}
	}
	// a single-leaf tree has an empty proof and the root equals the leaf
	if !VerifyInclusion(h, 0, 1, nil, h) {
		t.Error("rejected the one-leaf tree")
	}
}

func TestInclusionProofRejectsBadIndex(t *testing.T) {
	leaves := syntheticLeaves(4)
	for _, i := range []int{-1, 4, 100} {
		if _, err := InclusionProof(leaves, i); err != ErrIndexOutOfRange {
			t.Errorf("index %d: err = %v, want ErrIndexOutOfRange", i, err)
		}
	}
	if _, err := InclusionProof(nil, 0); err != ErrIndexOutOfRange {
		t.Errorf("empty tree: err = %v, want ErrIndexOutOfRange", err)
	}
}

func TestReceiptTreeEndToEnd(t *testing.T) {
	var rs []Receipt
	for i := 0; i < 11; i++ {
		r := fullReceipt()
		r.ReceiptID[15] = byte(i)
		r.ServiceCountersignTS = r.ServiceCountersignTS.Add(timeStep(i))
		rs = append(rs, r)
	}
	SortCanonical(rs)
	leaves, err := LeafHashes(rs)
	if err != nil {
		t.Fatal(err)
	}
	root := MerkleRoot(leaves)
	proof, err := InclusionProof(leaves, 7)
	if err != nil {
		t.Fatal(err)
	}
	// a verifier that only holds receipt 7, the proof and the root
	leaf7, err := rs[7].LeafHash()
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyInclusion(leaf7, 7, len(rs), proof, root) {
		t.Fatal("receipt 7 not proven against the root")
	}
	// a rewritten receipt no longer verifies
	forged := rs[7]
	forged.Result = ResultFail
	forgedLeaf, err := forged.LeafHash()
	if err != nil {
		t.Fatal(err)
	}
	if VerifyInclusion(forgedLeaf, 7, len(rs), proof, root) {
		t.Fatal("a rewritten receipt verified against the original root")
	}
}
