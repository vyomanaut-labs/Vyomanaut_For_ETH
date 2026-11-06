package receiptleaf

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// seq returns n bytes counting up from start.
func seq(start, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(start + i)
	}
	return b
}

// mul returns n bytes where byte i is (i*mul + add) mod 256.
func mul(mulBy, add, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*mulBy + add)
	}
	return b
}

func fullReceipt() Receipt {
	var r Receipt
	copy(r.ReceiptID[:], []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff})
	copy(r.ChunkID[:], seq(0, 32))
	copy(r.ProviderID[:], seq(0xA0, 16))
	r.ChallengeNonce[0] = 1
	copy(r.ChallengeNonce[1:], seq(0x40, 32))
	r.ServerChallengeTS = time.UnixMicro(1730000000123456)
	r.ServiceCountersignTS = time.UnixMicro(1730000000654321)
	r.Result = ResultPass
	h := [32]byte{}
	copy(h[:], seq(0x80, 32))
	r.ResponseHash = &h
	lat := uint32(7)
	r.ResponseLatencyMS = &lat
	r.AddressWasStale = true
	ps := [64]byte{}
	copy(ps[:], mul(3, 0, 64))
	r.ProviderSig = &ps
	copy(r.ServiceSig[:], mul(5, 1, 64))
	return r
}

func timeoutReceipt() Receipt {
	var r Receipt
	copy(r.ReceiptID[:], []byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00})
	copy(r.ChunkID[:], seq(0xE0, 32))
	copy(r.ProviderID[:], seq(0xA0, 16))
	r.ChallengeNonce[0] = 2
	copy(r.ChallengeNonce[1:], seq(0x10, 32))
	r.ServerChallengeTS = time.UnixMicro(1730000001000000)
	r.ServiceCountersignTS = time.UnixMicro(1730000062000000)
	r.Result = ResultTimeout
	copy(r.ServiceSig[:], mul(7, 2, 64))
	return r
}

// Golden values come from an independent implementation of the layout in
// doc.go, not from this package.
const (
	goldenFullEnc  = "0100112233445566778899aabbccddeeff000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1fa0a1a2a3a4a5a6a7a8a9aaabacadaeaf01404142434445464748494a4b4c4d4e4f505152535455565758595a5b5c5d5e5f0006256d037802400006256d03801bf101808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e9f1e00000007000306090c0f1215181b1e2124272a2d303336393c3f4245484b4e5154575a5d606366696c6f7275787b7e8184878a8d909396999c9fa2a5a8abaeb1b4b7babd01060b10151a1f24292e33383d42474c51565b60656a6f74797e83888d92979ca1a6abb0b5babfc4c9ced3d8dde2e7ecf1f6fb00050a0f14191e23282d32373c"
	goldenFullLeaf = "d093e5e62de9465fa3a31689bb46c91443715ec181c4b382272ffd61e69d8a54"

	goldenTimeoutEnc  = "01ffeeddccbbaa99887766554433221100e0e1e2e3e4e5e6e7e8e9eaebecedeeeff0f1f2f3f4f5f6f7f8f9fafbfcfdfeffa0a1a2a3a4a5a6a7a8a9aaabacadaeaf02101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f0006256d038562400006256d07282b80030000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000020910171e252c333a41484f565d646b727980878e959ca3aab1b8bfc6cdd4dbe2e9f0f7fe050c131a21282f363d444b525960676e757c838a91989fa6adb4bb"
	goldenTimeoutLeaf = "5f1f3048b87d4b108ebbd89824d22eba0f383a56a3a6fbe493fad046da9671d7"
)

func TestEncodeGoldenVectors(t *testing.T) {
	cases := []struct {
		name      string
		r         Receipt
		enc, leaf string
	}{
		{"full", fullReceipt(), goldenFullEnc, goldenFullLeaf},
		{"timeout", timeoutReceipt(), goldenTimeoutEnc, goldenTimeoutLeaf},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			enc, err := c.r.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if len(enc) != EncodedSize {
				t.Fatalf("encoding is %d bytes, want %d", len(enc), EncodedSize)
			}
			if got := hex.EncodeToString(enc); got != c.enc {
				t.Fatalf("encoding mismatch\n got %s\nwant %s", got, c.enc)
			}
			leaf, err := c.r.LeafHash()
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(leaf[:]); got != c.leaf {
				t.Fatalf("leaf hash = %s, want %s", got, c.leaf)
			}
		})
	}
}

