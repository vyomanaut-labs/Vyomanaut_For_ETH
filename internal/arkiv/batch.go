package arkiv

import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// Audit batches.
//
// One audit batch is the finished receipts of ONE provider whose
// service_countersign_ts falls in ONE period [PeriodStart, PeriodEnd), in the
// canonical receiptleaf order. A provider with more receipts in a period than
// MaxReceiptsPerBatch gets several batches, numbered by Seq from 0.
//
// The batch is published as one immutable entity. Its body is JSON (the
// receiptleaf published form of every receipt, so anyone can rebuild every
// leaf), and its root is receiptleaf.MerkleRoot over those leaves. The
// coordinator signs a fixed-layout summary (AuditBatchSigningBytes), never
// JSON.

const (
	// KindAuditBatch, KindFileManifest and KindFileTombstone are the values of
	// the "kind" attribute and of arkiv_publications.kind.
	KindAuditBatch    = "audit_batch"
	KindFileManifest  = "file_manifest"
	KindFileTombstone = "file_tombstone"

	// SchemaVersion is the "schema" attribute of every entity this package
	// publishes.
	SchemaVersion uint64 = 1

	// MaxReceiptsPerBatch keeps a batch body under VerifiedMaxBytes: one
	// receipt record is about 870 bytes of JSON, so 100 records are about
	// 87 KiB.
	MaxReceiptsPerBatch = 100

	// DefaultAppTag is the default value of the "app" attribute; every query
	// this package makes filters on it.
	DefaultAppTag = "vyomanaut-demo"

	auditBatchBodySchema = "vyomanaut.arkiv.audit_batch.v1"
	domainTagSize        = 32
)

// Attribute names shared by every kind.
const (
	AttrApp       = "app"
	AttrKind      = "kind"
	AttrSchema    = "schema"
	AttrCoordKey  = "coord_key"
	AttrCoordSig  = "coord_sig"
	AttrBody      = "body"
	AttrProvider  = "provider_id"
	AttrPeriod    = "period_start"
	AttrPeriodEnd = "period_end"
	AttrSeq       = "seq"
	AttrCount     = "count"
	AttrPass      = "pass"
	AttrFail      = "fail"
	AttrTimeout   = "timeout"
	AttrRoot      = "merkle_root"
	AttrFileID    = "file_id"
)

var (
	// ErrBadBatch means a published batch does not hold together: its body,
	// attributes, root or signature disagree.
	ErrBadBatch = errors.New("arkiv: invalid audit batch")

	domainAuditBatch = domainTag("vyomanaut/arkiv/audit_batch/v1")
)

func domainTag(s string) [domainTagSize]byte {
	var out [domainTagSize]byte
	copy(out[:], s)
	return out
}

// AuditBatch is one provider's finished receipts for one period (or one slice
// of them, when Seq > 0).
type AuditBatch struct {
	ProviderID  [16]byte
	PeriodStart time.Time
	PeriodEnd   time.Time
	Seq         uint32
	Receipts    []receiptleaf.Receipt // canonical order
	Root        [32]byte
	Pass        uint32
	Fail        uint32
	Timeout     uint32
}

// Reference is the arkiv_publications.reference of the batch:
// "<provider_id>:<period_start_unix>:<seq>".
func (b AuditBatch) Reference() string {
	return BatchReference(b.ProviderID, b.PeriodStart, b.Seq)
}

// BatchReference builds an audit batch reference.
func BatchReference(provider [16]byte, periodStart time.Time, seq uint32) string {
	return fmt.Sprintf("%s:%d:%d", receiptleaf.FormatUUID(provider), periodStart.Unix(), seq)
}

// ParseBatchReference is the inverse of BatchReference.
func ParseBatchReference(ref string) (provider [16]byte, periodStart time.Time, seq uint32, err error) {
	const referenceParts = 3 // provider, period start, seq
	parts := strings.Split(ref, ":")
	if len(parts) != referenceParts {
		return provider, periodStart, 0, fmt.Errorf("arkiv: bad batch reference %q", ref)
	}
	if provider, err = receiptleaf.ParseUUID(parts[0]); err != nil {
		return provider, periodStart, 0, err
	}
	start, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return provider, periodStart, 0, fmt.Errorf("arkiv: bad batch reference %q: %w", ref, err)
	}
	s, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil {
		return provider, periodStart, 0, fmt.Errorf("arkiv: bad batch reference %q: %w", ref, err)
	}
	return provider, time.Unix(start, 0).UTC(), uint32(s), nil
}

// PeriodStart returns the start of the period of length period that contains
// t. Periods are aligned to the Unix epoch, so every coordinator and every
// verifier computes the same boundaries.
func PeriodStart(t time.Time, period time.Duration) time.Time {
	return t.Truncate(period).UTC()
}

