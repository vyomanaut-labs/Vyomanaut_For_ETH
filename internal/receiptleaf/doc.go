// Package receiptleaf is the single definition of how a finished audit
// receipt becomes a leaf of a Merkle tree, and of the tree itself. Every
// feature that publishes or checks audit history against an outside system
// (a chain anchor, a public mirror, a verification tool) uses this package, so
// two publishers can never disagree about what a receipt hashes to.
//
// The package is pure: standard library only, no database driver, no network.
//
// # What counts as a receipt
//
// Only finished rows of audit_receipts: service_sig, service_countersign_ts
// and audit_result all set (FinalFilter). A pending row is never included.
// Rows are taken in the order CanonicalOrder and only once they are older than
// SettleLag, because the second phase of a receipt write can commit slightly
// out of timestamp order.
//
// # Leaf encoding (schema version 1, EncodedSize = 280 bytes, big endian)
//
//	offset size field
//	     0    1 schema version (1)
//	     1   16 receipt_id
//	    17   32 chunk_id
//	    49   16 provider_id
//	    65   33 challenge_nonce
//	    98    8 server_challenge_ts   (signed Unix microseconds)
//	   106    8 service_countersign_ts (signed Unix microseconds)
//	   114    1 audit_result          (1 PASS, 2 FAIL, 3 TIMEOUT)
//	   115   32 response_hash         (zero when absent)
//	   147    1 flags: bit0 jit_flag, bit1 address_was_stale,
//	                   bit2 response_hash present, bit3 latency present,
//	                   bit4 provider_sig present
//	   148    4 response_latency_ms   (zero when absent)
//	   152   64 provider_sig          (zero when absent)
//	   216   64 service_sig
//
// The presence bits keep "absent" and "all zero" distinct. file_id is
// deliberately not part of a receipt record: publishing it next to a provider
// id would reveal which provider holds which file.
//
// # Tree (RFC 6962 section 2.1)
//
//	leaf = SHA-256(0x00 || encoding)
//	node = SHA-256(0x01 || left || right)
//
// An empty tree hashes the empty string. Inclusion proofs and their
// verification follow RFC 6962 and RFC 9162 section 2.1.3.
package receiptleaf
