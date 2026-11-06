package anchor

import (
	"errors"
	"fmt"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// Config tunes a Service. Zero fields take their defaults.
type Config struct {
	// Interval is how often a new epoch is built.
	Interval time.Duration
	// Lag is how old a receipt must be before it is read. It must not be
	// shorter than receiptleaf.SettleLag, because the second phase of a receipt
	// write can commit slightly out of timestamp order.
	Lag time.Duration
	// MaxLeaves is the most receipts one epoch may hold.
	MaxLeaves int
}

const (
	// DefaultInterval is how often a new epoch is built unless configured.
	DefaultInterval  = 10 * time.Minute
	defaultMaxLeaves = 20000
	maxLeavesCeiling = 1 << 31 // the contract stores the count as uint32
)

var errConfig = errors.New("anchor config")

func (c Config) withDefaults() Config {
	if c.Interval == 0 {
		c.Interval = DefaultInterval
	}
	if c.Lag == 0 {
		c.Lag = receiptleaf.SettleLag
	}
	if c.MaxLeaves == 0 {
		c.MaxLeaves = defaultMaxLeaves
	}
	return c
}

func (c Config) validate() error {
	if c.Interval < 0 {
		return fmt.Errorf("%w: interval must not be negative", errConfig)
	}
	if c.Lag < receiptleaf.SettleLag {
		return fmt.Errorf("%w: lag %s is shorter than the shared settle lag %s", errConfig, c.Lag, receiptleaf.SettleLag)
	}
	if c.MaxLeaves < 1 || c.MaxLeaves > maxLeavesCeiling {
		return fmt.Errorf("%w: max leaves must be between 1 and %d", errConfig, maxLeavesCeiling)
	}
	return nil
}