// BuildAuditBatches groups receipts (all from one period, in canonical order)
// into batches: one per provider, split every MaxReceiptsPerBatch receipts.
// The result is ordered by provider id, then Seq, so it is deterministic.
func BuildAuditBatches(receipts []receiptleaf.Receipt, periodStart, periodEnd time.Time) ([]AuditBatch, error) {
	byProvider := map[[16]byte][]receiptleaf.Receipt{}
	var order [][16]byte
	for _, r := range receipts {
		if r.ServiceCountersignTS.Before(periodStart) || !r.ServiceCountersignTS.Before(periodEnd) {
			return nil, fmt.Errorf("arkiv: receipt %s is outside the period", receiptleaf.FormatUUID(r.ReceiptID))
		}
		if _, seen := byProvider[r.ProviderID]; !seen {
			order = append(order, r.ProviderID)
		}
		byProvider[r.ProviderID] = append(byProvider[r.ProviderID], r)
	}
	sortProviderIDs(order)

	var out []AuditBatch
	for _, p := range order {
		rs := byProvider[p]
		receiptleaf.SortCanonical(rs)
		for seq := 0; seq*MaxReceiptsPerBatch < len(rs); seq++ {
			end := min((seq+1)*MaxReceiptsPerBatch, len(rs))
			b, err := newAuditBatch(p, periodStart, periodEnd, uint32(seq), rs[seq*MaxReceiptsPerBatch:end])
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
	}
	return out, nil
}

func sortProviderIDs(ids [][16]byte) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && string(ids[j][:]) < string(ids[j-1][:]); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

func newAuditBatch(provider [16]byte, start, end time.Time, seq uint32, rs []receiptleaf.Receipt) (AuditBatch, error) {
	leaves, err := receiptleaf.LeafHashes(rs)
	if err != nil {
		return AuditBatch{}, err
	}
	b := AuditBatch{
		ProviderID: provider, PeriodStart: start.UTC(), PeriodEnd: end.UTC(), Seq: seq,
		Receipts: rs, Root: receiptleaf.MerkleRoot(leaves),
	}
	for _, r := range rs {
		switch r.Result {
		case receiptleaf.ResultPass:
			b.Pass++
		case receiptleaf.ResultFail:
			b.Fail++
		case receiptleaf.ResultTimeout:
			b.Timeout++
		}
	}
	return b, nil
}

// AuditBatchSigningBytes is the fixed-layout message the coordinator signs
// (big endian, 128 bytes):
//
//	32 domain tag "vyomanaut/arkiv/audit_batch/v1" (zero padded)
//	16 provider_id
//	 8 period_start (Unix microseconds)
//	 8 period_end   (Unix microseconds)
//	 4 seq
//	 4 count
//	 4 pass
//	 4 fail
//	 4 timeout
//	32 merkle_root
//	12 zero (reserved)
func AuditBatchSigningBytes(b AuditBatch) []byte {
	const size = 128
	buf := make([]byte, 0, size)
	buf = append(buf, domainAuditBatch[:]...)
	buf = append(buf, b.ProviderID[:]...)
	buf = binary.BigEndian.AppendUint64(buf, uint64(b.PeriodStart.UnixMicro()))
	buf = binary.BigEndian.AppendUint64(buf, uint64(b.PeriodEnd.UnixMicro()))
	buf = binary.BigEndian.AppendUint32(buf, b.Seq)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(b.Receipts)))
	buf = binary.BigEndian.AppendUint32(buf, b.Pass)
	buf = binary.BigEndian.AppendUint32(buf, b.Fail)
	buf = binary.BigEndian.AppendUint32(buf, b.Timeout)
	buf = append(buf, b.Root[:]...)
	return append(buf, make([]byte, size-len(buf))...)
}

// auditBatchBody is the JSON body of an audit batch entity.
type auditBatchBody struct {
	Schema        string                `json:"schema"`
	ProviderID    string                `json:"provider_id"`
	PeriodStartUS int64                 `json:"period_start_us"`
	PeriodEndUS   int64                 `json:"period_end_us"`
	Seq           uint32                `json:"seq"`
	MerkleRoot    string                `json:"merkle_root"`
	CoordKey      string                `json:"coord_key"`
	CoordSig      string                `json:"coord_sig"`
	Receipts      []receiptleaf.Receipt `json:"receipts"`
}

