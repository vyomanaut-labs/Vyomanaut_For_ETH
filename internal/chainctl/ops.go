package chainctl

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// Ops are the on-chain actions. Build one with NewOps.
type Ops struct {
	RPC    *chain.RPC
	Sender *chain.Sender
	Escrow *chain.EscrowContract
	Token  *chain.ERC20
}

// NewOps binds the escrow and its token for the wallet in key. token may be the
// zero address, in which case the escrow is asked for it.
func NewOps(ctx context.Context, rpc *chain.RPC, key *chain.Key, escrow, token chain.Address) (*Ops, error) {
	sender, err := chain.NewSender(ctx, rpc, key)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the chain: %w", err)
	}
	esc := chain.NewEscrowContract(escrow, rpc, sender)
	if token.IsZero() {
		if token, err = esc.Token(ctx); err != nil {
			return nil, fmt.Errorf("cannot read the escrow's token (is %s an escrow?): %w", escrow, err)
		}
	}
	return &Ops{RPC: rpc, Sender: sender, Escrow: esc, Token: chain.NewERC20(token, rpc, sender)}, nil
}

// ErrNotEnoughUSDT means the wallet holds less than the deposit.
var ErrNotEnoughUSDT = errors.New("chainctl: the wallet holds less USDT than that")

// DepositResult lists the transactions a deposit made.
type DepositResult struct {
	ApproveTx *[32]byte // nil when an earlier approval already covered it
	DepositTx [32]byte
	Block     uint64
}

// Deposit puts amount of USDT from the sender's wallet into the escrow: it
// approves the escrow first if the existing allowance is too small, then
// deposits. It checks the balance first so a short wallet fails before any gas
// is spent.
func (o *Ops) Deposit(ctx context.Context, amount *big.Int) (DepositResult, error) {
	var res DepositResult
	me := o.Sender.Address()
	bal, err := o.Token.BalanceOf(ctx, me)
	if err != nil {
		return res, fmt.Errorf("read the USDT balance: %w", err)
	}
	if bal.Cmp(amount) < 0 {
		return res, fmt.Errorf("%w (holds %s, needs %s)", ErrNotEnoughUSDT, FormatAmount(bal), FormatAmount(amount))
	}
	allowance, err := o.Token.Allowance(ctx, me, o.Escrow.Address())
	if err != nil {
		return res, fmt.Errorf("read the allowance: %w", err)
	}
	if allowance.Cmp(amount) < 0 {
		rc, err := o.Token.Approve(ctx, o.Escrow.Address(), amount)
		if err != nil {
			return res, fmt.Errorf("approve: %s", Explain(err))
		}
		tx := rc.TxHash
		res.ApproveTx = &tx
	}
	rc, err := o.Escrow.Deposit(ctx, amount)
	if err != nil {
		return res, fmt.Errorf("deposit: %s", Explain(err))
	}
	res.DepositTx, res.Block = rc.TxHash, rc.BlockNumber
	return res, nil
}

// Claim sends a posted payout to account. Anyone may do this; the money goes to
// the account, not to the sender.
func (o *Ops) Claim(ctx context.Context, period uint64, account chain.Address) (*chain.Receipt, error) {
	rc, err := o.Escrow.Claim(ctx, period, account)
	if err != nil {
		return nil, errors.New(Explain(err))
	}
	return rc, nil
}

// Refund returns an owner's unspent credit to the owner. It works only after
// the coordinator has posted nothing for the silence timeout.
func (o *Ops) Refund(ctx context.Context, owner chain.Address) (*chain.Receipt, error) {
	rc, err := o.Escrow.Refund(ctx, owner)
	if err != nil {
		return nil, errors.New(Explain(err))
	}
	return rc, nil
}

// PeriodView is one payout period as the chain holds it.
type PeriodView struct {
	Period      uint64
	Total       *big.Int
	Unclaimed   *big.Int
	ClaimableAt time.Time
	Vetoed      bool
	Allocation  *big.Int // for the account asked about, if any
}

// Period reads a payout period, and what is owed to account in it when account
// is not the zero address.
func (o *Ops) Period(ctx context.Context, period uint64, account chain.Address) (PeriodView, error) {
	info, err := o.Escrow.PeriodInfo(ctx, period)
	if err != nil {
		return PeriodView{}, err
	}
	v := PeriodView{Period: period, Total: info.Total, Unclaimed: info.Unclaimed, Vetoed: info.Vetoed,
		ClaimableAt: time.Unix(int64(info.ClaimableAt), 0).UTC()} //nolint:gosec // a block timestamp
	if !account.IsZero() {
		if v.Allocation, err = o.Escrow.AllocationOf(ctx, period, account); err != nil {
			return v, err
		}
	}
	return v, nil
}

// Explain turns a chain error into a sentence a person can act on.
func Explain(err error) string {
	var re *chain.RevertError
	if !errors.As(err, &re) {
		return err.Error()
	}
	name, args, _ := strings.Cut(re.Reason, "(")
	word := func(i int) (*big.Int, bool) {
		_, hexArgs, ok := strings.Cut(re.Reason, "args=0x")
		if !ok || len(hexArgs) < (i+1)*64 { //nolint:mnd // a word is 32 bytes, 64 hex digits
			return nil, false
		}
		b, derr := hex.DecodeString(hexArgs[i*64 : (i+1)*64]) //nolint:mnd // see above
		if derr != nil {
			return nil, false
		}
		return new(big.Int).SetBytes(b), true
	}
	_ = args
	at := func(n *big.Int) string {
		return time.Unix(n.Int64(), 0).UTC().Format("15:04:05 UTC")
	}
	switch name {
	case "WindowStillOpen":
		if t, ok := word(1); ok {
			return fmt.Sprintf("too early: the challenge window is still open and ends at %s; try again then", at(t))
		}
		return "too early: the challenge window is still open"
	case "CoordinatorStillActive":
		if t, ok := word(0); ok {
			return fmt.Sprintf("too early: the coordinator posted recently, so refunds open at %s", at(t))
		}
		return "too early: the coordinator posted recently"
	case "NothingToClaim":
		return "nothing to claim: that account has nothing in this period, or it was already paid"
	case "NothingToRefund":
		return "nothing to refund: that owner has no unspent credit in the escrow"
	case "UnknownPeriod":
		return "that period was never posted"
	case "PeriodIsVetoed":
		return "that period was cancelled by the guardian; its money went back to the owners' credit"
	case "ZeroAmount":
		return "the amount must be greater than zero"
	}
	return "the contract refused: " + re.Reason
}
