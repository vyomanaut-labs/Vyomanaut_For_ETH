package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func validChainEnv() map[string]string {
	return map[string]string{
		envChainEnabled:    "1",
		envChainRPCURL:     "https://rpc.example.org/v1/SECRET-KEY-123",
		envChainKeyFile:    "/keys/coordinator.key",
		envChainEscrow:     "0xBcE841C78c6c40C551fc223Ba9415D4751Af5958",
		envChainStartBlock: "12326265",
	}
}

func TestChainIsOffByDefault(t *testing.T) {
	for _, v := range []string{"", "0", "false", "off", " no "} {
		c, err := chainSettingsFromEnv(env(map[string]string{envChainEnabled: v}))
		if err != nil || c.Enabled {
			t.Errorf("%q: enabled=%v err=%v; want off with no error", v, c.Enabled, err)
		}
	}
	if c, err := chainSettingsFromEnv(env(nil)); err != nil || c.Enabled {
		t.Errorf("empty environment: enabled=%v err=%v", c.Enabled, err)
	}
}

func TestChainSettingsParseWithDefaultsLeftToThePackage(t *testing.T) {
	c, err := chainSettingsFromEnv(env(validChainEnv()))
	if err != nil || !c.Enabled {
		t.Fatalf("enabled=%v err=%v", c.Enabled, err)
	}
	if c.StartBlock != 12326265 || c.KeyFile != "/keys/coordinator.key" || c.Escrow.IsZero() {
		t.Fatalf("settings = %+v", c)
	}
	if c.MicroPerPaise != 0 || c.Confirmations != 0 || c.Poll != 0 || c.SettleLag != 0 {
		t.Fatalf("unset optional values must stay zero so chainsettle applies its defaults: %+v", c)
	}
}

func TestChainOptionalSettingsAreRead(t *testing.T) {
	m := validChainEnv()
	m[envChainMicroPerPaise] = "25"
	m[envChainConfirmations] = "4"
	m[envChainPollSeconds] = "7"
	m[envChainSettleLagSecs] = "30"
	m[envChainChainID] = "1874"
	c, err := chainSettingsFromEnv(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.MicroPerPaise != 25 || c.Confirmations != 4 || c.Poll != 7*time.Second || c.SettleLag != 30*time.Second || c.ChainID != 1874 {
		t.Fatalf("settings = %+v", c)
	}
}

func TestEveryRequiredSettingIsNamedWhenMissing(t *testing.T) {
	for _, name := range []string{envChainRPCURL, envChainKeyFile, envChainEscrow, envChainStartBlock} {
		m := validChainEnv()
		delete(m, name)
		_, err := chainSettingsFromEnv(env(m))
		if !errors.Is(err, errChainConfig) || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: err = %v", name, err)
		}
	}
}

func TestBadValuesAreRejectedWithoutEchoingSecrets(t *testing.T) {
	cases := map[string]map[string]string{
		"enabled":   {envChainEnabled: "maybe"},
		"url":       {envChainRPCURL: "ftp://SECRET-KEY-123@host/x"},
		"address":   {envChainEscrow: "0x1234"},
		"start":     {envChainStartBlock: "soon"},
		"micro":     {envChainMicroPerPaise: "-3"},
		"poll":      {envChainPollSeconds: "fast"},
		"too large": {envChainPollSeconds: "99999999999999999999"},
	}
	for name, override := range cases {
		m := validChainEnv()
		for k, v := range override {
			m[k] = v
		}
		_, err := chainSettingsFromEnv(env(m))
		if !errors.Is(err, errChainConfig) {
			t.Errorf("%s: err = %v, want errChainConfig", name, err)
			continue
		}
		if strings.Contains(err.Error(), "SECRET-KEY-123") {
			t.Errorf("%s: the error leaks the endpoint: %v", name, err)
		}
	}
}

func TestRPCHostHidesThePathAndAnyKeyInIt(t *testing.T) {
	c, err := chainSettingsFromEnv(env(validChainEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if h := c.rpcHost(); h != "rpc.example.org" || strings.Contains(h, "SECRET") {
		t.Fatalf("host = %q", h)
	}
}

func TestAnchoringIsOffUnlessAnAddressIsGiven(t *testing.T) {
	c, err := chainSettingsFromEnv(env(validChainEnv()))
	if err != nil || c.anchoring() {
		t.Fatalf("anchoring=%v err=%v; want off", c.anchoring(), err)
	}
}

func TestAnchoringSettingsAreRead(t *testing.T) {
	m := validChainEnv()
	m[envChainAnchor] = "0x1b2b623ecA0A7899C6a24eaf401a99d2434FDB3d"
	m[envChainAnchorIntervalSec] = "120"
	m[envChainAnchorMaxLeaves] = "500"
	c, err := chainSettingsFromEnv(env(m))
	if err != nil || !c.anchoring() {
		t.Fatalf("anchoring=%v err=%v", c.anchoring(), err)
	}
	if c.AnchorInterval != 120*time.Second || c.AnchorMaxLeaves != 500 {
		t.Fatalf("settings = %+v", c)
	}
}

func TestAnchoringRejectsAnAddressThatIsNotOne(t *testing.T) {
	m := validChainEnv()
	m[envChainAnchor] = "not-an-address"
	_, err := chainSettingsFromEnv(env(m))
	if !errors.Is(err, errChainConfig) || !strings.Contains(err.Error(), envChainAnchor) || strings.Contains(err.Error(), "not-an-address") {
		t.Fatalf("err = %v; want a message naming the variable but not echoing the value", err)
	}
}
