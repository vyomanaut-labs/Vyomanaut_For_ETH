package arkiv

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

func rnd(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
}

// fullReceipt returns a receipt with every optional field set (the largest
// JSON form), countersigned at ts.
func fullReceipt(provider [16]byte, ts time.Time, result receiptleaf.Result) receiptleaf.Receipt {
	var r receiptleaf.Receipt
	rnd(r.ReceiptID[:])
	rnd(r.ChunkID[:])
	rnd(r.ChallengeNonce[:])
	rnd(r.ServiceSig[:])
	r.ProviderID = provider
	r.ServerChallengeTS = ts.Add(-time.Second).UTC()
	r.ServiceCountersignTS = ts.UTC()
	r.Result = result
	var h [32]byte
	rnd(h[:])
	r.ResponseHash = &h
	lat := uint32(123)
	r.ResponseLatencyMS = &lat
	var s [64]byte
	rnd(s[:])
	r.ProviderSig = &s
	return r
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func bodyOf(t *testing.T, attrs []Attribute) []byte {
	t.Helper()
	for _, a := range attrs {
		if a.NameString() == AttrBody {
			return a.Value
		}
	}
	t.Fatal("no body attribute")
	return nil
}

var testPeriod = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

func TestBuildAuditBatchesGroupsAndSplits(t *testing.T) {
	var a, b [16]byte
	a[0], b[0] = 2, 1 // b sorts first
	var rs []receiptleaf.Receipt
	for i := 0; i < 2*MaxReceiptsPerBatch+50; i++ {
		rs = append(rs, fullReceipt(a, testPeriod.Add(time.Duration(i)*time.Millisecond), receiptleaf.ResultPass))
	}
	rs = append(rs, fullReceipt(b, testPeriod.Add(time.Second), receiptleaf.ResultFail),
		fullReceipt(b, testPeriod.Add(2*time.Second), receiptleaf.ResultTimeout))
	receiptleaf.SortCanonical(rs)

	batches, err := BuildAuditBatches(rs, testPeriod, testPeriod.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 4 {
		t.Fatalf("got %d batches, want 4", len(batches))
	}
	if batches[0].ProviderID != b || batches[0].Fail != 1 || batches[0].Timeout != 1 || len(batches[0].Receipts) != 2 {
		t.Fatalf("first batch should be provider b with 1 fail and 1 timeout: %+v", batches[0])
	}
	wantSizes := []int{MaxReceiptsPerBatch, MaxReceiptsPerBatch, 50}
	for i, want := range wantSizes {
		bt := batches[i+1]
		if bt.ProviderID != a || int(bt.Seq) != i || len(bt.Receipts) != want || int(bt.Pass) != want {
			t.Fatalf("batch %d: provider/seq/size/pass wrong: seq=%d size=%d pass=%d", i+1, bt.Seq, len(bt.Receipts), bt.Pass)
		}
		leaves, err := receiptleaf.LeafHashes(bt.Receipts)
		if err != nil {
			t.Fatal(err)
		}
		if receiptleaf.MerkleRoot(leaves) != bt.Root {
			t.Fatalf("batch %d root is not receiptleaf.MerkleRoot of its leaves", i+1)
		}
	}
}

func TestBuildAuditBatchesRejectsReceiptOutsidePeriod(t *testing.T) {
	var p [16]byte
	r := fullReceipt(p, testPeriod.Add(2*time.Minute), receiptleaf.ResultPass) // == end: outside
	if _, err := BuildAuditBatches([]receiptleaf.Receipt{r}, testPeriod, testPeriod.Add(2*time.Minute)); err == nil {
		t.Fatal("a receipt at the period end must be rejected")
	}
}

func TestFullBatchFitsVerifiedPayload(t *testing.T) {
	var p [16]byte
	rnd(p[:])
	var rs []receiptleaf.Receipt
	for i := 0; i < MaxReceiptsPerBatch; i++ {
		rs = append(rs, fullReceipt(p, testPeriod.Add(time.Duration(i)*time.Second), receiptleaf.ResultPass))
	}
	batches, err := BuildAuditBatches(rs, testPeriod, testPeriod.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	attrs, size, err := AuditBatchAttributes(batches[0], newKey(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if size > VerifiedMaxBytes {
		t.Fatalf("a full batch is %d bytes, above %d", size, VerifiedMaxBytes)
	}
	if len(attrs) > MaxAttributes {
		t.Fatalf("%d attributes", len(attrs))
	}
	t.Logf("full batch body: %d bytes", size)
}

func TestAuditBatchRoundTripAndTamper(t *testing.T) {
	key := newKey(t)
	var p [16]byte
	rnd(p[:])
	rs := []receiptleaf.Receipt{
		fullReceipt(p, testPeriod.Add(time.Second), receiptleaf.ResultPass),
		fullReceipt(p, testPeriod.Add(3*time.Second), receiptleaf.ResultFail),
	}
	batches, err := BuildAuditBatches(rs, testPeriod, testPeriod.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	attrs, _, err := AuditBatchAttributes(batches[0], key, "test-app")
	if err != nil {
		t.Fatal(err)
	}
	body := bodyOf(t, attrs)

	got, pub, err := DecodeAuditBatch(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !pub.Equal(key.Public()) || got.Root != batches[0].Root || got.Pass != 1 || got.Fail != 1 {
		t.Fatal("decoded batch differs from the published one")
	}

	// Flip one result: the root no longer matches.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	setResult(t, raw, 1, "PASS")
	tampered, _ := json.Marshal(raw)
	if _, _, err := DecodeAuditBatch(tampered); !errors.Is(err, ErrBadBatch) {
		t.Fatalf("tampered receipt: got %v, want ErrBadBatch", err)
	}

	// Re-sign with another key but keep the old signature: rejected.
	setResult(t, raw, 1, "FAIL")
	other, ok := newKey(t).Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("not an Ed25519 key")
	}
	raw["coord_key"] = hexs(other)
	swapped, _ := json.Marshal(raw)
	if _, _, err := DecodeAuditBatch(swapped); !errors.Is(err, ErrBadBatch) {
		t.Fatalf("swapped key: got %v, want ErrBadBatch", err)
	}
}

// setResult changes the audit_result of receipt i in a decoded batch body.
func setResult(t *testing.T, raw map[string]any, i int, result string) {
	t.Helper()
	list, ok := raw["receipts"].([]any)
	if !ok || i >= len(list) {
		t.Fatal("body has no receipts list")
	}
	rec, ok := list[i].(map[string]any)
	if !ok {
		t.Fatal("receipt is not an object")
	}
	rec["audit_result"] = result
}

func hexs(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, v := range b {
		out = append(out, digits[v>>4], digits[v&0x0f])
	}
	return string(out)
}

func TestBatchReferenceRoundTrip(t *testing.T) {
	var p [16]byte
	rnd(p[:])
	ref := BatchReference(p, testPeriod, 7)
	gp, gs, seq, err := ParseBatchReference(ref)
	if err != nil || gp != p || !gs.Equal(testPeriod) || seq != 7 {
		t.Fatalf("round trip of %q failed: %v", ref, err)
	}
	for _, bad := range []string{"", "a:b", "x:1:2", receiptleaf.FormatUUID(p) + ":x:1"} {
		if _, _, _, err := ParseBatchReference(bad); err == nil {
			t.Fatalf("%q should not parse", bad)
		}
	}
}

func TestSigningBytesHaveFixedSize(t *testing.T) {
	if n := len(AuditBatchSigningBytes(AuditBatch{PeriodStart: testPeriod, PeriodEnd: testPeriod})); n != 128 {
		t.Fatalf("audit batch signing bytes: %d", n)
	}
	if n := len(FileManifestSigningBytes(FileManifest{RegisteredAt: testPeriod})); n != 160 {
		t.Fatalf("manifest signing bytes: %d", n)
	}
	if n := len(TombstoneSigningBytes([16]byte{}, testPeriod)); n != 64 {
		t.Fatalf("tombstone signing bytes: %d", n)
	}
}

func TestPeriodStartAlignsToEpoch(t *testing.T) {
	got := PeriodStart(time.Date(2026, 11, 6, 10, 3, 59, 0, time.UTC), 2*time.Minute)
	if !got.Equal(time.Date(2026, 11, 6, 10, 2, 0, 0, time.UTC)) {
		t.Fatalf("got %s", got)
	}
}

func TestManifestRoundTripAndTamper(t *testing.T) {
	key := newKey(t)
	var shards []Shard
	for seg := uint32(0); seg < 2; seg++ {
		for sh := uint16(0); sh < 5; sh++ {
			s := Shard{SegmentIndex: seg, ShardIndex: sh}
			rnd(s.ChunkID[:])
			shards = append(shards, s)
		}
	}
	m := FileManifest{
		PointerSHA256: PointerHash([]byte("nonce"), []byte("ct"), []byte("tag")),
		ShardRoot:     ShardRoot(shards), Segments: 2, Shards: 10, RSK: 3, RSN: 5,
		RegisteredAt: testPeriod,
	}
	rnd(m.FileID[:])

	// Order of the input does not change the root.
	rev := append([]Shard(nil), shards...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	if ShardRoot(rev) != m.ShardRoot {
		t.Fatal("ShardRoot depends on input order")
	}

	attrs, _, err := FileManifestAttributes(m, key, "")
	if err != nil {
		t.Fatal(err)
	}
	got, pub, err := DecodeFileManifest(bodyOf(t, attrs))
	if err != nil || got != m || !pub.Equal(key.Public()) {
		t.Fatalf("manifest round trip: %v", err)
	}

	var raw map[string]any
	_ = json.Unmarshal(bodyOf(t, attrs), &raw)
	raw["shards"] = 9
	bad, _ := json.Marshal(raw)
	if _, _, err := DecodeFileManifest(bad); !errors.Is(err, ErrBadManifest) {
		t.Fatalf("tampered manifest: got %v", err)
	}

	tAttrs, _, err := TombstoneAttributes(m.FileID, testPeriod, key, "")
	if err != nil {
		t.Fatal(err)
	}
	id, seen, tpub, err := DecodeTombstone(bodyOf(t, tAttrs))
	if err != nil || id != m.FileID || !seen.Equal(testPeriod) || !tpub.Equal(key.Public()) {
		t.Fatalf("tombstone round trip: %v", err)
	}
}
