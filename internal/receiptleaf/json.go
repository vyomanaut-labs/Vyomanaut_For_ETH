package receiptleaf

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// jsonRecord is the published JSON form of a receipt. Every field the leaf
// hash covers is present, so a reader can rebuild the leaf from the JSON
// alone. Absent database values are JSON null.
type jsonRecord struct {
	ReceiptID            string  `json:"receipt_id"`
	ChunkID              string  `json:"chunk_id"`
	ProviderID           string  `json:"provider_id"`
	ChallengeNonce       string  `json:"challenge_nonce"`
	ServerChallengeTS    int64   `json:"server_challenge_ts_us"`
	ServiceCountersignTS int64   `json:"service_countersign_ts_us"`
	AuditResult          string  `json:"audit_result"`
	ResponseHash         *string `json:"response_hash"`
	ResponseLatencyMS    *uint32 `json:"response_latency_ms"`
	JITFlag              bool    `json:"jit_flag"`
	AddressWasStale      bool    `json:"address_was_stale"`
	ProviderSig          *string `json:"provider_sig"`
	ServiceSig           string  `json:"service_sig"`
}

// MarshalJSON writes the published form: UUIDs in 8-4-4-4-12 text, binary
// fields in lower-case hex, timestamps in Unix microseconds.
func (r Receipt) MarshalJSON() ([]byte, error) {
	if !r.Result.valid() {
		return nil, fmt.Errorf("%w: %d", ErrInvalidResult, uint8(r.Result))
	}
	if r.ServerChallengeTS.IsZero() || r.ServiceCountersignTS.IsZero() {
		return nil, ErrZeroTimestamp
	}
	rec := jsonRecord{
		ReceiptID:            FormatUUID(r.ReceiptID),
		ChunkID:              hex.EncodeToString(r.ChunkID[:]),
		ProviderID:           FormatUUID(r.ProviderID),
		ChallengeNonce:       hex.EncodeToString(r.ChallengeNonce[:]),
		ServerChallengeTS:    r.ServerChallengeTS.UnixMicro(),
		ServiceCountersignTS: r.ServiceCountersignTS.UnixMicro(),
		AuditResult:          r.Result.String(),
		ResponseLatencyMS:    r.ResponseLatencyMS,
		JITFlag:              r.JITFlag,
		AddressWasStale:      r.AddressWasStale,
		ServiceSig:           hex.EncodeToString(r.ServiceSig[:]),
	}
	if r.ResponseHash != nil {
		s := hex.EncodeToString(r.ResponseHash[:])
		rec.ResponseHash = &s
	}
	if r.ProviderSig != nil {
		s := hex.EncodeToString(r.ProviderSig[:])
		rec.ProviderSig = &s
	}
	return json.Marshal(rec)
}

// UnmarshalJSON reads the published form and validates every field width.
func (r *Receipt) UnmarshalJSON(data []byte) error {
	var rec jsonRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("receiptleaf: %w", err)
	}
	var out Receipt
	var err error
	if out.ReceiptID, err = ParseUUID(rec.ReceiptID); err != nil {
		return err
	}
	if out.ProviderID, err = ParseUUID(rec.ProviderID); err != nil {
		return err
	}
	if err = decodeHexExact(out.ChunkID[:], rec.ChunkID, "chunk_id"); err != nil {
		return err
	}
	if err = decodeHexExact(out.ChallengeNonce[:], rec.ChallengeNonce, "challenge_nonce"); err != nil {
		return err
	}
	if err = decodeHexExact(out.ServiceSig[:], rec.ServiceSig, "service_sig"); err != nil {
		return err
	}
	if out.Result, err = ParseResult(rec.AuditResult); err != nil {
		return err
	}
	if rec.ResponseHash != nil {
		var h [sizeHash]byte
		if err = decodeHexExact(h[:], *rec.ResponseHash, "response_hash"); err != nil {
			return err
		}
		out.ResponseHash = &h
	}
	if rec.ProviderSig != nil {
		var s [sizeSig]byte
		if err = decodeHexExact(s[:], *rec.ProviderSig, "provider_sig"); err != nil {
			return err
		}
		out.ProviderSig = &s
	}
	out.ServerChallengeTS = timeFromMicro(rec.ServerChallengeTS)
	out.ServiceCountersignTS = timeFromMicro(rec.ServiceCountersignTS)
	out.ResponseLatencyMS = rec.ResponseLatencyMS
	out.JITFlag = rec.JITFlag
	out.AddressWasStale = rec.AddressWasStale
	*r = out
	return nil
}

func decodeHexExact(dst []byte, s, field string) error {
	raw, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrBadHex, field, err)
	}
	return copyExact(dst, raw, field)
}
