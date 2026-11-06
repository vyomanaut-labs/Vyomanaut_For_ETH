package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/config"
)

func validArkivEnv() map[string]string {
	return map[string]string{
		envArkivEnabled: "1",
		envArkivKeyFile: "/keys/arkiv.key",
	}
}

func TestArkivIsOffByDefault(t *testing.T) {
	for _, v := range []string{"", "0", "false", "off", " no "} {
		c, err := arkivSettingsFromEnv(env(map[string]string{envArkivEnabled: v}))
		if err != nil || c.Enabled {
			t.Errorf("%q: enabled=%v err=%v; want off with no error", v, c.Enabled, err)
		}
	}
}

func TestArkivOffIgnoresOtherVariables(t *testing.T) {
	c, err := arkivSettingsFromEnv(env(map[string]string{envArkivPeriodSeconds: "not a number"}))
	if err != nil || c.Enabled {
		t.Fatalf("enabled=%v err=%v", c.Enabled, err)
	}
}

func TestArkivDefaults(t *testing.T) {
	c, err := arkivSettingsFromEnv(env(validArkivEnv()))
	if err != nil || !c.Enabled {
		t.Fatalf("enabled=%v err=%v", c.Enabled, err)
	}
	if c.RPCURL != arkivDefaultRPCURL || c.ChainID != arkivDefaultChainID || !c.Manifests {
		t.Fatalf("defaults = %+v", c)
	}
	if c.Period != 0 || c.Poll != 0 || c.Backfill != 0 || c.LifetimeBlocks != 0 {
		t.Fatalf("unset durations must stay zero so the package defaults apply: %+v", c)
	}
}

func TestArkivOptionalSettingsAreRead(t *testing.T) {
	m := validArkivEnv()
	m[envArkivRPCURL] = "https://rpc.example.org"
	m[envArkivChainID] = "0"
	m[envArkivPeriodSeconds] = "300"
	m[envArkivPollSeconds] = "5"
	m[envArkivBackfillSecs] = "3600"
	m[envArkivManifests] = "0"
	m[envArkivAppTag] = "vyomanaut-test"
	m[envArkivLifetimeBlocks] = "43200"
	c, err := arkivSettingsFromEnv(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.ChainID != 0 || c.Period != 5*time.Minute || c.Poll != 5*time.Second || c.Backfill != time.Hour ||
		c.Manifests || c.AppTag != "vyomanaut-test" || c.LifetimeBlocks != 43200 || c.rpcHost() != "rpc.example.org" {
		t.Fatalf("settings = %+v", c)
	}
}

func TestArkivRejectsBadValuesWithoutEchoingThem(t *testing.T) {
	cases := map[string]string{
		envArkivEnabled:       "maybe",
		envArkivRPCURL:        "ftp://SECRET-host",
		envArkivPeriodSeconds: "-SECRET",
		envArkivManifests:     "SECRET",
		envArkivChainID:       "SECRET",
	}
	for name, v := range cases {
		m := validArkivEnv()
		m[name] = v
		_, err := arkivSettingsFromEnv(env(m))
		if !errors.Is(err, errArkivConfig) {
			t.Errorf("%s=%q: got %v, want errArkivConfig", name, v, err)
			continue
		}
		if strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: error must name the variable and not echo the value: %v", name, err)
		}
	}
	m := validArkivEnv()
	delete(m, envArkivKeyFile)
	if _, err := arkivSettingsFromEnv(env(m)); err == nil || !strings.Contains(err.Error(), envArkivKeyFile) {
		t.Fatalf("missing key file: %v", err)
	}
}

func TestArkivPublisherConfigUsesProfile(t *testing.T) {
	d := extensionDeps{Profile: config.NetworkProfile{DataShards: 3, TotalShards: 5, AuditPeriodDuration: 2 * time.Minute}}
	p, err := arkivPublisherConfig(arkivSettings{Manifests: true}, d)
	if err != nil {
		t.Fatal(err)
	}
	if p.Period != 2*time.Minute || p.RSK != 3 || p.RSN != 5 || !p.Manifests {
		t.Fatalf("publisher config = %+v", p)
	}
	p, err = arkivPublisherConfig(arkivSettings{Period: time.Hour}, d)
	if err != nil || p.Period != time.Hour {
		t.Fatalf("an explicit period must win: %+v %v", p, err)
	}
	if _, err := arkivPublisherConfig(arkivSettings{}, extensionDeps{}); err == nil {
		t.Fatal("a profile without RS parameters must be rejected")
	}
}

func TestStartArkivIsANoOpWhenOff(t *testing.T) {
	t.Setenv(envArkivEnabled, "")
	if err := startArkiv(context.Background(), extensionDeps{}); err != nil {
		t.Fatalf("off: %v", err)
	}
}

func TestStartArkivFailsFastOnMissingKeyFile(t *testing.T) {
	t.Setenv(envArkivEnabled, "1")
	t.Setenv(envArkivKeyFile, t.TempDir()+"/does-not-exist.key")
	d := extensionDeps{
		Profile:    config.NetworkProfile{DataShards: 3, TotalShards: 5},
		SigningKey: make([]byte, 64),
	}
	d.DB = nil
	if err := startArkiv(context.Background(), d); err == nil {
		t.Fatal("a missing database or key file must stop the extension at boot")
	}
}
