// cmd/microservice — see main.go's package doc comment.
//
// config_arkiv.go reads the optional Arkiv mirror settings. They live in the
// environment, not in config.NetworkProfile, so the profile stays free of
// anything an operator must keep secret or per-deployment.
package main

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Environment variables read by the Arkiv extension.
const (
	envArkivEnabled        = "VYOMANAUT_ARKIV_ENABLED"
	envArkivRPCURL         = "VYOMANAUT_ARKIV_RPC_URL"
	envArkivKeyFile        = "VYOMANAUT_ARKIV_PRIVATE_KEY_FILE"
	envArkivAccessKeyFile  = "VYOMANAUT_ARKIV_ACCESS_KEY_FILE"
	envArkivChainID        = "VYOMANAUT_ARKIV_CHAIN_ID"
	envArkivPeriodSeconds  = "VYOMANAUT_ARKIV_PERIOD_SECONDS"
	envArkivPollSeconds    = "VYOMANAUT_ARKIV_POLL_SECONDS"
	envArkivBackfillSecs   = "VYOMANAUT_ARKIV_BACKFILL_SECONDS"
	envArkivManifests      = "VYOMANAUT_ARKIV_MANIFESTS"
	envArkivAppTag         = "VYOMANAUT_ARKIV_APP_TAG"
	envArkivLifetimeBlocks = "VYOMANAUT_ARKIV_LIFETIME_BLOCKS"

	// Rabdi testnet defaults (verified in spike A0, 2026-11-05).
	arkivDefaultRPCURL  = "https://rpc.rabdi.db-chain.testnet.arkiv.network"
	arkivDefaultChainID = 7794095

	arkivMaxSeconds = 1 << 31
	arkivMaxUint    = 1 << 62
	arkivMaxAppTag  = 64
)

var errArkivConfig = errors.New("arkiv settings")

// arkivSettings is the parsed Arkiv configuration.
type arkivSettings struct {
	Enabled        bool
	RPCURL         string // may embed an access key: never log it
	KeyFile        string
	AccessKeyFile  string
	ChainID        uint64 // 0 means do not check
	Period         time.Duration
	Poll           time.Duration
	Backfill       time.Duration
	Manifests      bool
	AppTag         string
	LifetimeBlocks uint64
}

// rpcHost is the endpoint's host name, safe to print.
func (c arkivSettings) rpcHost() string {
	u, err := url.Parse(c.RPCURL)
	if err != nil || u.Host == "" {
		return "(unparsable)"
	}
	return u.Host
}

func parseSwitch(v string) (on, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false, true
	case "1", "true", "yes", "on":
		return true, true
	default:
		return false, false
	}
}

// arkivSettingsFromEnv parses the Arkiv environment. It returns Enabled=false
// with no error when the feature is off; every other problem names the
// variable at fault and never echoes a value.
func arkivSettingsFromEnv(getenv func(string) string) (arkivSettings, error) {
	c := arkivSettings{ChainID: arkivDefaultChainID, Manifests: true}
	on, ok := parseSwitch(getenv(envArkivEnabled))
	if !ok {
		return c, fmt.Errorf("%w: %s must be 1 or 0", errArkivConfig, envArkivEnabled)
	}
	if !on {
		return arkivSettings{}, nil
	}
	c.Enabled = true

	c.RPCURL = strings.TrimSpace(getenv(envArkivRPCURL))
	if c.RPCURL == "" {
		c.RPCURL = arkivDefaultRPCURL
	}
	if u, err := url.Parse(c.RPCURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, fmt.Errorf("%w: %s must be an http(s) URL", errArkivConfig, envArkivRPCURL)
	}
	c.KeyFile = strings.TrimSpace(getenv(envArkivKeyFile))
	if c.KeyFile == "" {
		return c, fmt.Errorf("%w: %s is required when %s=1", errArkivConfig, envArkivKeyFile, envArkivEnabled)
	}
	c.AccessKeyFile = strings.TrimSpace(getenv(envArkivAccessKeyFile))
	c.AppTag = strings.TrimSpace(getenv(envArkivAppTag))
	if len(c.AppTag) > arkivMaxAppTag {
		return c, fmt.Errorf("%w: %s is longer than %d bytes", errArkivConfig, envArkivAppTag, arkivMaxAppTag)
	}

	if v := strings.TrimSpace(getenv(envArkivManifests)); v != "" {
		m, ok := parseSwitch(v)
		if !ok {
			return c, fmt.Errorf("%w: %s must be 1 or 0", errArkivConfig, envArkivManifests)
		}
		c.Manifests = m
	}

	readUint := func(name string, max uint64) (uint64, bool, error) {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			return 0, false, nil
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n > max {
			return 0, false, fmt.Errorf("%w: %s must be a whole number up to %d", errArkivConfig, name, max)
		}
		return n, true, nil
	}
	id, set, err := readUint(envArkivChainID, arkivMaxUint)
	if err != nil {
		return c, err
	}
	if set {
		c.ChainID = id
	}
	secs := map[string]*time.Duration{
		envArkivPeriodSeconds: &c.Period, envArkivPollSeconds: &c.Poll, envArkivBackfillSecs: &c.Backfill,
	}
	for name, dst := range secs {
		n, _, err := readUint(name, arkivMaxSeconds)
		if err != nil {
			return c, err
		}
		*dst = time.Duration(n) * time.Second
	}
	if c.LifetimeBlocks, _, err = readUint(envArkivLifetimeBlocks, arkivMaxUint); err != nil {
		return c, err
	}
	return c, nil
}
