package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Arkiv mirror metrics. Same naming contract as the rest of this package
// (vyomanaut_{subsystem}_{name}_{unit}); the subsystem is arkiv. This file must
// not import go-ethereum: internal/metrics is linked into every binary,
// including the provider daemon.
var (
	// ArkivPublicationsTotal counts entities by kind (audit_batch,
	// file_manifest, file_tombstone) and the state they reached (reserved,
	// confirmed, failed).
	ArkivPublicationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "vyomanaut_arkiv_publications_total",
		Help: "Total number of Arkiv entities by kind and the state they reached.",
	}, []string{"kind", "state"})

	// ArkivItemsPublishedTotal counts what confirmed entities cover: audit
	// receipts for batches, shards for manifests, one per tombstone.
	ArkivItemsPublishedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "vyomanaut_arkiv_items_published_total",
		Help: "Total number of items (receipts, shards, tombstones) covered by confirmed Arkiv entities.",
	}, []string{"kind"})

	// ArkivPayloadBytesTotal counts body bytes of confirmed entities.
	ArkivPayloadBytesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vyomanaut_arkiv_payload_bytes_total",
		Help: "Total body bytes written to Arkiv.",
	})

	// ArkivGasUsedTotal counts gas spent on confirmed creates.
	ArkivGasUsedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vyomanaut_arkiv_gas_used_total",
		Help: "Total gas used by confirmed Arkiv creates.",
	})

	// ArkivFeeGLMTotal counts fees paid, in tGLM (wei / 1e18).
	ArkivFeeGLMTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vyomanaut_arkiv_fee_glm_total",
		Help: "Total fees paid for Arkiv creates, in tGLM.",
	})

	// ArkivCursorTimestampSeconds is how far each stream has been mirrored.
	ArkivCursorTimestampSeconds = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vyomanaut_arkiv_cursor_timestamp_seconds",
		Help: "Unix time up to which each stream has been queued for Arkiv.",
	}, []string{"stream"})

	// ArkivPendingPublications is the number of rows not yet confirmed.
	ArkivPendingPublications = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "vyomanaut_arkiv_pending_publications",
		Help: "Arkiv publications queued or failed and not yet confirmed.",
	})

	// ArkivWalletBalanceGLM is the publisher wallet's balance in tGLM.
	ArkivWalletBalanceGLM = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "vyomanaut_arkiv_wallet_balance_glm",
		Help: "Balance of the Arkiv publisher wallet, in tGLM.",
	})

	// ArkivStepErrorsTotal counts failed publisher steps.
	ArkivStepErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "vyomanaut_arkiv_step_errors_total",
		Help: "Total number of failed Arkiv publisher steps by step name.",
	}, []string{"step"})
)
