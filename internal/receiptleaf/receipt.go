package receiptleaf

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"time"
)

// Parameters every publisher shares.
const (
	// SchemaVersion is the version byte at the start of every encoding.
	SchemaVersion byte = 1
	// LeafPrefix and NodePrefix are the RFC 6962 domain-separation bytes.
	LeafPrefix byte = 0x00
	NodePrefix byte = 0x01
	// SettleLag is how long a publisher waits after a receipt's countersign
	// time before reading it, so late-committing rows are not skipped.
	SettleLag = 30 * time.Second
)

// Field widths in bytes.
const (
	sizeUUID    = 16
	sizeChunkID = 32
	sizeNonce   = 33
	sizeHash    = 32
	sizeSig     = 64
	sizeTS      = 8
	sizeLatency = 4
	sizeByte    = 1
)

// Field offsets inside the encoding. Each is derived from the previous one so
// the layout cannot drift out of step with the widths above.
const (
	offVersion       = 0
	offReceiptID     = offVersion + sizeByte
	offChunkID       = offReceiptID + sizeUUID
	offProviderID    = offChunkID + sizeChunkID
	offNonce         = offProviderID + sizeUUID
	offChallengeTS   = offNonce + sizeNonce
	offCountersignTS = offChallengeTS + sizeTS
	offResult        = offCountersignTS + sizeTS
	offResponseHash  = offResult + sizeByte
	offFlags         = offResponseHash + sizeHash
	offLatency       = offFlags + sizeByte
	offProviderSig   = offLatency + sizeLatency
	offServiceSig    = offProviderSig + sizeSig

	// EncodedSize is the length of every leaf encoding.
	EncodedSize = offServiceSig + sizeSig
)

// Flag bits in the byte at offFlags.
const (
	flagJIT             byte = 1 << 0
	flagAddressStale    byte = 1 << 1
	flagHasResponseHash byte = 1 << 2
	flagHasLatency      byte = 1 << 3
	flagHasProviderSig  byte = 1 << 4
	flagsDefined             = flagJIT | flagAddressStale | flagHasResponseHash | flagHasLatency | flagHasProviderSig
)

// Result is a final audit outcome.
type Result uint8

// The three final outcomes, numbered as in the encoding.
const (
	ResultPass    Result = 1
	ResultFail    Result = 2
	ResultTimeout Result = 3
)

func (r Result) valid() bool { return r >= ResultPass && r <= ResultTimeout }

// String returns the database spelling of the result.
func (r Result) String() string {
	switch r {
	case ResultPass:
		return "PASS"
	case ResultFail:
		return "FAIL"
	case ResultTimeout:
		return "TIMEOUT"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", uint8(r))
	}
}

// ParseResult converts the database spelling back to a Result.
func ParseResult(s string) (Result, error) {
	for _, r := range []Result{ResultPass, ResultFail, ResultTimeout} {
		if r.String() == s {
			return r, nil
		}
	}
	return 0, fmt.Errorf("%w: %q", ErrInvalidResult, s)
}

// Receipt is one finished audit receipt. Nullable database columns are
// pointers: nil means NULL.
type Receipt struct {
	ReceiptID            [sizeUUID]byte
	ChunkID              [sizeChunkID]byte
	ProviderID           [sizeUUID]byte
	ChallengeNonce       [sizeNonce]byte
	ServerChallengeTS    time.Time // stored to microsecond precision
	ServiceCountersignTS time.Time // stored to microsecond precision
	Result               Result
	ResponseHash         *[sizeHash]byte
	ResponseLatencyMS    *uint32
	JITFlag              bool
	AddressWasStale      bool
	ProviderSig          *[sizeSig]byte
	ServiceSig           [sizeSig]byte
}

// Encode returns the fixed-width encoding of the receipt.
func (r Receipt) Encode() ([]byte, error) {
	if !r.Result.valid() {
		return nil, fmt.Errorf("%w: %d", ErrInvalidResult, uint8(r.Result))
	}
	if r.ServerChallengeTS.IsZero() || r.ServiceCountersignTS.IsZero() {
		return nil, ErrZeroTimestamp
	}

	buf := make([]byte, EncodedSize)
	buf[offVersion] = SchemaVersion
	copy(buf[offReceiptID:], r.ReceiptID[:])
	copy(buf[offChunkID:], r.ChunkID[:])
	copy(buf[offProviderID:], r.ProviderID[:])
	copy(buf[offNonce:], r.ChallengeNonce[:])
	binary.BigEndian.PutUint64(buf[offChallengeTS:], uint64(r.ServerChallengeTS.UnixMicro()))
	binary.BigEndian.PutUint64(buf[offCountersignTS:], uint64(r.ServiceCountersignTS.UnixMicro()))
	buf[offResult] = byte(r.Result)

	var flags byte
	if r.JITFlag {
		flags |= flagJIT
	}
	if r.AddressWasStale {
		flags |= flagAddressStale
	}
	if r.ResponseHash != nil {
		flags |= flagHasResponseHash
		copy(buf[offResponseHash:], r.ResponseHash[:])
	}
	if r.ResponseLatencyMS != nil {
		flags |= flagHasLatency
		binary.BigEndian.PutUint32(buf[offLatency:], *r.ResponseLatencyMS)
	}
	if r.ProviderSig != nil {
		flags |= flagHasProviderSig
		copy(buf[offProviderSig:], r.ProviderSig[:])
	}
	buf[offFlags] = flags
	copy(buf[offServiceSig:], r.ServiceSig[:])
	return buf, nil
}