func TestEncodedSizeIs280(t *testing.T) {
	if EncodedSize != 280 {
		t.Fatalf("EncodedSize = %d; the published layout is 280 bytes", EncodedSize)
	}
}

func TestEncodeRejectsInvalidReceipts(t *testing.T) {
	bad := fullReceipt()
	bad.Result = 0
	if _, err := bad.Encode(); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("result 0: err = %v, want ErrInvalidResult", err)
	}
	bad.Result = 4
	if _, err := bad.Encode(); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("result 4: err = %v, want ErrInvalidResult", err)
	}
	noTS := fullReceipt()
	noTS.ServiceCountersignTS = time.Time{}
	if _, err := noTS.Encode(); !errors.Is(err, ErrZeroTimestamp) {
		t.Fatalf("zero timestamp: err = %v, want ErrZeroTimestamp", err)
	}
}

func TestEncodeDistinguishesAbsentFromAllZero(t *testing.T) {
	a := fullReceipt()
	b := fullReceipt()
	zero := [32]byte{}
	a.ResponseHash = nil
	b.ResponseHash = &zero
	la, errA := a.LeafHash()
	lb, errB := b.LeafHash()
	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	if la == lb {
		t.Fatal("a NULL response_hash and an all-zero response_hash hash to the same leaf")
	}
}

func TestEveryFieldChangesTheLeaf(t *testing.T) {
	base, err := fullReceipt().LeafHash()
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*Receipt){
		"receipt_id":     func(r *Receipt) { r.ReceiptID[0] ^= 1 },
		"chunk_id":       func(r *Receipt) { r.ChunkID[31] ^= 1 },
		"provider_id":    func(r *Receipt) { r.ProviderID[0] ^= 1 },
		"nonce":          func(r *Receipt) { r.ChallengeNonce[32] ^= 1 },
		"challenge_ts":   func(r *Receipt) { r.ServerChallengeTS = r.ServerChallengeTS.Add(time.Microsecond) },
		"countersign_ts": func(r *Receipt) { r.ServiceCountersignTS = r.ServiceCountersignTS.Add(time.Microsecond) },
		"result":         func(r *Receipt) { r.Result = ResultFail },
		"response_hash":  func(r *Receipt) { r.ResponseHash[0] ^= 1 },
		"latency":        func(r *Receipt) { v := uint32(8); r.ResponseLatencyMS = &v },
		"jit":            func(r *Receipt) { r.JITFlag = true },
		"stale":          func(r *Receipt) { r.AddressWasStale = false },
		"provider_sig":   func(r *Receipt) { r.ProviderSig[63] ^= 1 },
		"service_sig":    func(r *Receipt) { r.ServiceSig[0] ^= 1 },
	}
	for name, mutate := range mutations {
		r := fullReceipt()
		mutate(&r)
		got, err := r.LeafHash()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got == base {
			t.Errorf("changing %s did not change the leaf hash", name)
		}
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	for _, r := range []Receipt{fullReceipt(), timeoutReceipt()} {
		enc, err := r.Encode()
		if err != nil {
			t.Fatal(err)
		}
		back, err := Decode(enc)
		if err != nil {
			t.Fatal(err)
		}
		again, err := back.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(enc) != hex.EncodeToString(again) {
			t.Fatal("decode then encode did not reproduce the original bytes")
		}
		if !back.ServerChallengeTS.Equal(r.ServerChallengeTS) || !back.ServiceCountersignTS.Equal(r.ServiceCountersignTS) {
			t.Fatal("timestamps changed in a round trip")
		}
	}
}

func TestDecodeRejectsMalformedEncodings(t *testing.T) {
	good, err := fullReceipt().Encode()
	if err != nil {
		t.Fatal(err)
	}
	clone := func() []byte { return append([]byte(nil), good...) }

	if _, err := Decode(good[:EncodedSize-1]); !errors.Is(err, ErrBadLength) {
		t.Errorf("short: err = %v, want ErrBadLength", err)
	}
	v := clone()
	v[offVersion] = 2
	if _, err := Decode(v); !errors.Is(err, ErrBadVersion) {
		t.Errorf("version: err = %v, want ErrBadVersion", err)
	}
	res := clone()
	res[offResult] = 9
	if _, err := Decode(res); !errors.Is(err, ErrInvalidResult) {
		t.Errorf("result: err = %v, want ErrInvalidResult", err)
	}
	fl := clone()
	fl[offFlags] |= 0x80
	if _, err := Decode(fl); !errors.Is(err, ErrBadFlags) {
		t.Errorf("undefined flag: err = %v, want ErrBadFlags", err)
	}
	// provider_sig bytes present while the presence bit is clear
	to, err := timeoutReceipt().Encode()
	if err != nil {
		t.Fatal(err)
	}
	to[offProviderSig] = 1
	if _, err := Decode(to); !errors.Is(err, ErrBadFlags) {
		t.Errorf("stray provider_sig bytes: err = %v, want ErrBadFlags", err)
	}
}

