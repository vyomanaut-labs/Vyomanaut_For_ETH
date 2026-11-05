package chain

import "errors"

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrBadKey means a key file is missing, unreadable, too permissive, or
	// does not hold a valid secp256k1 private key.
	ErrBadKey = errors.New("chain: bad key file")
	// ErrBadAddress means a string is not a 20-byte hex address.
	ErrBadAddress = errors.New("chain: bad address")
	// ErrRPC wraps a failure to reach the node or a malformed reply.
	ErrRPC = errors.New("chain: rpc failure")
	// ErrReverted means the contract rejected the call (simulation or mined).
	ErrReverted = errors.New("chain: execution reverted")
	// ErrTxFailed means a transaction was mined with status 0.
	ErrTxFailed = errors.New("chain: transaction failed on chain")
	// ErrReceiptTimeout means a sent transaction was not mined in time. It may
	// still be mined later.
	ErrReceiptTimeout = errors.New("chain: timed out waiting for receipt")
	// ErrABI means call data or return data did not have the expected shape.
	ErrABI = errors.New("chain: abi mismatch")
	// ErrNoFinality means the node does not report a finalized block.
	ErrNoFinality = errors.New("chain: node reports no finalized block")
)