// Decode is the inverse of Encode. It rejects anything Encode could not have
// produced, so a tool that is handed an encoding can trust what it reads.
func Decode(enc []byte) (Receipt, error) {
	var r Receipt
	if len(enc) != EncodedSize {
		return r, fmt.Errorf("%w: encoding is %d bytes, want %d", ErrBadLength, len(enc), EncodedSize)
	}
	if enc[offVersion] != SchemaVersion {
		return r, fmt.Errorf("%w: %d", ErrBadVersion, enc[offVersion])
	}
	r.Result = Result(enc[offResult])
	if !r.Result.valid() {
		return r, fmt.Errorf("%w: %d", ErrInvalidResult, enc[offResult])
	}
	flags := enc[offFlags]
	if flags&^flagsDefined != 0 {
		return r, fmt.Errorf("%w: undefined bits in %#02x", ErrBadFlags, flags)
	}

	copy(r.ReceiptID[:], enc[offReceiptID:])
	copy(r.ChunkID[:], enc[offChunkID:])
	copy(r.ProviderID[:], enc[offProviderID:])
	copy(r.ChallengeNonce[:], enc[offNonce:])
	r.ServerChallengeTS = time.UnixMicro(int64(binary.BigEndian.Uint64(enc[offChallengeTS:]))).UTC()
	r.ServiceCountersignTS = time.UnixMicro(int64(binary.BigEndian.Uint64(enc[offCountersignTS:]))).UTC()
	r.JITFlag = flags&flagJIT != 0
	r.AddressWasStale = flags&flagAddressStale != 0

	if flags&flagHasResponseHash != 0 {
		var h [sizeHash]byte
		copy(h[:], enc[offResponseHash:])
		r.ResponseHash = &h
	} else if !allZero(enc[offResponseHash : offResponseHash+sizeHash]) {
		return r, fmt.Errorf("%w: response_hash bytes set but flag clear", ErrBadFlags)
	}
	if flags&flagHasLatency != 0 {
		v := binary.BigEndian.Uint32(enc[offLatency:])
		r.ResponseLatencyMS = &v
	} else if !allZero(enc[offLatency : offLatency+sizeLatency]) {
		return r, fmt.Errorf("%w: latency bytes set but flag clear", ErrBadFlags)
	}
	if flags&flagHasProviderSig != 0 {
		var s [sizeSig]byte
		copy(s[:], enc[offProviderSig:])
		r.ProviderSig = &s
	} else if !allZero(enc[offProviderSig : offProviderSig+sizeSig]) {
		return r, fmt.Errorf("%w: provider_sig bytes set but flag clear", ErrBadFlags)
	}
	copy(r.ServiceSig[:], enc[offServiceSig:])
	return r, nil
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// LeafHash returns SHA-256(0x00 || Encode()).
func (r Receipt) LeafHash() ([sizeHash]byte, error) {
	enc, err := r.Encode()
	if err != nil {
		return [sizeHash]byte{}, err
	}
	return sha256.Sum256(append([]byte{LeafPrefix}, enc...)), nil
}

// Less reports whether a sorts before b in the canonical order:
// service_countersign_ts, then receipt_id compared byte by byte. This is also
// the order PostgreSQL gives for CanonicalOrder.
func Less(a, b Receipt) bool {
	if !a.ServiceCountersignTS.Equal(b.ServiceCountersignTS) {
		return a.ServiceCountersignTS.Before(b.ServiceCountersignTS)
	}
	return bytes.Compare(a.ReceiptID[:], b.ReceiptID[:]) < 0
}

// SortCanonical sorts receipts into the canonical order in place.
func SortCanonical(rs []Receipt) {
	sort.SliceStable(rs, func(i, j int) bool { return Less(rs[i], rs[j]) })
}

// LeafHashes returns the leaf hash of every receipt, in the order given.
func LeafHashes(rs []Receipt) ([][sizeHash]byte, error) {
	out := make([][sizeHash]byte, len(rs))
	for i, r := range rs {
		h, err := r.LeafHash()
		if err != nil {
			return nil, fmt.Errorf("receiptleaf: receipt %d: %w", i, err)
		}
		out[i] = h
	}
	return out, nil
}

func timeFromMicro(us int64) time.Time { return time.UnixMicro(us).UTC() }
