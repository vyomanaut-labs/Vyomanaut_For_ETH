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
	"crypto/ed25519"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/anchor"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/api"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chainapi"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chainbind"
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
	if cfg.AutoPayoutWallets {
		go runWalletAssigner(ctx, d)
	}
	go runChainSettlement(ctx, d, cfg, key)
	return nil
}

func runChainSettlement(ctx context.Context, d extensionDeps, cfg chainSettings, key *chain.Key) {
	rpc := chain.NewRPC(cfg.RPCURL, chainRPCTimeout)
	// One Sender serves settlement and anchoring: both sign with the
	// coordinator wallet, and the Sender keeps the nonce count.
	sender := connectSender(ctx, rpc, key, cfg)
	if sender == nil {
		return // the context ended while waiting for the node
	}
	if cfg.anchoring() {
		go runChainAnchoring(ctx, d, cfg, rpc, sender)
	}
	if cfg.APIAddr != "" {
		go serveChainAPI(ctx, d, cfg, rpc)
	}
	for {
		svc, err := buildChainService(ctx, d, cfg, sender, rpc)
		if err == nil {
			log.Printf("[CHAIN] settlement running: endpoint %s, escrow %s, coordinator wallet %s",
				cfg.rpcHost(), cfg.Escrow, key.Address())
			svc.Run(ctx)
			return
		}
		log.Printf("[CHAIN] cannot start yet (retrying): %v", err)
		if !sleepCtx(ctx, chainStartRetry) {
			return
		}
	}
}

