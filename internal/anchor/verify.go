package anchor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/receiptleaf"
)

// Reader is what a Verifier needs from the chain. It must talk to the chain
// directly: a verifier that asked the coordinator for the root would prove
// nothing. *chain.AnchorContract satisfies it.
type Reader interface {
	Roots(ctx context.Context, epoch uint64) (chain.AnchorRecord, error)
}

// Head reports how final an anchoring transaction is. *chain.RPC satisfies it.
// It is optional: without it the finality state is "unknown".
type Head interface {
	Receipt(ctx context.Context, hash [32]byte) (*chain.Receipt, error)
	FinalizedBlock(ctx context.Context) (uint64, error)
}

// Finality of an anchoring transaction.
const (
	FinalityFinalized = "finalized" // included in a block Ethereum has finalized
	FinalitySoft      = "soft"      // confirmed by the sequencer only
	FinalityUnknown   = "unknown"
)

// Verifier rebuilds an epoch's root from the database and compares it with the
// chain's copy. It only reads.
type Verifier struct {
	store *store
	chain Reader
	head  Head
}

// NewVerifier builds a Verifier. head may be nil.
func NewVerifier(db *sql.DB, reader Reader, head Head) *Verifier {
	return &Verifier{store: &store{db: db}, chain: reader, head: head}
}

// EpochSummary is one row of the local record of anchored epochs.
type EpochSummary struct {
	Epoch    uint64
	State    string
	Leaves   int
	First    time.Time
	Last     time.Time
	AnchorTx []byte
	Error    string
}

// List returns up to limit epochs, newest first.
func (v *Verifier) List(ctx context.Context, limit int) ([]EpochSummary, error) {
	rows, err := v.store.list(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]EpochSummary, len(rows))
	for i, r := range rows {
		out[i] = EpochSummary{Epoch: r.Epoch, State: r.State, Leaves: r.Leaves, First: r.First,
			Last: r.Last.TS, AnchorTx: r.AnchorTx, Error: r.LastError}
	}
	return out, nil
}

// Latest returns the newest anchored epoch number.
func (v *Verifier) Latest(ctx context.Context) (uint64, error) {
	r, ok, err := v.store.latestAnchored(ctx)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrNothingAnchored
	}
	return r.Epoch, nil
}

// Sentinel errors.
var (
	// ErrNothingAnchored means the database records no anchored epoch.
	ErrNothingAnchored = errors.New("anchor: no epoch has been anchored yet")
	// ErrUnknownEpoch means the database has no such epoch.
	ErrUnknownEpoch = errors.New("anchor: the database has no such epoch")
	// ErrReceiptNotFound means the receipt is in no anchored epoch.
	ErrReceiptNotFound = errors.New("anchor: the receipt is not in any anchored epoch")
)

// Verdict says how an epoch compares with the chain.
type Verdict string

// Verdicts.
const (
	// VerdictMatch means the database rebuilds exactly the root on chain.
	VerdictMatch Verdict = "MATCH"
	// VerdictMismatch means the database no longer reproduces the chain's root.
	VerdictMismatch Verdict = "MISMATCH"
	// VerdictNotOnChain means the chain has no such epoch.
	VerdictNotOnChain Verdict = "NOT_ON_CHAIN"
)

// EpochReport is the result of checking one epoch.
type EpochReport struct {
	Epoch          uint64
	State          string // the local row's state
	Verdict        Verdict
	Detail         string
	DBRoot         [32]byte
	DBLeaves       int
	ChainRoot      [32]byte
	ChainLeaves    uint32
	ChainTimestamp time.Time
	AnchorTx       []byte
	AnchorBlock    uint64
	Finality       string
}

// VerifyEpoch rebuilds epoch's root from the database and compares it with the
// chain.
func (v *Verifier) VerifyEpoch(ctx context.Context, epoch uint64) (EpochReport, error) {
	row, receipts, leaves, err := v.rebuild(ctx, epoch)
	if err != nil {
		return EpochReport{}, err
	}
	rep := EpochReport{Epoch: epoch, State: row.State, DBLeaves: len(receipts), AnchorTx: row.AnchorTx}
	rep.DBRoot = receiptleaf.MerkleRoot(leaves)
	if len(receipts) == 0 {
		rep.DBRoot = [32]byte{}
	}

	rec, err := v.chain.Roots(ctx, epoch)
	if err != nil {
		return rep, fmt.Errorf("read epoch %d from the chain: %w", epoch, err)
	}
	if rec.Timestamp == 0 {
		rep.Verdict = VerdictNotOnChain
		rep.Detail = "the chain has no record of this epoch"
		return rep, nil
	}
	rep.ChainRoot, rep.ChainLeaves = rec.Root, rec.LeafCount
	rep.ChainTimestamp = time.Unix(int64(rec.Timestamp), 0).UTC() //nolint:gosec // a block timestamp
	v.addFinality(ctx, &rep)

	switch {
	case rep.DBRoot == rec.Root && uint32(len(receipts)) == rec.LeafCount: //nolint:gosec // bounded by the table
		rep.Verdict = VerdictMatch
		rep.Detail = fmt.Sprintf("the database rebuilds the on-chain root from %d receipts", len(receipts))
	case uint32(len(receipts)) != rec.LeafCount: //nolint:gosec // bounded by the table
		rep.Verdict = VerdictMismatch
		rep.Detail = fmt.Sprintf("the chain fixed %d receipts for this epoch but the database now holds %d in the same range: receipts were added, removed or moved",
			rec.LeafCount, len(receipts))
	default:
		rep.Verdict = VerdictMismatch
		rep.Detail = "the receipt count is unchanged but the rebuilt root differs: at least one receipt was altered after it was anchored"
	}
	return rep, nil
}