func TestResultStringAndParse(t *testing.T) {
	for _, r := range []Result{ResultPass, ResultFail, ResultTimeout} {
		back, err := ParseResult(r.String())
		if err != nil || back != r {
			t.Errorf("round trip of %v failed: %v %v", r, back, err)
		}
	}
	if _, err := ParseResult("PENDING"); !errors.Is(err, ErrInvalidResult) {
		t.Errorf("PENDING: err = %v, want ErrInvalidResult", err)
	}
}

func TestSortCanonicalOrdersByCountersignThenReceiptID(t *testing.T) {
	mk := func(sec int64, id byte) Receipt {
		r := fullReceipt()
		r.ServiceCountersignTS = time.Unix(sec, 0)
		r.ReceiptID = [16]byte{id}
		return r
	}
	rs := []Receipt{mk(20, 1), mk(10, 9), mk(10, 2), mk(15, 5)}
	SortCanonical(rs)
	want := [][2]int64{{10, 2}, {10, 9}, {15, 5}, {20, 1}}
	for i, r := range rs {
		if r.ServiceCountersignTS.Unix() != want[i][0] || int64(r.ReceiptID[0]) != want[i][1] {
			t.Fatalf("position %d = (%d,%d), want %v", i, r.ServiceCountersignTS.Unix(), r.ReceiptID[0], want[i])
		}
	}
}

func TestLeafHashesNamesTheBadReceipt(t *testing.T) {
	bad := fullReceipt()
	bad.Result = 0
	_, err := LeafHashes([]Receipt{fullReceipt(), bad})
	if !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("err = %v, want ErrInvalidResult", err)
	}
}

// ---- JSON ----

func TestJSONRoundTripPreservesTheLeaf(t *testing.T) {
	for _, r := range []Receipt{fullReceipt(), timeoutReceipt()} {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var back Receipt
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("%v\n%s", err, raw)
		}
		l1, _ := r.LeafHash()
		l2, err := back.LeafHash()
		if err != nil {
			t.Fatal(err)
		}
		if l1 != l2 {
			t.Fatalf("leaf changed through JSON\n%s", raw)
		}
	}
}

func TestJSONWritesNullForAbsentFieldsAndCanonicalUUIDs(t *testing.T) {
	raw, err := json.Marshal(timeoutReceipt())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"response_hash", "response_latency_ms", "provider_sig"} {
		if v, ok := m[k]; !ok || v != nil {
			t.Errorf("%s = %v (present=%v), want explicit null", k, v, ok)
		}
	}
	if m["receipt_id"] != "ffeeddcc-bbaa-9988-7766-554433221100" {
		t.Errorf("receipt_id = %v", m["receipt_id"])
	}
	if m["audit_result"] != "TIMEOUT" {
		t.Errorf("audit_result = %v", m["audit_result"])
	}
}

func TestJSONRejectsBadFields(t *testing.T) {
	raw, err := json.Marshal(fullReceipt())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	cases := map[string]any{
		"chunk_id":     "abcd",
		"service_sig":  "zz",
		"audit_result": "PENDING",
		"receipt_id":   "not-a-uuid",
	}
	for field, bad := range cases {
		mm := map[string]any{}
		for k, v := range m {
			mm[k] = v
		}
		mm[field] = bad
		b, _ := json.Marshal(mm)
		var r Receipt
		if err := json.Unmarshal(b, &r); err == nil {
			t.Errorf("bad %s was accepted", field)
		}
	}
}

// ---- database scan ----

