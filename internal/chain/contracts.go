package chain

import (
	"context"
	"fmt"
	"math/big"
)

// ── ERC-20 ───────────────────────────────────────────────────────────────

// ERC20 is a token such as the demo stablecoin.
type ERC20 struct {
	addr Address
	rpc  *RPC
	s    *Sender // may be nil for read-only use
}

// NewERC20 binds a token contract. s may be nil if only views are needed.
func NewERC20(addr Address, rpc *RPC, s *Sender) *ERC20 { return &ERC20{addr: addr, rpc: rpc, s: s} }

// Address is the token contract address.
func (t *ERC20) Address() Address { return t.addr }

// BalanceOf returns an account's token balance in base units.
func (t *ERC20) BalanceOf(ctx context.Context, who Address) (*big.Int, error) {
	return viewUint(ctx, t.rpc, t.addr, "balanceOf(address)", addrArg(who))
}

// Allowance returns how much spender may pull from owner.
func (t *ERC20) Allowance(ctx context.Context, owner, spender Address) (*big.Int, error) {
	return viewUint(ctx, t.rpc, t.addr, "allowance(address,address)", addrArg(owner), addrArg(spender))
}

// Approve lets spender pull up to amount from the sender's account.
func (t *ERC20) Approve(ctx context.Context, spender Address, amount *big.Int) (*Receipt, error) {
	return send(ctx, t.s, t.addr, "approve(address,uint256)", addrArg(spender), uintArg(amount))
}

// Transfer sends amount from the sender's account to `to`.
func (t *ERC20) Transfer(ctx context.Context, to Address, amount *big.Int) (*Receipt, error) {
	return send(ctx, t.s, t.addr, "transfer(address,uint256)", addrArg(to), uintArg(amount))
}

// ── AuditAnchor ──────────────────────────────────────────────────────────

// AnchorContract is the AuditAnchor contract.
type AnchorContract struct {
	addr Address
	rpc  *RPC
	s    *Sender
}

// NewAnchorContract binds an AuditAnchor. s may be nil for read-only use.
func NewAnchorContract(addr Address, rpc *RPC, s *Sender) *AnchorContract {
	return &AnchorContract{addr: addr, rpc: rpc, s: s}
}

// Address is the contract address.
func (a *AnchorContract) Address() Address { return a.addr }

// AnchorRecord is what the contract stores for one epoch.
type AnchorRecord struct {
	Root      [hashLen]byte
	LeafCount uint32
	Timestamp uint64
}

const (
	bits32 = 32
	bits64 = 64

	// Positions of the return words of roots(uint64) and periodInfo(uint64).
	wordRoot, wordLeafCount, wordTimestamp = 0, 1, 2
	wordTotal, wordUnclaimed               = 0, 1
	wordClaimableAt, wordVetoed            = 2, 3
)

// LastEpoch returns the highest epoch anchored so far.
func (a *AnchorContract) LastEpoch(ctx context.Context) (uint64, error) {
	out, err := callView(ctx, a.rpc, a.addr, "lastEpoch()")
	if err != nil {
		return 0, err
	}
	return abiReader{out}.uintBits(0, bits64)
}

// Roots reads the record for epoch. An epoch never anchored has Timestamp 0.
func (a *AnchorContract) Roots(ctx context.Context, epoch uint64) (AnchorRecord, error) {
	var rec AnchorRecord
	out, err := callView(ctx, a.rpc, a.addr, "roots(uint64)", uint64Arg(epoch))
	if err != nil {
		return rec, err
	}
	r := abiReader{out}
	if rec.Root, err = r.bytes32(wordRoot); err != nil {
		return rec, err
	}
	count, err := r.uintBits(wordLeafCount, bits32)
	if err != nil {
		return rec, err
	}
	rec.LeafCount = uint32(count)
	rec.Timestamp, err = r.uintBits(wordTimestamp, bits64)
	return rec, err
}

// Publish anchors root for epoch. Only the coordinator key can do this.
func (a *AnchorContract) Publish(ctx context.Context, epoch uint64, root [hashLen]byte, leafCount uint32) (*Receipt, error) {
	return send(ctx, a.s, a.addr, "anchor(uint64,bytes32,uint32)",
		uint64Arg(epoch), bytes32Arg(root), uint64Arg(uint64(leafCount)))
}

// ── VyomaEscrow ──────────────────────────────────────────────────────────

// EscrowContract is the VyomaEscrow contract.
type EscrowContract struct {
	addr Address
	rpc  *RPC
	s    *Sender
}

// NewEscrowContract binds a VyomaEscrow. s may be nil for read-only use.
func NewEscrowContract(addr Address, rpc *RPC, s *Sender) *EscrowContract {
	return &EscrowContract{addr: addr, rpc: rpc, s: s}
}

// Address is the contract address.
func (e *EscrowContract) Address() Address { return e.addr }

// Credit is an owner's unspent deposit.
func (e *EscrowContract) Credit(ctx context.Context, owner Address) (*big.Int, error) {
	return viewUint(ctx, e.rpc, e.addr, "credit(address)", addrArg(owner))
}

// TotalCredit is the sum of all owners' credit.
func (e *EscrowContract) TotalCredit(ctx context.Context) (*big.Int, error) {
	return viewUint(ctx, e.rpc, e.addr, "totalCredit()")
}

// TotalUnclaimed is the sum of posted payouts not yet claimed.
func (e *EscrowContract) TotalUnclaimed(ctx context.Context) (*big.Int, error) {
	return viewUint(ctx, e.rpc, e.addr, "totalUnclaimed()")
}

