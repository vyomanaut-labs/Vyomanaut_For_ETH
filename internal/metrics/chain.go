package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Chain settlement metrics. They follow the same naming contract as the rest of
// this package (vyomanaut_{subsystem}_{name}_{unit}); the subsystem is chain.
// Amounts are in the token's micro-units.
var (
	// ChainScannedBlock is the highest block whose Deposited logs were read.
	ChainScannedBlock = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "vyomanaut_chain_scanned_block",
		Help: "Highest block whose escrow deposit logs have been read.",
	})

	// ChainDepositsRecordedTotal counts Deposited logs seen for the first time.
	ChainDepositsRecordedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vyomanaut_chain_deposits_recorded_total",
		Help: "Total number of on-chain deposits recorded.",
	})

	// ChainDepositCreditedMicroTotal counts micro-units credited to owner ledgers.
	ChainDepositCreditedMicroTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vyomanaut_chain_deposit_credited_micro_total",
		Help: "Total micro-units of on-chain deposits credited to owners.",
	})

	// ChainPeriodsTotal counts payout periods by the state they reached
	// (reserved, posted, claimed, failed).
	ChainPeriodsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "vyomanaut_chain_periods_total",
		Help: "Total number of payout periods by the state they reached.",
	}, []string{"state"})

	// ChainPaidMicroTotal counts micro-units paid out to providers on chain.
	ChainPaidMicroTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vyomanaut_chain_paid_micro_total",
		Help: "Total micro-units claimed by providers on chain.",
	})

	// ChainUnboundReleases is the number of ledger releases waiting only for the
	// provider to have a payout wallet.
	ChainUnboundReleases = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "vyomanaut_chain_unbound_releases",
		Help: "Ledger releases waiting for a provider payout wallet.",
	})

	// ChainStepErrorsTotal counts failed settlement steps.
	ChainStepErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "vyomanaut_chain_step_errors_total",
		Help: "Total number of failed settlement steps by step name.",
	}, []string{"step"})

	// ChainAnchorsTotal counts audit-history epochs by the state they reached
	// (reserved, anchored, failed).
	ChainAnchorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "vyomanaut_chain_anchors_total",
		Help: "Total number of audit-history anchor epochs by the state they reached.",
	}, []string{"state"})

	// ChainAnchoredReceiptsTotal counts audit receipts covered by an anchored epoch.
	ChainAnchoredReceiptsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vyomanaut_chain_anchored_receipts_total",
		Help: "Total number of audit receipts covered by an anchored epoch.",
	})

	// ChainLastAnchoredEpoch is the newest epoch this coordinator anchored.
	ChainLastAnchoredEpoch = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "vyomanaut_chain_last_anchored_epoch",
		Help: "Newest audit-history epoch anchored on chain.",
	})
)
