package receiptleaf

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
)

// SQL fragments every reader of audit_receipts uses, so two publishers select
// the same rows, in the same order, into the same fields.
const (
	// SelectColumns lists the columns ScanReceipt expects, in order.
	SelectColumns = "receipt_id, chunk_id, provider_id, challenge_nonce, " +
		"server_challenge_ts, service_countersign_ts, audit_result, " +
		"response_hash, response_latency_ms, jit_flag, address_was_stale, " +
		"provider_sig, service_sig"
	// FinalFilter keeps only finished receipts. Use it in every WHERE clause.
	FinalFilter = "service_sig IS NOT NULL AND service_countersign_ts IS NOT NULL AND audit_result IS NOT NULL"
	// CanonicalOrder is the ORDER BY every reader uses.
	CanonicalOrder = "service_countersign_ts, receipt_id"
)

// ScanReceipt reads one row selected with SelectColumns and filtered with
// FinalFilter. Pass rows.Scan (or any function with that shape).
func ScanReceipt(scan func(dest ...any) error) (Receipt, error) {
	var (
		receiptID, providerID, resultText string
		chunkID, nonce, respHash          []byte
		providerSig, serviceSig           []byte
		challengeTS, countersignTS        time.Time
		latency                           sql.NullInt64
		jit, stale                        bool
	)
	if err := scan(&receiptID, &chunkID, &providerID, &nonce, &challengeTS, &countersignTS,
		&resultText, &respHash, &latency, &jit, &stale, &providerSig, &serviceSig); err != nil {
		return Receipt{}, fmt.Errorf("receiptleaf: scan: %w", err)
	}

	var r Receipt
	var err error
	if r.ReceiptID, err = ParseUUID(receiptID); err != nil {
		return Receipt{}, err
	}
	if r.ProviderID, err = ParseUUID(providerID); err != nil {
		return Receipt{}, err
	}
	if err = copyExact(r.ChunkID[:], chunkID, "chunk_id"); err != nil {
		return Receipt{}, err
	}
	if err = copyExact(r.ChallengeNonce[:], nonce, "challenge_nonce"); err != nil {
		return Receipt{}, err
	}
	if err = copyExact(r.ServiceSig[:], serviceSig, "service_sig"); err != nil {
		return Receipt{}, err
	}
	if r.Result, err = ParseResult(resultText); err != nil {
		return Receipt{}, err
	}
	if r.ResponseHash, err = optionalBytes32(respHash, "response_hash"); err != nil {
		return Receipt{}, err
	}
	if r.ProviderSig, err = optionalBytes64(providerSig, "provider_sig"); err != nil {
		return Receipt{}, err
	}
	if latency.Valid {
		if latency.Int64 < 0 || latency.Int64 > math.MaxUint32 {
			return Receipt{}, fmt.Errorf("%w: %d", ErrLatencyRange, latency.Int64)
		}
		v := uint32(latency.Int64)
		r.ResponseLatencyMS = &v
	}
	r.ServerChallengeTS = challengeTS
	r.ServiceCountersignTS = countersignTS
	r.JITFlag = jit
	r.AddressWasStale = stale
	return r, nil
}

// copyExact copies src into dst when the lengths match exactly.
func copyExact(dst, src []byte, field string) error {
	if len(src) != len(dst) {
		return fmt.Errorf("%w: %s is %d bytes, want %d", ErrBadLength, field, len(src), len(dst))
	}
	copy(dst, src)
	return nil
}

func optionalBytes32(src []byte, field string) (*[sizeHash]byte, error) {
	if src == nil {
		return nil, nil
	}
	var out [sizeHash]byte
	if err := copyExact(out[:], src, field); err != nil {
		return nil, err
	}
	return &out, nil
}

func optionalBytes64(src []byte, field string) (*[sizeSig]byte, error) {
	if src == nil {
		return nil, nil
	}
	var out [sizeSig]byte
	if err := copyExact(out[:], src, field); err != nil {
		return nil, err
	}
	return &out, nil
}

// ParseUUID parses 32 hex digits, with or without dashes.
func ParseUUID(s string) ([sizeUUID]byte, error) {
	var out [sizeUUID]byte
	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(raw) != sizeUUID {
		return out, fmt.Errorf("%w: %q", ErrBadUUID, s)
	}
	copy(out[:], raw)
	return out, nil
}

// FormatUUID returns the usual 8-4-4-4-12 lower-case text form.
func FormatUUID(id [sizeUUID]byte) string {
	h := hex.EncodeToString(id[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
