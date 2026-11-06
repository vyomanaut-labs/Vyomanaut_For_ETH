// cmd/microservice — see main.go's package doc comment.
//
// config_chain.go reads the optional chain-settlement settings. They live in
// the environment, not in config.NetworkProfile, so the profile stays free of
// anything an operator must keep secret or per-deployment.
package main

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// Environment variables read by the chain extension.
const (
	envChainEnabled       = "VYOMANAUT_CHAIN_ENABLED"
	envChainRPCURL        = "VYOMANAUT_CHAIN_RPC_URL"
	envChainKeyFile       = "VYOMANAUT_CHAIN_PRIVATE_KEY_FILE"
	envChainEscrow        = "VYOMANAUT_CHAIN_ESCROW_ADDRESS"
	envChainStartBlock    = "VYOMANAUT_CHAIN_START_BLOCK"
	envChainChainID       = "VYOMANAUT_CHAIN_ID"
	envChainMicroPerPaise = "VYOMANAUT_CHAIN_MICRO_PER_PAISE"
	envChainConfirmations = "VYOMANAUT_CHAIN_CONFIRMATIONS"
	envChainPollSeconds   = "VYOMANAUT_CHAIN_POLL_SECONDS"
	envChainSettleLagSecs = "VYOMANAUT_CHAIN_SETTLE_LAG_SECONDS"

	// Audit anchoring is off unless an AuditAnchor address is given.
	envChainAnchor            = "VYOMANAUT_CHAIN_ANCHOR_ADDRESS"
	envChainAnchorIntervalSec = "VYOMANAUT_CHAIN_ANCHOR_INTERVAL_SECONDS"
	envChainAnchorMaxLeaves   = "VYOMANAUT_CHAIN_ANCHOR_MAX_LEAVES"
)

var errChainConfig = errors.New("chain settings")

// chainSettings is the parsed chain configuration.
type chainSettings struct {
	Enabled       bool
	RPCURL        string // may embed an API key: never log it
	KeyFile       string
	Escrow        chain.Address
	StartBlock    uint64
	ChainID       uint64 // 0 means do not check
	MicroPerPaise int64
	Confirmations uint64
	Poll          time.Duration
	SettleLag     time.Duration

	// Audit anchoring. A zero Anchor address leaves it off.
	Anchor          chain.Address
	AnchorInterval  time.Duration
	AnchorMaxLeaves int
}

// anchoring reports whether audit anchoring was asked for.
func (c chainSettings) anchoring() bool { return !c.Anchor.IsZero() }

// rpcHost is the endpoint's host name, safe to print.
func (c chainSettings) rpcHost() string {
	u, err := url.Parse(c.RPCURL)
	if err != nil || u.Host == "" {
		return "(unparsable)"
	}
	return u.Host
}

// chainSettingsFromEnv parses the chain environment. It returns Enabled=false
// with no error when the feature is off; every other problem names the
// variable at fault and never echoes a value.
func chainSettingsFromEnv(getenv func(string) string) (chainSettings, error) {
	var c chainSettings
	switch strings.ToLower(strings.TrimSpace(getenv(envChainEnabled))) {
	case "", "0", "false", "no", "off":
		return c, nil
	case "1", "true", "yes", "on":
		c.Enabled = true
	default:
		return c, fmt.Errorf("%w: %s must be 1 or 0", errChainConfig, envChainEnabled)
	}

	required := func(name string) (string, error) {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			return "", fmt.Errorf("%w: %s is required when %s=1", errChainConfig, name, envChainEnabled)
		}
		return v, nil
	}

	var err error
	if c.RPCURL, err = required(envChainRPCURL); err != nil {
		return c, err
	}
	if u, perr := url.Parse(c.RPCURL); perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, fmt.Errorf("%w: %s must be an http(s) URL", errChainConfig, envChainRPCURL)
	}
	if c.KeyFile, err = required(envChainKeyFile); err != nil {
		return c, err
	}
	escrow, err := required(envChainEscrow)
	if err != nil {
		return c, err
	}
	if c.Escrow, err = chain.ParseAddress(escrow); err != nil {
		return c, fmt.Errorf("%w: %s is not an address", errChainConfig, envChainEscrow)
	}
	start, err := required(envChainStartBlock)
	if err != nil {
		return c, fmt.Errorf("%w (the block the escrow contract was deployed in)", err)
	}
	if c.StartBlock, err = strconv.ParseUint(start, 10, 64); err != nil {
		return c, fmt.Errorf("%w: %s must be a block number", errChainConfig, envChainStartBlock)
	}

	optionalUint := func(name string, dst *uint64) error {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			return nil
		}
		n, perr := strconv.ParseUint(v, 10, 64)
		if perr != nil {
			return fmt.Errorf("%w: %s must be a non-negative whole number", errChainConfig, name)
		}
		*dst = n
		return nil
	}
	var micro, poll, lag, anchorEvery, anchorMax uint64
	for name, dst := range map[string]*uint64{
		envChainChainID: &c.ChainID, envChainConfirmations: &c.Confirmations,
		envChainMicroPerPaise: &micro, envChainPollSeconds: &poll, envChainSettleLagSecs: &lag,
		envChainAnchorIntervalSec: &anchorEvery, envChainAnchorMaxLeaves: &anchorMax,
	} {
		if err := optionalUint(name, dst); err != nil {
			return c, err
		}
	}
	const maxInt = 1 << 62
	if micro > maxInt || poll > maxInt || lag > maxInt || anchorEvery > maxInt || anchorMax > maxInt {
		return c, fmt.Errorf("%w: a numeric setting is out of range", errChainConfig)
	}
	c.MicroPerPaise = int64(micro)
	c.Poll = time.Duration(poll) * time.Second
	c.SettleLag = time.Duration(lag) * time.Second
	c.AnchorInterval = time.Duration(anchorEvery) * time.Second
	c.AnchorMaxLeaves = int(anchorMax)

	if v := strings.TrimSpace(getenv(envChainAnchor)); v != "" {
		if c.Anchor, err = chain.ParseAddress(v); err != nil {
			return c, fmt.Errorf("%w: %s is not an address", errChainConfig, envChainAnchor)
		}
	}
	return c, nil
}