// AuditBatchAttributes signs the batch and returns the attributes of its
// entity. appTag is the "app" attribute (DefaultAppTag when empty).
func AuditBatchAttributes(b AuditBatch, key ed25519.PrivateKey, appTag string) ([]Attribute, int, error) {
	if len(b.Receipts) == 0 {
		return nil, 0, fmt.Errorf("%w: no receipts", ErrBadBatch)
	}
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		return nil, 0, errors.New("arkiv: the signing key is not an Ed25519 key")
	}
	sig := ed25519.Sign(key, AuditBatchSigningBytes(b))
	body, err := json.Marshal(auditBatchBody{
		Schema:        auditBatchBodySchema,
		ProviderID:    receiptleaf.FormatUUID(b.ProviderID),
		PeriodStartUS: b.PeriodStart.UnixMicro(),
		PeriodEndUS:   b.PeriodEnd.UnixMicro(),
		Seq:           b.Seq,
		MerkleRoot:    hex.EncodeToString(b.Root[:]),
		CoordKey:      hex.EncodeToString(pub),
		CoordSig:      hex.EncodeToString(sig),
		Receipts:      b.Receipts,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("arkiv: batch body: %w", err)
	}
	if len(body) > VerifiedMaxBytes {
		return nil, 0, fmt.Errorf("arkiv: batch body is %d bytes, limit %d", len(body), VerifiedMaxBytes)
	}
	if appTag == "" {
		appTag = DefaultAppTag
	}
	attrs, err := buildAttrs(
		str(AttrApp, appTag, true),
		str(AttrKind, KindAuditBatch, true),
		u64(AttrSchema, SchemaVersion, true),
		str(AttrProvider, receiptleaf.FormatUUID(b.ProviderID), true),
		u64(AttrPeriod, uint64(b.PeriodStart.Unix()), true),
		u64(AttrPeriodEnd, uint64(b.PeriodEnd.Unix()), true),
		u64(AttrSeq, uint64(b.Seq), true),
		u64(AttrCount, uint64(len(b.Receipts)), true),
		u64(AttrPass, uint64(b.Pass), true),
		u64(AttrFail, uint64(b.Fail), true),
		u64(AttrTimeout, uint64(b.Timeout), true),
		str(AttrRoot, hex.EncodeToString(b.Root[:]), true),
		str(AttrCoordKey, hex.EncodeToString(pub), true),
		str(AttrCoordSig, hex.EncodeToString(sig), false),
		bytesAttr(AttrBody, body),
	)
	return attrs, len(body), err
}

// DecodeAuditBatch reads a batch back from an entity body and checks that it
// holds together: the receipts are in canonical order and inside the period,
// they rebuild the stated root, the counts match, and the coordinator
// signature verifies under the stated key. It returns the batch and the
// coordinator key; the caller decides whether that key is the one it trusts.
func DecodeAuditBatch(body []byte) (AuditBatch, ed25519.PublicKey, error) {
	var raw auditBatchBody
	if err := json.Unmarshal(body, &raw); err != nil {
		return AuditBatch{}, nil, fmt.Errorf("%w: body: %v", ErrBadBatch, err)
	}
	if raw.Schema != auditBatchBodySchema {
		return AuditBatch{}, nil, fmt.Errorf("%w: schema %q", ErrBadBatch, raw.Schema)
	}
	provider, err := receiptleaf.ParseUUID(raw.ProviderID)
	if err != nil {
		return AuditBatch{}, nil, fmt.Errorf("%w: %v", ErrBadBatch, err)
	}
	start := time.UnixMicro(raw.PeriodStartUS).UTC()
	end := time.UnixMicro(raw.PeriodEndUS).UTC()
	b, err := newAuditBatch(provider, start, end, raw.Seq, raw.Receipts)
	if err != nil {
		return AuditBatch{}, nil, fmt.Errorf("%w: %v", ErrBadBatch, err)
	}
	for i, r := range b.Receipts {
		if r.ProviderID != provider {
			return b, nil, fmt.Errorf("%w: receipt %d belongs to another provider", ErrBadBatch, i)
		}
		if r.ServiceCountersignTS.Before(start) || !r.ServiceCountersignTS.Before(end) {
			return b, nil, fmt.Errorf("%w: receipt %d is outside the period", ErrBadBatch, i)
		}
		if i > 0 && !receiptleaf.Less(b.Receipts[i-1], r) {
			return b, nil, fmt.Errorf("%w: receipts are not in canonical order", ErrBadBatch)
		}
	}
	if hex.EncodeToString(b.Root[:]) != raw.MerkleRoot {
		return b, nil, fmt.Errorf("%w: the receipts do not rebuild the stated Merkle root", ErrBadBatch)
	}
	pub, err := hex.DecodeString(raw.CoordKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return b, nil, fmt.Errorf("%w: bad coordinator key", ErrBadBatch)
	}
	sig, err := hex.DecodeString(raw.CoordSig)
	if err != nil || !ed25519.Verify(pub, AuditBatchSigningBytes(b), sig) {
		return b, nil, fmt.Errorf("%w: the coordinator signature does not verify", ErrBadBatch)
	}
	return b, ed25519.PublicKey(pub), nil
}

// ---------- attribute helpers ----------

type attrResult struct {
	a   Attribute
	err error
}

func str(name, v string, indexed bool) attrResult {
	a, err := NewStr(name, v, indexed)
	return attrResult{a, err}
}

func u64(name string, v uint64, indexed bool) attrResult {
	a, err := NewU64(name, v, indexed)
	return attrResult{a, err}
}

func bytesAttr(name string, v []byte) attrResult {
	a, err := NewBytes(name, v)
	return attrResult{a, err}
}

func buildAttrs(rs ...attrResult) ([]Attribute, error) {
	out := make([]Attribute, 0, len(rs))
	for _, r := range rs {
		if r.err != nil {
			return nil, r.err
		}
		out = append(out, r.a)
	}
	return normalize(out)
}