// connectSender waits until the node answers and, when asked, is the chain the
// operator expected.
func connectSender(ctx context.Context, rpc *chain.RPC, key *chain.Key, cfg chainSettings) *chain.Sender {
	for {
		sender, err := chain.NewSender(ctx, rpc, key)
		if err == nil && cfg.ChainID != 0 && sender.ChainID() != cfg.ChainID {
			err = fmt.Errorf("the node reports chain %d but %s is %d", sender.ChainID(), envChainChainID, cfg.ChainID)
		}
		if err == nil {
			return sender
		}
		log.Printf("[CHAIN] cannot start yet (retrying): %v", err)
		if !sleepCtx(ctx, chainStartRetry) {
			return nil
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func buildChainService(ctx context.Context, d extensionDeps, cfg chainSettings, sender *chain.Sender, rpc *chain.RPC) (*chainsettle.Service, error) {
	escrow := chain.NewEscrowContract(cfg.Escrow, rpc, sender)
	if coord, err := escrow.Coordinator(ctx); err != nil {
		return nil, fmt.Errorf("read the escrow's coordinator: %w", err)
	} else if coord != sender.Address() {
		log.Printf("[CHAIN] WARNING: this key (%s) is not the escrow's coordinator (%s); posting periods will be rejected",
			sender.Address(), coord)
	}
	return chainsettle.New(d.DB, escrow, rpc, chainsettle.Config{
		MicroPerPaise: cfg.MicroPerPaise, Confirmations: cfg.Confirmations, StartBlock: cfg.StartBlock,
		PollInterval: cfg.Poll, SettleLag: cfg.SettleLag,
	}, chainsettle.WithObserver(chainObserver{}))
}

// runChainAnchoring publishes a fingerprint of the audit history. It is
// independent of settlement: either can be down while the other works.
func runChainAnchoring(ctx context.Context, d extensionDeps, cfg chainSettings, rpc *chain.RPC, sender *chain.Sender) {
	contract := chain.NewAnchorContract(cfg.Anchor, rpc, sender)
	for {
		coord, err := anchorCoordinator(ctx, rpc, cfg.Anchor)
		if err == nil {
			if coord != sender.Address() {
				log.Printf("[ANCHOR] WARNING: this key (%s) is not the anchor's coordinator (%s); anchors will be rejected",
					sender.Address(), coord)
			}
			break
		}
		log.Printf("[ANCHOR] cannot start yet (retrying): %v", err)
		if !sleepCtx(ctx, chainStartRetry) {
			return
		}
	}
	svc, err := anchor.New(d.DB, contract, anchor.Config{
		Interval: cfg.AnchorInterval, MaxLeaves: cfg.AnchorMaxLeaves,
	}, anchor.WithObserver(anchorObserver{}))
	if err != nil {
		log.Printf("[ANCHOR] not started: %v", err)
		return
	}
	log.Printf("[ANCHOR] anchoring running: anchor contract %s, every %s", cfg.Anchor, anchorEvery(cfg))
	svc.Run(ctx)
}

func anchorEvery(cfg chainSettings) time.Duration {
	if cfg.AnchorInterval == 0 {
		return anchor.DefaultInterval
	}
	return cfg.AnchorInterval
}

// anchorCoordinator reads the coordinator address of an AuditAnchor.
func anchorCoordinator(ctx context.Context, rpc *chain.RPC, addr chain.Address) (chain.Address, error) {
	return chain.NewAnchorContract(addr, rpc, nil).Coordinator(ctx)
}

const (
	walletAssignEvery = 10 * time.Second
	apiReadTimeout    = 10 * time.Second
	apiShutdownWait   = 3 * time.Second
)

// runWalletAssigner gives providers a derived demo payout wallet as they
// appear, so no provider (and no operator) has to register an address. The
// wallets are derived from the coordinator's own secret: custodial, and said so.
func runWalletAssigner(ctx context.Context, d extensionDeps) {
	binder := chainbind.NewBinder(d.DB)
	secret := d.SigningKey.Seed()
	derive := func(p uuid.UUID) (chain.Address, error) { return chainbind.DeriveProviderWallet(secret, p) }
	log.Printf("[CHAIN] demo payout wallets: providers get an address derived from the coordinator's key (custodial)")
	for {
		n, err := binder.AssignProviderWallets(ctx, derive)
		switch {
		case err != nil && ctx.Err() == nil:
			log.Printf("[CHAIN] could not assign payout wallets (will retry): %v", err)
		case n > 0:
			log.Printf("[CHAIN] assigned demo payout wallets to %d provider(s)", n)
		}
		if !sleepCtx(ctx, walletAssignEvery) {
			return
		}
	}
}

// serveChainAPI serves the chain API: which contracts are in use, and binding
// an owner's wallet. It needs the escrow's token address, read from the chain.
func serveChainAPI(ctx context.Context, d extensionDeps, cfg chainSettings, rpc *chain.RPC) {
	escrow := chain.NewEscrowContract(cfg.Escrow, rpc, nil)
	var token chain.Address
	for {
		var err error
		if token, err = escrow.Token(ctx); err == nil {
			break
		}
		log.Printf("[CHAIN] chain API waiting for the escrow's token address (retrying): %v", err)
		if !sleepCtx(ctx, chainStartRetry) {
			return
		}
	}
	chainID, err := rpc.ChainID(ctx)
	if err != nil {
		chainID = cfg.ChainID
	}

	pub, ok := d.SigningKey.Public().(ed25519.PublicKey)
	if !ok {
		log.Printf("[CHAIN] chain API not started: the coordinator's key is not an Ed25519 key")
		return
	}
	verify := func(tok string) (uuid.UUID, string, error) {
		c, err := api.VerifyJWT(pub, tok)
		return c.Subject, c.Role, err
	}
	srv := &http.Server{
		Addr: cfg.APIAddr,
		Handler: chainapi.New(chainapi.Deps{
			Wallets: chainbind.NewBinder(d.DB),
			Verify:  verify,
			Info:    chainapi.Info{ChainID: chainID, Escrow: cfg.Escrow, Token: token, Anchor: cfg.Anchor, Explorer: cfg.Explorer},
		}),
		ReadHeaderTimeout: apiReadTimeout,
		ReadTimeout:       apiReadTimeout,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), apiShutdownWait)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("[CHAIN] chain API listening on %s (wallet binding; plain HTTP, for the demo network only)", cfg.APIAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("[CHAIN] chain API stopped: %v", err)
	}
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

// anchorObserver feeds anchoring events into Prometheus.
type anchorObserver struct{}

func (anchorObserver) EpochReserved(int) { metrics.ChainAnchorsTotal.WithLabelValues("reserved").Inc() }
func (anchorObserver) EpochAnchored(epoch uint64, leaves int) {
	metrics.ChainAnchorsTotal.WithLabelValues("anchored").Inc()
	metrics.ChainAnchoredReceiptsTotal.Add(float64(leaves))
	metrics.ChainLastAnchoredEpoch.Set(float64(epoch))
}
func (anchorObserver) EpochFailed(string) { metrics.ChainAnchorsTotal.WithLabelValues("failed").Inc() }
func (anchorObserver) StepError(step string) {
	metrics.ChainStepErrorsTotal.WithLabelValues("anchor-" + step).Inc()
}
