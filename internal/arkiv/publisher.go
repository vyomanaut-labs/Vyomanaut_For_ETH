package arkiv

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// Publisher defaults.
const (
	DefaultPeriod       = 2 * time.Minute
	DefaultPollInterval = 15 * time.Second
	DefaultMaxAttempts  = 8

	periodsPerStep   = 10  // catch up at most this many audit periods per step
	filesPerStep     = 200 // manifests reserved per step
	sendsPerStep     = 25  // entities sent per step
	stepTimeout      = 5 * time.Minute
	logPrefix        = "[ARKIV] "
	errStepSend      = "send"
	errStepAudit     = "plan_audit"
	errStepManifests = "plan_manifests"
)

// Creator writes one entity. *Client satisfies it; tests use a fake.
type Creator interface {
	Create(ctx context.Context, attrs []Attribute) (CreateResult, error)
}

// Observer receives publisher events (for metrics). All methods must be cheap
// and must not block.
type Observer interface {
	Reserved(kind string, n int)
	Confirmed(kind string, items, payloadBytes int, gasUsed uint64, feeWei *big.Int)
	Failed(kind string)
	CursorAt(stream string, t time.Time)
	Pending(n int)
	StepError(step string)
}

type nopObserver struct{}

func (nopObserver) Reserved(string, int)                         {}
func (nopObserver) Confirmed(string, int, int, uint64, *big.Int) {}
func (nopObserver) Failed(string)                                {}
func (nopObserver) CursorAt(string, time.Time)                   {}
func (nopObserver) Pending(int)                                  {}
func (nopObserver) StepError(string)                             {}

// PublisherConfig configures a Publisher. Zero values take the defaults.
type PublisherConfig struct {
	// Period is the audit batch period; batches cover [k*Period, (k+1)*Period).
	Period time.Duration
	// SettleLag is how long after a period ends before it is read
	// (receiptleaf.SettleLag when zero).
	SettleLag time.Duration
	// Poll is how often the publisher looks for new work.
	Poll time.Duration
	// Backfill makes a fresh cursor start this far in the past instead of now.
	Backfill time.Duration
	// MaxAttempts is how many times one entity is tried before giving up.
	MaxAttempts int
	// AppTag is the "app" attribute on every entity (DefaultAppTag when empty).
	AppTag string
	// Manifests turns file manifests and tombstones on.
	Manifests bool
	// RSK and RSN are the Reed-Solomon parameters recorded in manifests.
	RSK, RSN uint8
	// Now is the clock (time.Now when nil).
	Now func() time.Time
}

// Publisher mirrors finished audit receipts (and, optionally, file manifests)
// from PostgreSQL to Arkiv. PostgreSQL stays the source of truth: a failure
// here is logged, counted and retried, and never reaches the request path.
type Publisher struct {
	st  store
	c   Creator
	key ed25519.PrivateKey
	cfg PublisherConfig
	obs Observer
}

// NewPublisher builds a Publisher. db is the coordinator's vyomanaut_app pool;
// key is the coordinator's Ed25519 key, used only to sign batch summaries.
func NewPublisher(db *sql.DB, c Creator, key ed25519.PrivateKey, cfg PublisherConfig, obs Observer) (*Publisher, error) {
	if db == nil || c == nil {
		return nil, errors.New("arkiv: publisher needs a database and a creator")
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("arkiv: publisher needs the coordinator's Ed25519 key")
	}
	if cfg.Period <= 0 {
		cfg.Period = DefaultPeriod
	}
	if cfg.Period%time.Second != 0 {
		return nil, fmt.Errorf("arkiv: period %s must be a whole number of seconds", cfg.Period)
	}
	if cfg.SettleLag <= 0 {
		cfg.SettleLag = receiptleaf.SettleLag
	}
	if cfg.Poll <= 0 {
		cfg.Poll = DefaultPollInterval
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}
	if cfg.AppTag == "" {
		cfg.AppTag = DefaultAppTag
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if obs == nil {
		obs = nopObserver{}
	}
	return &Publisher{st: store{db: db}, c: c, key: key, cfg: cfg, obs: obs}, nil
}

// Run publishes until ctx ends.
func (p *Publisher) Run(ctx context.Context) {
	for {
		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		p.Step(stepCtx)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.cfg.Poll):
		}
	}
}

