// cmd/microservice — see main.go's package doc comment.
//
// chain_ext.go is the coordinator's side of on-chain settlement. It is wiring
// only: the logic lives in internal/chainsettle and internal/chain. It is off
// unless VYOMANAUT_CHAIN_ENABLED=1, and it never blocks or fails the
// coordinator: if the chain is unreachable it keeps retrying in the background
// while uploads, audits, repair and the paise ledger carry on unchanged.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chainsettle"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/metrics"
)

const (
	chainRPCTimeout = 20 * time.Second
	chainStartRetry = 15 * time.Second
)

func init() {
	registerExtension(extension{name: "chain", start: startChainSettlement})
}

// startChainSettlement validates the settings and the key file immediately, so
// a typo fails loudly at boot, then settles in a background goroutine.
func startChainSettlement(ctx context.Context, d extensionDeps) error {
	cfg, err := chainSettingsFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	key, err := chain.LoadKeyFile(cfg.KeyFile)
	if err != nil {
		return err
	}
	go runChainSettlement(ctx, d, cfg, key)
	return nil
}

func runChainSettlement(ctx context.Context, d extensionDeps, cfg chainSettings, key *chain.Key) {
	rpc := chain.NewRPC(cfg.RPCURL, chainRPCTimeout)
	for {
		svc, err := buildChainService(ctx, d, cfg, key, rpc)
		if err == nil {
			log.Printf("[CHAIN] settlement running: endpoint %s, escrow %s, coordinator wallet %s",
				cfg.rpcHost(), cfg.Escrow, key.Address())
			svc.Run(ctx)
			return
		}
		log.Printf("[CHAIN] cannot start yet (retrying): %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(chainStartRetry):
		}
	}
}

func buildChainService(ctx context.Context, d extensionDeps, cfg chainSettings, key *chain.Key, rpc *chain.RPC) (*chainsettle.Service, error) {
	sender, err := chain.NewSender(ctx, rpc, key)
	if err != nil {
		return nil, err
	}
	if cfg.ChainID != 0 && sender.ChainID() != cfg.ChainID {
		return nil, fmt.Errorf("the node reports chain %d but %s is %d", sender.ChainID(), envChainChainID, cfg.ChainID)
	}
	escrow := chain.NewEscrowContract(cfg.Escrow, rpc, sender)
	if coord, err := escrow.Coordinator(ctx); err != nil {
		return nil, fmt.Errorf("read the escrow's coordinator: %w", err)
	} else if coord != key.Address() {
		log.Printf("[CHAIN] WARNING: this key (%s) is not the escrow's coordinator (%s); posting periods will be rejected",
			key.Address(), coord)
	}
	return chainsettle.New(d.DB, escrow, rpc, chainsettle.Config{
		MicroPerPaise: cfg.MicroPerPaise, Confirmations: cfg.Confirmations, StartBlock: cfg.StartBlock,
		PollInterval: cfg.Poll, SettleLag: cfg.SettleLag,
	}, chainsettle.WithObserver(chainObserver{}))
}

// chainObserver feeds settlement events into Prometheus.
type chainObserver struct{}

func (chainObserver) ScannedTo(block uint64) { metrics.ChainScannedBlock.Set(float64(block)) }
func (chainObserver) DepositsRecorded(n int) { metrics.ChainDepositsRecordedTotal.Add(float64(n)) }
func (chainObserver) DepositCredited(micro int64) {
	metrics.ChainDepositCreditedMicroTotal.Add(float64(micro))
}
func (chainObserver) PeriodReserved(int64, int) {
	metrics.ChainPeriodsTotal.WithLabelValues("reserved").Inc()
}
func (chainObserver) PeriodPosted(int64) { metrics.ChainPeriodsTotal.WithLabelValues("posted").Inc() }
func (chainObserver) PeriodClaimed(micro int64) {
	metrics.ChainPeriodsTotal.WithLabelValues("claimed").Inc()
	metrics.ChainPaidMicroTotal.Add(float64(micro))
}
func (chainObserver) PeriodFailed(string)     { metrics.ChainPeriodsTotal.WithLabelValues("failed").Inc() }
func (chainObserver) UnboundReleases(n int64) { metrics.ChainUnboundReleases.Set(float64(n)) }
func (chainObserver) StepError(step string)   { metrics.ChainStepErrorsTotal.WithLabelValues(step).Inc() }
