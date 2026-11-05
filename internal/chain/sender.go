package chain

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

const (
	gasMarginPercent    = 30
	gasMarginFlat       = 25_000
	percentBase         = 100
	defaultReceiptWait  = 90 * time.Second
	defaultPollInterval = time.Second
)

// Sender sends transactions from one key, one at a time in nonce order.
//
// Every send is simulated first (a reverting call never reaches the chain),
// the gas estimate is padded (an estimate taken against one state can fall
// short when the transaction executes against the next), and the nonce is
// tracked locally so a load-balanced endpoint that lags behind cannot make it
// repeat a nonce.
type Sender struct {
	rpc     *RPC
	key     *Key
	chainID uint64

	// ReceiptTimeout bounds how long Send waits for a transaction to be mined.
	ReceiptTimeout time.Duration
	// PollInterval is how often Send asks for the receipt.
	PollInterval time.Duration

	mu        sync.Mutex
	haveNonce bool
	nextNonce uint64
}

// NewSender reads the chain id from the node and returns a Sender for key.
func NewSender(ctx context.Context, rpc *RPC, key *Key) (*Sender, error) {
	id, err := rpc.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	return &Sender{
		rpc: rpc, key: key, chainID: id,
		ReceiptTimeout: defaultReceiptWait, PollInterval: defaultPollInterval,
	}, nil
}

// Address is the account transactions are sent from.
func (s *Sender) Address() Address { return s.key.Address() }

// ChainID is the chain this sender signs for.
func (s *Sender) ChainID() uint64 { return s.chainID }

func paddedGas(estimate uint64) uint64 {
	return estimate + estimate*gasMarginPercent/percentBase + gasMarginFlat
}

func isNonceProblem(err error) bool {
	var re *RPCError
	if !errors.As(err, &re) {
		return false
	}
	m := strings.ToLower(re.Message)
	return strings.Contains(m, "nonce too low") || strings.Contains(m, "already known") ||
		strings.Contains(m, "replacement transaction underpriced")
}

// Send simulates, signs and submits a transaction to `to` (nil creates a
// contract), then waits for it to be mined. It returns ErrReverted if the
// simulation reverts, ErrTxFailed if the mined transaction has status 0, and
// ErrReceiptTimeout if it is not mined in time.
func (s *Sender) Send(ctx context.Context, to *Address, data []byte) (*Receipt, error) {
	hash, err := s.submit(ctx, to, data)
	if err != nil {
		return nil, err
	}
	return s.wait(ctx, hash)
}

func (s *Sender) submit(ctx context.Context, to *Address, data []byte) ([hashLen]byte, error) {
	var zero [hashLen]byte
	s.mu.Lock()
	defer s.mu.Unlock()

	gas, err := s.rpc.EstimateGas(ctx, s.key.Address(), to, data)
	if err != nil {
		return zero, err
	}
	tip, feeCap, err := s.rpc.FeeCaps(ctx)
	if err != nil {
		return zero, err
	}

	const attempts = 2
	for attempt := 0; attempt < attempts; attempt++ {
		nonce, nerr := s.pickNonce(ctx)
		if nerr != nil {
			return zero, nerr
		}
		tx := &Tx{
			ChainID: s.chainID, Nonce: nonce, GasTipCap: tip, GasFeeCap: feeCap,
			Gas: paddedGas(gas), To: to, Value: new(big.Int), Data: data,
		}
		raw, _, serr := tx.Sign(s.key)
		if serr != nil {
			return zero, serr
		}
		h, serr := s.rpc.SendRaw(ctx, raw)
		if serr == nil {
			s.nextNonce = nonce + 1
			s.haveNonce = true
			return h, nil
		}
		if isNonceProblem(serr) && attempt+1 < attempts {
			s.haveNonce = false // re-read from the node and try once more
			continue
		}
		return zero, serr
	}
	return zero, ErrRPC // unreachable: the loop always returns
}

// pickNonce returns the larger of the node's pending nonce and our own count.
func (s *Sender) pickNonce(ctx context.Context) (uint64, error) {
	n, err := s.rpc.PendingNonce(ctx, s.key.Address())
	if err != nil {
		return 0, err
	}
	if s.haveNonce && s.nextNonce > n {
		n = s.nextNonce
	}
	return n, nil
}

func (s *Sender) wait(ctx context.Context, hash [hashLen]byte) (*Receipt, error) {
	deadline := time.NewTimer(s.ReceiptTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(s.PollInterval)
	defer tick.Stop()

	label := "0x" + hex.EncodeToString(hash[:])
	for {
		rc, err := s.rpc.Receipt(ctx, hash)
		if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil && rc != nil {
			if rc.Status != 1 {
				return rc, fmt.Errorf("%w (tx %s)", ErrTxFailed, label)
			}
			return rc, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w (tx %s): %v", ErrReceiptTimeout, label, ctx.Err())
		case <-deadline.C:
			return nil, fmt.Errorf("%w (tx %s)", ErrReceiptTimeout, label)
		case <-tick.C:
		}
	}
}
