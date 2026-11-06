// cmd/microservice — see main.go's package doc comment.
//
// arkiv_ext.go is the coordinator's side of the Arkiv mirror. It is wiring
// only: the logic lives in internal/arkiv. It is off unless
// VYOMANAUT_ARKIV_ENABLED=1, and it never blocks or fails the coordinator: if
// Arkiv, the RPC or the wallet is unavailable it keeps retrying in the
// background while uploads, audits, repair and payments carry on unchanged.
// PostgreSQL stays the source of truth; Arkiv holds a signed, public copy.
package main

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/arkiv"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/metrics"
)

const (
	arkivDialTimeout  = 20 * time.Second
	arkivStartRetry   = 15 * time.Second
	arkivBalanceEvery = time.Minute
	arkivMaxRSShards  = 255
)

// weiPerGLM is 1e18: tGLM has 18 decimals.
var weiPerGLM = new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

func init() {
	registerExtension(extension{name: "arkiv", start: startArkiv})
}

// startArkiv validates the settings and the key files immediately, so a typo
// fails loudly at boot, then publishes in a background goroutine.
func startArkiv(ctx context.Context, d extensionDeps) error {
	cfg, err := arkivSettingsFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	if d.DB == nil || len(d.SigningKey) == 0 {
		return errors.New("arkiv: the coordinator gave no database or signing key")
	}
	key, err := arkiv.LoadKeyFile(cfg.KeyFile)
	if err != nil {
		return err
	}
	var access string
	if cfg.AccessKeyFile != "" {
		if access, err = arkiv.ReadSecretFile(cfg.AccessKeyFile); err != nil {
			return err
		}
	}
	pcfg, err := arkivPublisherConfig(cfg, d)
	if err != nil {
		return err
	}
	go runArkiv(ctx, d, cfg, pcfg, key, access)
	return nil
}

// arkivPublisherConfig fills the publisher settings from the environment and
// the active profile. The audit period defaults to the profile's.
func arkivPublisherConfig(cfg arkivSettings, d extensionDeps) (arkiv.PublisherConfig, error) {
	if d.Profile.DataShards <= 0 || d.Profile.TotalShards < d.Profile.DataShards || d.Profile.TotalShards > arkivMaxRSShards {
		return arkiv.PublisherConfig{}, fmt.Errorf("arkiv: the profile's RS parameters (%d of %d) do not fit a manifest",
			d.Profile.DataShards, d.Profile.TotalShards)
	}
	period := cfg.Period
	if period == 0 {
		period = d.Profile.AuditPeriodDuration
	}
	if period <= 0 || period%time.Second != 0 {
		period = arkiv.DefaultPeriod
	}
	return arkiv.PublisherConfig{
		Period: period, Poll: cfg.Poll, Backfill: cfg.Backfill, AppTag: cfg.AppTag,
		Manifests: cfg.Manifests,
		RSK:       uint8(d.Profile.DataShards),  //nolint:gosec // bounded above
		RSN:       uint8(d.Profile.TotalShards), //nolint:gosec // bounded above
	}, nil
}

func runArkiv(ctx context.Context, d extensionDeps, cfg arkivSettings, pcfg arkiv.PublisherConfig, key *ecdsa.PrivateKey, access string) {
	client := dialArkiv(ctx, cfg, key, access)
	if client == nil {
		return // the context ended while waiting for the node
	}
	defer client.Close()

	pub, err := arkiv.NewPublisher(d.DB, client, d.SigningKey, pcfg, arkivObserver{})
	if err != nil {
		log.Printf("[ARKIV] not started: %v", err)
		return
	}
	go watchArkivBalance(ctx, client)
	log.Printf("[ARKIV] mirror running: endpoint %s, wallet %s, audit period %s, manifests %v",
		cfg.rpcHost(), client.Owner().Hex(), pcfg.Period, pcfg.Manifests)
	pub.Run(ctx)
}

// dialArkiv waits until the node answers and, when asked, is the expected
// chain. Errors never print the RPC URL (it may hold an access key).
func dialArkiv(ctx context.Context, cfg arkivSettings, key *ecdsa.PrivateKey, access string) *arkiv.Client {
	opts := arkiv.Options{Key: key, MinLifetimeBlocks: cfg.LifetimeBlocks}
	for {
		dctx, cancel := context.WithTimeout(ctx, arkivDialTimeout)
		client, err := arkiv.Dial(dctx, cfg.RPCURL, access, opts)
		if err == nil {
			var id *big.Int
			if id, err = client.ChainID(dctx); err == nil && cfg.ChainID != 0 && id.Uint64() != cfg.ChainID {
				err = fmt.Errorf("the node reports chain %s but %s is %d", id, envArkivChainID, cfg.ChainID)
			}
			if err != nil {
				client.Close()
			}
		}
		cancel()
		if err == nil {
			return client
		}
		log.Printf("[ARKIV] cannot reach %s yet (retrying): %v", cfg.rpcHost(), err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(arkivStartRetry):
		}
	}
}

// watchArkivBalance keeps the wallet balance gauge current and warns once
// when the wallet is empty (creates would then fail and be retried).
func watchArkivBalance(ctx context.Context, c *arkiv.Client) {
	warned := false
	for {
		if wei, err := c.Balance(ctx); err == nil {
			glm, _ := new(big.Float).Quo(new(big.Float).SetInt(wei), weiPerGLM).Float64()
			metrics.ArkivWalletBalanceGLM.Set(glm)
			if wei.Sign() == 0 && !warned {
				log.Printf("[ARKIV] WARNING: wallet %s has no tGLM; fund it at hub.arkiv.network", c.Owner().Hex())
				warned = true
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(arkivBalanceEvery):
		}
	}
}

// arkivObserver feeds publisher events into Prometheus.
type arkivObserver struct{}

func (arkivObserver) Reserved(kind string, n int) {
	metrics.ArkivPublicationsTotal.WithLabelValues(kind, "reserved").Add(float64(n))
}

func (arkivObserver) Confirmed(kind string, items, payload int, gas uint64, feeWei *big.Int) {
	metrics.ArkivPublicationsTotal.WithLabelValues(kind, "confirmed").Inc()
	metrics.ArkivItemsPublishedTotal.WithLabelValues(kind).Add(float64(items))
	metrics.ArkivPayloadBytesTotal.Add(float64(payload))
	metrics.ArkivGasUsedTotal.Add(float64(gas))
	if feeWei != nil {
		glm, _ := new(big.Float).Quo(new(big.Float).SetInt(feeWei), weiPerGLM).Float64()
		metrics.ArkivFeeGLMTotal.Add(glm)
	}
}

func (arkivObserver) Failed(kind string) {
	metrics.ArkivPublicationsTotal.WithLabelValues(kind, "failed").Inc()
}

func (arkivObserver) CursorAt(stream string, t time.Time) {
	metrics.ArkivCursorTimestampSeconds.WithLabelValues(stream).Set(float64(t.Unix()))
}

func (arkivObserver) Pending(n int) { metrics.ArkivPendingPublications.Set(float64(n)) }

func (arkivObserver) StepError(step string) {
	metrics.ArkivStepErrorsTotal.WithLabelValues(step).Inc()
}
