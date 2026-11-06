package chainsettle

import (
	"fmt"
	"time"
)

// Config holds the settlement settings. Zero values are replaced by defaults.
type Config struct {
	// MicroPerPaise converts ledger paise to the token's micro-units. At 10,
	// one rupee (100 paise) is 1,000 micro-units, so Rs 1,000 is 1 token.
	MicroPerPaise int64
	// Confirmations is how many blocks deep a Deposited log must be before it
	// is credited.
	Confirmations uint64
	// StartBlock is where the log scan begins when no cursor exists yet: the
	// block the escrow contract was deployed in.
	StartBlock uint64
	// LogChunk is the most blocks one log query covers.
	LogChunk uint64
	// PollInterval is how often the service runs one cycle.
	PollInterval time.Duration
	// SettleLag is how long a RELEASE row ages before it is paid out, so late
	// corrections can still land first.
	SettleLag time.Duration
	// ClaimMargin is added to the clock when deciding a challenge window is over.
	ClaimMargin time.Duration
	// MaxPayees bounds the accounts in one period (posting gas grows with it).
	MaxPayees int
	// MaxItems bounds the ledger rows read for one period.
	MaxItems int
}

const (
	defaultMicroPerPaise = 10
	defaultConfirmations = 2
	defaultLogChunk      = 2000
	defaultPoll          = 10 * time.Second
	defaultSettleLag     = 20 * time.Second
	defaultClaimMargin   = 3 * time.Second
	defaultMaxPayees     = 50
	defaultMaxItems      = 1000
)

// withDefaults returns c with every zero field replaced by its default.
func (c Config) withDefaults() Config {
	if c.MicroPerPaise == 0 {
		c.MicroPerPaise = defaultMicroPerPaise
	}
	if c.Confirmations == 0 {
		c.Confirmations = defaultConfirmations
	}
	if c.LogChunk == 0 {
		c.LogChunk = defaultLogChunk
	}
	if c.PollInterval == 0 {
		c.PollInterval = defaultPoll
	}
	if c.SettleLag == 0 {
		c.SettleLag = defaultSettleLag
	}
	if c.ClaimMargin == 0 {
		c.ClaimMargin = defaultClaimMargin
	}
	if c.MaxPayees == 0 {
		c.MaxPayees = defaultMaxPayees
	}
	if c.MaxItems == 0 {
		c.MaxItems = defaultMaxItems
	}
	return c
}

func (c Config) validate() error {
	switch {
	case c.MicroPerPaise < 1:
		return fmt.Errorf("%w: MicroPerPaise must be at least 1", ErrConfig)
	case c.MaxPayees < 1 || c.MaxItems < 1:
		return fmt.Errorf("%w: MaxPayees and MaxItems must be at least 1", ErrConfig)
	case c.PollInterval < 0 || c.SettleLag < 0 || c.ClaimMargin < 0:
		return fmt.Errorf("%w: durations must not be negative", ErrConfig)
	}
	return nil
}