// LastPeriod is the highest period posted so far.
func (e *EscrowContract) LastPeriod(ctx context.Context) (uint64, error) {
	out, err := callView(ctx, e.rpc, e.addr, "lastPeriod()")
	if err != nil {
		return 0, err
	}
	return abiReader{out}.uintBits(0, bits64)
}

// AllocationOf is what account may still claim for period.
func (e *EscrowContract) AllocationOf(ctx context.Context, period uint64, account Address) (*big.Int, error) {
	return viewUint(ctx, e.rpc, e.addr, "allocationOf(uint64,address)", uint64Arg(period), addrArg(account))
}

// PeriodInfo mirrors the contract's periodInfo view.
type PeriodInfo struct {
	Total, Unclaimed *big.Int
	ClaimableAt      uint64
	Vetoed           bool
}

// PeriodInfo reads one period. A period never posted has a zero Total.
func (e *EscrowContract) PeriodInfo(ctx context.Context, period uint64) (PeriodInfo, error) {
	var info PeriodInfo
	out, err := callView(ctx, e.rpc, e.addr, "periodInfo(uint64)", uint64Arg(period))
	if err != nil {
		return info, err
	}
	r := abiReader{out}
	if info.Total, err = r.big(wordTotal); err != nil {
		return info, err
	}
	if info.Unclaimed, err = r.big(wordUnclaimed); err != nil {
		return info, err
	}
	if info.ClaimableAt, err = r.uintBits(wordClaimableAt, bits64); err != nil {
		return info, err
	}
	info.Vetoed, err = r.boolean(wordVetoed)
	return info, err
}

// Deposit moves amount of the token from the sender into escrow credit. The
// sender must already have approved the escrow for at least amount.
func (e *EscrowContract) Deposit(ctx context.Context, amount *big.Int) (*Receipt, error) {
	return send(ctx, e.s, e.addr, "deposit(uint256)", uintArg(amount))
}

// PostPeriod posts one billing period. Only the coordinator key can do this.
func (e *EscrowContract) PostPeriod(
	ctx context.Context, period uint64, ref [hashLen]byte,
	owners []Address, debits []*big.Int, accounts []Address, amounts []*big.Int,
) (*Receipt, error) {
	if len(owners) != len(debits) || len(accounts) != len(amounts) {
		return nil, fmt.Errorf("%w: list lengths differ", ErrABI)
	}
	return send(ctx, e.s, e.addr, "postPeriod(uint64,bytes32,address[],uint256[],address[],uint256[])",
		uint64Arg(period), bytes32Arg(ref),
		addrArrayArg(owners), uintArrayArg(debits), addrArrayArg(accounts), uintArrayArg(amounts))
}

// Claim pays one account its allocation for period.
func (e *EscrowContract) Claim(ctx context.Context, period uint64, account Address) (*Receipt, error) {
	return send(ctx, e.s, e.addr, "claim(uint64,address)", uint64Arg(period), addrArg(account))
}

// ClaimMany pays several accounts in one transaction.
func (e *EscrowContract) ClaimMany(ctx context.Context, period uint64, accounts []Address) (*Receipt, error) {
	return send(ctx, e.s, e.addr, "claimMany(uint64,address[])", uint64Arg(period), addrArrayArg(accounts))
}

// Refund returns owner's unspent credit to owner after coordinator silence.
func (e *EscrowContract) Refund(ctx context.Context, owner Address) (*Receipt, error) {
	return send(ctx, e.s, e.addr, "refund(address)", addrArg(owner))
}

// DepositEvent is one Deposited log.
type DepositEvent struct {
	Owner       Address
	Amount      *big.Int
	BlockNumber uint64
	TxHash      [hashLen]byte
	LogIndex    uint64
}

var depositedTopic = Keccak256([]byte("Deposited(address,uint256)"))

// DepositedEvents returns Deposited logs in blocks from..to inclusive.
func (e *EscrowContract) DepositedEvents(ctx context.Context, from, to uint64) ([]DepositEvent, error) {
	logs, err := e.rpc.Logs(ctx, e.addr, depositedTopic, from, to)
	if err != nil {
		return nil, err
	}
	return parseDeposits(logs)
}

func parseDeposits(logs []Log) ([]DepositEvent, error) {
	out := make([]DepositEvent, 0, len(logs))
	for _, l := range logs {
		const topicsWanted = 2 // signature and the indexed owner
		if len(l.Topics) != topicsWanted {
			return nil, fmt.Errorf("%w: Deposited log has %d topics", ErrABI, len(l.Topics))
		}
		owner, err := abiReader{data: l.Topics[1][:]}.address(0)
		if err != nil {
			return nil, err
		}
		amount, err := abiReader{data: l.Data}.big(0)
		if err != nil {
			return nil, err
		}
		out = append(out, DepositEvent{
			Owner: owner, Amount: amount, BlockNumber: l.BlockNumber, TxHash: l.TxHash, LogIndex: l.LogIndex,
		})
	}
	return out, nil
}

// ── shared plumbing ──────────────────────────────────────────────────────

func callView(ctx context.Context, rpc *RPC, to Address, sig string, args ...abiArg) ([]byte, error) {
	data, err := encodeCall(sig, args...)
	if err != nil {
		return nil, err
	}
	return rpc.Call(ctx, nil, to, data)
}

func viewUint(ctx context.Context, rpc *RPC, to Address, sig string, args ...abiArg) (*big.Int, error) {
	out, err := callView(ctx, rpc, to, sig, args...)
	if err != nil {
		return nil, err
	}
	return abiReader{out}.big(0)
}

func send(ctx context.Context, s *Sender, to Address, sig string, args ...abiArg) (*Receipt, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: this binding is read-only (no sender)", ErrABI)
	}
	data, err := encodeCall(sig, args...)
	if err != nil {
		return nil, err
	}
	return s.Send(ctx, &to, data)
}
