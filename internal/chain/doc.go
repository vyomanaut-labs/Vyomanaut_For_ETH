// Package chain talks to an EVM chain (Whitechain Sepolia in the demo) over
// plain JSON-RPC. It exists so the coordinator can anchor audit roots and
// settle provider payouts without carrying a full Ethereum client library.
//
// What it contains:
//
//   - Key: a secp256k1 signing key loaded from a file, never from the
//     environment.
//   - Tx: an EIP-1559 transaction with RLP encoding and signing.
//   - A small ABI encoder and decoder for the handful of calls the contracts
//     in /contracts need.
//   - RPC: a JSON-RPC client with timeouts, bounded responses, and error text
//     that never contains the endpoint URL (an RPC URL may embed an API key).
//   - Sender: sends one transaction at a time from one key, simulates it first,
//     pads the gas estimate, tracks its own nonce, and waits for the receipt.
//   - Typed wrappers for AuditAnchor, VyomaEscrow and an ERC-20 token.
//
// Nothing here reads the environment or the database; callers pass in
// configuration. Everything that talks to a node takes a context.
package chain