// fakeScan returns a scan function that stores vals into the destinations.
func fakeScan(vals ...any) func(dest ...any) error {
	return func(dest ...any) error {
		if len(dest) != len(vals) {
			return fmt.Errorf("fakeScan: %d destinations, %d values", len(dest), len(vals))
		}
		for i, d := range dest {
			rv := reflect.ValueOf(d).Elem()
			if vals[i] == nil {
				rv.Set(reflect.Zero(rv.Type()))
				continue
			}
			rv.Set(reflect.ValueOf(vals[i]))
		}
		return nil
	}
}

func TestScanReceiptMatchesDirectConstruction(t *testing.T) {
	want := fullReceipt()
	got, err := ScanReceipt(fakeScan(
		FormatUUID(want.ReceiptID), want.ChunkID[:], FormatUUID(want.ProviderID), want.ChallengeNonce[:],
		want.ServerChallengeTS, want.ServiceCountersignTS, "PASS",
		want.ResponseHash[:], sql.NullInt64{Int64: 7, Valid: true}, false, true,
		want.ProviderSig[:], want.ServiceSig[:],
	))
	if err != nil {
		t.Fatal(err)
	}
	lw, _ := want.LeafHash()
	lg, err := got.LeafHash()
	if err != nil {
		t.Fatal(err)
	}
	if lw != lg {
		t.Fatal("scanned receipt hashes differently from the directly built one")
	}
}

func TestScanReceiptHandlesNullColumns(t *testing.T) {
	want := timeoutReceipt()
	got, err := ScanReceipt(fakeScan(
		FormatUUID(want.ReceiptID), want.ChunkID[:], FormatUUID(want.ProviderID), want.ChallengeNonce[:],
		want.ServerChallengeTS, want.ServiceCountersignTS, "TIMEOUT",
		nil, sql.NullInt64{}, false, false, nil, want.ServiceSig[:],
	))
	if err != nil {
		t.Fatal(err)
	}
	if got.ResponseHash != nil || got.ResponseLatencyMS != nil || got.ProviderSig != nil {
		t.Fatal("NULL columns must scan to nil")
	}
	lw, _ := want.LeafHash()
	lg, _ := got.LeafHash()
	if lw != lg {
		t.Fatal("NULL-column receipt hashes differently")
	}
}

func TestScanReceiptRejectsBadRows(t *testing.T) {
	r := fullReceipt()
	ok := func() []any {
		return []any{
			FormatUUID(r.ReceiptID), r.ChunkID[:], FormatUUID(r.ProviderID), r.ChallengeNonce[:],
			r.ServerChallengeTS, r.ServiceCountersignTS, "PASS",
			r.ResponseHash[:], sql.NullInt64{Int64: 7, Valid: true}, false, true,
			r.ProviderSig[:], r.ServiceSig[:],
		}
	}
	const (
		idxChunk   = 1
		idxNonce   = 3
		idxResult  = 6
		idxLatency = 8
		idxSvcSig  = 12
	)
	cases := []struct {
		name string
		idx  int
		val  any
		want error
	}{
		{"short chunk id", idxChunk, []byte{1, 2}, ErrBadLength},
		{"32-byte nonce", idxNonce, make([]byte, 32), ErrBadLength},
		{"unknown result", idxResult, "PENDING", ErrInvalidResult},
		{"negative latency", idxLatency, sql.NullInt64{Int64: -1, Valid: true}, ErrLatencyRange},
		{"missing service sig", idxSvcSig, []byte(nil), ErrBadLength},
	}
	for _, c := range cases {
		vals := ok()
		vals[c.idx] = c.val
		if _, err := ScanReceipt(fakeScan(vals...)); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := ScanReceipt(func(...any) error { return errors.New("driver down") }); err == nil {
		t.Error("a scan error must be returned")
	}
}

func TestUUIDHelpers(t *testing.T) {
	id, err := ParseUUID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	if FormatUUID(id) != "00112233-4455-6677-8899-aabbccddeeff" {
		t.Fatal("format/parse round trip failed")
	}
	if _, err := ParseUUID("00112233"); !errors.Is(err, ErrBadUUID) {
		t.Fatalf("err = %v, want ErrBadUUID", err)
	}
}

func TestSQLFragmentsStayInStepWithTheScanner(t *testing.T) {
	// ScanReceipt reads thirteen columns; SelectColumns must list thirteen.
	const columns = 13
	n := 1
	for _, ch := range SelectColumns {
		if ch == ',' {
			n++
		}
	}
	if n != columns {
		t.Fatalf("SelectColumns lists %d columns, ScanReceipt reads %d", n, columns)
	}
}
