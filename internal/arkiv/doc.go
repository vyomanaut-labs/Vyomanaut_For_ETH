// Package arkiv is a small client for the Arkiv entity database (Rabdi testnet
// for the hackathon build), written against raw Ethereum JSON-RPC.
//
// It is the Arkiv half of the optional "mirror" feature (cmd/microservice/
// arkiv_ext.go wires it in). It knows nothing about audits, files or
// Postgres: callers hand it attributes and get back an entity key.
//
// Wire format (verified against the live Rabdi chain, spike A0, 2026-11-05):
//
//   - A create is ONE transaction to Address with value 0 and calldata
//     executeSelector || abi.encode(Op[]), where Op = (uint8 operation,
//     bytes operationData) and operation 1 is create.
//   - operationData is the five create fields wrapped as ONE tuple: a 32-byte
//     offset word (0x20) followed by abi.encode(uint128 salt, uint64
//     expiresAt, uint64 minLifetime, uint8 creationFlags, Attribute[]).
//     Encoding the five fields as plain parameters reverts on chain.
//   - Attribute = (bytes32 name, uint8 typeId, bytes value); names are ASCII,
//     left-aligned and zero-padded, and the list must be sorted ascending by
//     the 32 name bytes. typeId OR 0x80 marks an attribute as indexed (bytes
//     attributes cannot be indexed).
//   - The entity key is read from the EntityCreated log (topics[1]). It can
//     also be predicted (PredictKey), which the tests use as a cross-check.
//   - Reads use the arkiv_query JSON-RPC method.
//
// Rules for callers:
//
//   - Arkiv is a mirror, not a dependency. A failed Create must never block
//     uploads, audits or payments; callers back off and retry.
//   - A Client serialises its own transactions (one writer, nonce order).
//     Do not share one wallet between two Clients.
//   - Never log the access key or the wallet key, and never log the RPC URL
//     if the access key was put in its path.
//   - Keep payloads at or below VerifiedMaxBytes. Larger creates (up to the
//     128 KiB protocol limit) have not been tried on chain.
package arkiv
