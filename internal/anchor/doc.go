// Package anchor publishes a fingerprint of the audit history to the
// AuditAnchor contract, and checks the database against it later.
//
// Every interval the Service takes the finished audit receipts that have not
// been anchored yet, in the canonical order shared with the other publishers
// (internal/receiptleaf), builds one RFC 6962 Merkle root over them, and writes
// that 32-byte root and the receipt count to the chain as the next epoch. The
// receipts themselves never leave the database.
//
// What this proves, and what it does not: once an epoch is on chain, the
// operator cannot rewrite, drop or add a receipt inside it without the rebuilt
// root changing. A Verifier rebuilds the root from the database and compares it
// with the chain's copy. It does not prove a receipt was ever written in the
// first place: that is what the provider's own signature on a PASS is for.
//
// Crash safety follows the payout settlement: an epoch is RESERVED in the
// database (with its root and the exact range it covers) before anything is
// sent, and before sending the chain is asked whether that epoch is already
// there, so a lost reply is adopted rather than repeated. Epochs are strictly
// increasing, as the contract requires, and a failed epoch leaves the next one
// starting from the same place.
//
// The anchor never blocks audits, uploads or payments: if the chain is
// unreachable the Service retries in the background.
package anchor