// ReceiptReport is the result of checking one receipt against the chain.
type ReceiptReport struct {
	ReceiptID   string
	Epoch       uint64
	Index       int // the receipt's position among the epoch's receipts
	Size        int // how many receipts the database now holds in the epoch
	LeafHash    [32]byte
	ProofLength int
	// Included is true when the inclusion proof built from the database
	// reproduces the root the chain holds.
	Included bool
	Report   EpochReport
}

// VerifyReceipt finds the epoch holding receiptID, builds its inclusion proof
// from the database, and checks the proof against the root read from the chain.
func (v *Verifier) VerifyReceipt(ctx context.Context, receiptID string) (ReceiptReport, error) {
	want, err := receiptleaf.ParseUUID(receiptID)
	if err != nil {
		return ReceiptReport{}, fmt.Errorf("receipt id: %w", err)
	}
	pos, found, err := v.store.receiptPosition(ctx, receiptleaf.FormatUUID(want))
	if err != nil {
		return ReceiptReport{}, err
	}
	if !found {
		return ReceiptReport{}, ErrReceiptNotFound
	}
	row, found, err := v.store.epochContaining(ctx, pos)
	if err != nil {
		return ReceiptReport{}, err
	}
	if !found {
		return ReceiptReport{}, ErrReceiptNotFound
	}

	_, receipts, leaves, err := v.rebuild(ctx, row.Epoch)
	if err != nil {
		return ReceiptReport{}, err
	}
	rep := ReceiptReport{ReceiptID: receiptID, Epoch: row.Epoch, Size: len(receipts), Index: -1}
	for i, r := range receipts {
		if r.ReceiptID == want {
			rep.Index = i
			break
		}
	}
	if rep.Index < 0 {
		return rep, ErrReceiptNotFound
	}
	rep.LeafHash = leaves[rep.Index]
	if rep.Report, err = v.VerifyEpoch(ctx, row.Epoch); err != nil {
		return rep, err
	}
	proof, err := receiptleaf.InclusionProof(leaves, rep.Index)
	if err != nil {
		return rep, err
	}
	rep.ProofLength = len(proof)
	// The check that matters: the root comes from the chain, not the database.
	rep.Included = rep.Report.Verdict != VerdictNotOnChain &&
		receiptleaf.VerifyInclusion(leaves[rep.Index], rep.Index, len(receipts), proof, rep.Report.ChainRoot)
	return rep, nil
}

// rebuild reads the receipts an epoch covers and hashes them.
func (v *Verifier) rebuild(ctx context.Context, epoch uint64) (epochRow, []receiptleaf.Receipt, [][32]byte, error) {
	row, ok, err := v.store.byEpoch(ctx, epoch)
	if err != nil {
		return epochRow{}, nil, nil, err
	}
	if !ok || row.State != stateAnchored {
		return epochRow{}, nil, nil, fmt.Errorf("%w: %d", ErrUnknownEpoch, epoch)
	}
	var after *position
	if p, found, err := v.store.previousEnd(ctx, epoch); err != nil {
		return epochRow{}, nil, nil, err
	} else if found {
		after = &p
	}
	upTo := row.Last
	receipts, err := v.store.receipts(ctx, after, &upTo, time.Time{}, 0)
	if err != nil {
		return epochRow{}, nil, nil, err
	}
	leaves, err := receiptleaf.LeafHashes(receipts)
	if err != nil {
		return epochRow{}, nil, nil, err
	}
	return row, receipts, leaves, nil
}

func (v *Verifier) addFinality(ctx context.Context, rep *EpochReport) {
	rep.Finality = FinalityUnknown
	if v.head == nil || len(rep.AnchorTx) != txHashLen {
		return
	}
	var h [32]byte
	copy(h[:], rep.AnchorTx)
	rc, err := v.head.Receipt(ctx, h)
	if err != nil || rc == nil {
		return
	}
	rep.AnchorBlock = rc.BlockNumber
	fin, err := v.head.FinalizedBlock(ctx)
	if err != nil {
		return
	}
	if rc.BlockNumber <= fin {
		rep.Finality = FinalityFinalized
	} else {
		rep.Finality = FinalitySoft
	}
}
