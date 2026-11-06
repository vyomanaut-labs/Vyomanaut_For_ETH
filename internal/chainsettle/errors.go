package chainsettle

import "errors"

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrConfig means the settings are unusable.
	ErrConfig = errors.New("chainsettle: invalid configuration")
	// ErrInsufficientCapacity means the owners' remaining charges or on-chain
	// credit cannot fund a payout.
	ErrInsufficientCapacity = errors.New("chainsettle: owners cannot fund this payout")
	// ErrLockHeld means another coordinator holds the settlement lock.
	ErrLockHeld = errors.New("chainsettle: settlement lock is held by another process")
)
