package receiptleaf

import "errors"

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrInvalidResult means the audit result is not PASS, FAIL or TIMEOUT.
	ErrInvalidResult = errors.New("receiptleaf: invalid audit result")
	// ErrZeroTimestamp means a required timestamp was not set.
	ErrZeroTimestamp = errors.New("receiptleaf: timestamp not set")
	// ErrBadLength means a fixed-width field or the whole encoding has the
	// wrong length.
	ErrBadLength = errors.New("receiptleaf: unexpected field length")
	// ErrBadVersion means an encoding carries an unknown schema version.
	ErrBadVersion = errors.New("receiptleaf: unknown schema version")
	// ErrBadFlags means an encoding sets flag bits this version does not define,
	// or a presence bit disagrees with the field it describes.
	ErrBadFlags = errors.New("receiptleaf: inconsistent flags")
	// ErrIndexOutOfRange means a leaf index is outside the tree.
	ErrIndexOutOfRange = errors.New("receiptleaf: leaf index out of range")
	// ErrBadUUID means a UUID string is not 32 hex digits (dashes optional).
	ErrBadUUID = errors.New("receiptleaf: malformed uuid")
	// ErrBadHex means a hex field could not be decoded.
	ErrBadHex = errors.New("receiptleaf: malformed hex")
	// ErrLatencyRange means a latency does not fit in 32 bits.
	ErrLatencyRange = errors.New("receiptleaf: latency out of range")
)