// Step does one round: reserve new work, then send what is unfinished. It is
// exported for tests and for a one-shot run.
func (p *Publisher) Step(ctx context.Context) {
	if err := p.planAudit(ctx); err != nil && ctx.Err() == nil {
		p.obs.StepError(errStepAudit)
		log.Printf(logPrefix+"could not plan audit batches (will retry): %v", err)
	}
	if p.cfg.Manifests {
		if err := p.planManifests(ctx); err != nil && ctx.Err() == nil {
			p.obs.StepError(errStepManifests)
			log.Printf(logPrefix+"could not plan file manifests (will retry): %v", err)
		}
	}
	if err := p.sendUnfinished(ctx); err != nil && ctx.Err() == nil {
		p.obs.StepError(errStepSend)
		log.Printf(logPrefix+"publishing paused (will retry): %v", err)
	}
	if n, err := p.st.pendingCount(ctx); err == nil {
		p.obs.Pending(n)
	}
}

// planAudit reserves the batches of every closed period after the cursor.
func (p *Publisher) planAudit(ctx context.Context) error {
	ready := p.cfg.Now().UTC().Add(-p.cfg.SettleLag)
	start, found, err := p.st.cursor(ctx, StreamAuditReceipts)
	if err != nil {
		return err
	}
	if !found {
		at := PeriodStart(ready.Add(-p.cfg.Backfill), p.cfg.Period)
		if start, err = p.st.initCursor(ctx, StreamAuditReceipts, at); err != nil {
			return err
		}
		log.Printf(logPrefix+"audit cursor starts at %s", start.Format(time.RFC3339))
	}
	for i := 0; i < periodsPerStep; i++ {
		end := start.Add(p.cfg.Period)
		if end.After(ready) {
			break
		}
		rs, err := p.st.receipts(ctx, start, end, nil)
		if err != nil {
			return err
		}
		batches, err := BuildAuditBatches(rs, start, end)
		if err != nil {
			return err
		}
		n, err := p.st.reserveBatches(ctx, batches, end)
		if err != nil {
			return err
		}
		if n > 0 {
			p.obs.Reserved(KindAuditBatch, n)
			log.Printf(logPrefix+"period %s: %d receipt(s) in %d batch(es) queued",
				start.Format(time.RFC3339), len(rs), n)
		}
		start = end
		p.obs.CursorAt(StreamAuditReceipts, start)
	}
	return nil
}

// planManifests reserves manifests for newly registered files and tombstones
// for deleted ones.
func (p *Publisher) planManifests(ctx context.Context) error {
	ready := p.cfg.Now().UTC().Add(-p.cfg.SettleLag)
	from, found, err := p.st.cursor(ctx, StreamFilesRegistered)
	if err != nil {
		return err
	}
	if !found {
		if from, err = p.st.initCursor(ctx, StreamFilesRegistered, ready.Add(-p.cfg.Backfill)); err != nil {
			return err
		}
	}
	ids, last, err := p.st.registeredFiles(ctx, from, ready, filesPerStep)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		n, err := p.st.reserveManifests(ctx, ids, last)
		if err != nil {
			return err
		}
		if n > 0 {
			p.obs.Reserved(KindFileManifest, n)
			log.Printf(logPrefix+"%d file manifest(s) queued", n)
		}
		p.obs.CursorAt(StreamFilesRegistered, last)
	}
	n, err := p.st.reserveTombstones(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		p.obs.Reserved(KindFileTombstone, n)
		log.Printf(logPrefix+"%d file tombstone(s) queued", n)
	}
	return nil
}

// errGiveUp marks an item that can no longer be rebuilt; it is not retried.
var errGiveUp = errors.New("cannot be rebuilt")

// sendUnfinished sends reserved rows, oldest first. The first network failure
// stops the round: if the RPC or the wallet is down, the rest would fail too.
func (p *Publisher) sendUnfinished(ctx context.Context) error {
	rows, err := p.st.unfinished(ctx, p.cfg.MaxAttempts, sendsPerStep)
	if err != nil {
		return err
	}
	for _, row := range rows {
		attrs, items, payload, err := p.build(ctx, row)
		if err != nil {
			giveUp := errors.Is(err, errGiveUp)
			_ = p.st.markFailed(ctx, row.ID, err, nil, giveUp, p.cfg.MaxAttempts)
			p.obs.Failed(row.Kind)
			log.Printf(logPrefix+"%s %s not built: %v", row.Kind, row.Reference, err)
			if giveUp {
				continue
			}
			return err
		}
		if row.Status == StatusFailed {
			if err := p.st.markRetry(ctx, row.ID); err != nil {
				return err
			}
		}
		res, err := p.c.Create(ctx, attrs)
		if err != nil {
			var tx []byte
			if res.TxHash != (common.Hash{}) {
				tx = res.TxHash.Bytes()
			}
			_ = p.st.markFailed(ctx, row.ID, err, tx, false, p.cfg.MaxAttempts)
			p.obs.Failed(row.Kind)
			return fmt.Errorf("%s %s: %w", row.Kind, row.Reference, err)
		}
		if err := p.st.markConfirmed(ctx, row.ID, res, items, payload); err != nil {
			return fmt.Errorf("record confirmed %s %s (entity %s): %w", row.Kind, row.Reference, res.Key.Hex(), err)
		}
		p.obs.Confirmed(row.Kind, items, payload, res.GasUsed, res.FeeWei)
		log.Printf(logPrefix+"%s %s published: entity %s, tx %s, gas %d",
			row.Kind, row.Reference, res.Key.Hex(), res.TxHash.Hex(), res.GasUsed)
	}
	return nil
}

// build turns a reserved row back into entity attributes, from PostgreSQL.
func (p *Publisher) build(ctx context.Context, row publication) (attrs []Attribute, items, payload int, err error) {
	switch row.Kind {
	case KindAuditBatch:
		b, err := p.rebuildBatch(ctx, row)
		if err != nil {
			return nil, 0, 0, err
		}
		attrs, payload, err = AuditBatchAttributes(b, p.key, p.cfg.AppTag)
		return attrs, len(b.Receipts), payload, err
	case KindFileManifest:
		m, err := p.st.manifest(ctx, row.Reference, p.cfg.RSK, p.cfg.RSN)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, 0, fmt.Errorf("%w: file not found", errGiveUp)
		}
		if err != nil {
			return nil, 0, 0, err
		}
		attrs, payload, err = FileManifestAttributes(m, p.key, p.cfg.AppTag)
		return attrs, int(m.Shards), payload, err
	case KindFileTombstone:
		id, err := receiptleaf.ParseUUID(row.Reference)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("%w: %v", errGiveUp, err)
		}
		attrs, payload, err = TombstoneAttributes(id, row.CreatedAt, p.key, p.cfg.AppTag)
		return attrs, 1, payload, err
	default:
		return nil, 0, 0, fmt.Errorf("%w: unknown kind %q", errGiveUp, row.Kind)
	}
}

// rebuildBatch re-reads one batch. The period is closed and settled, so the
// same rows come back in the same order every time.
func (p *Publisher) rebuildBatch(ctx context.Context, row publication) (AuditBatch, error) {
	provider, start, seq, err := ParseBatchReference(row.Reference)
	if err != nil {
		return AuditBatch{}, fmt.Errorf("%w: %v", errGiveUp, err)
	}
	if !row.RangeStart.Valid || !row.RangeEnd.Valid || !row.RangeStart.Time.Equal(start) {
		return AuditBatch{}, fmt.Errorf("%w: the row's range does not match its reference", errGiveUp)
	}
	end := row.RangeEnd.Time.UTC()
	rs, err := p.st.receipts(ctx, start, end, &provider)
	if err != nil {
		return AuditBatch{}, err
	}
	batches, err := BuildAuditBatches(rs, start, end)
	if err != nil {
		return AuditBatch{}, err
	}
	for _, b := range batches {
		if b.Seq == seq {
			return b, nil
		}
	}
	return AuditBatch{}, fmt.Errorf("%w: its receipts are no longer in the database", errGiveUp)
}
