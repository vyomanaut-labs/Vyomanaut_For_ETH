package chainsettle

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// OwnerCapacity is how much of a payout one owner can fund.
type OwnerCapacity struct {
	Wallet chain.Address
	// Weight is the owner's outstanding charges in micro-units: what the
	// ledger says they owe providers and the chain has not yet debited.
	Weight int64
	// Credit is the owner's spendable credit in the escrow contract, in
	// micro-units.
	Credit int64
}

// Debit is how much of a payout one owner's on-chain credit pays.
type Debit struct {
	Wallet chain.Address
	Micro  int64
}

func (o OwnerCapacity) effective() int64 {
	w, c := o.Weight, o.Credit
	if w < 0 {
		w = 0
	}
	if c < 0 {
		c = 0
	}
	return min(w, c)
}

// SplitDebits divides total across owners in proportion to what each can fund,
// using integer arithmetic only. The result sums to exactly total, never asks
// an owner for more than their weight or their on-chain credit, and is
// deterministic. It returns ErrInsufficientCapacity if the owners together
// cannot fund total.
func SplitDebits(total int64, owners []OwnerCapacity) ([]Debit, error) {
	if total <= 0 {
		return nil, nil
	}
	// The sum is exact: capacities near the int64 limit must not be clipped,
	// or the proportions below would be wrong.
	bigSum := new(big.Int)
	for _, o := range owners {
		bigSum.Add(bigSum, big.NewInt(o.effective()))
	}
	bigTotal := big.NewInt(total)
	if bigSum.Cmp(bigTotal) < 0 {
		return nil, fmt.Errorf("%w: need %d, owners can fund %s", ErrInsufficientCapacity, total, bigSum)
	}

	type share struct {
		idx       int
		base      int64
		remainder *big.Int // numerator of the discarded fraction (over sum)
	}
	shares := make([]share, 0, len(owners))
	var assigned int64
	for i, o := range owners {
		e := o.effective()
		if e == 0 {
			continue
		}
		prod := new(big.Int).Mul(bigTotal, big.NewInt(e))
		q, r := new(big.Int).QuoRem(prod, bigSum, new(big.Int))
		shares = append(shares, share{idx: i, base: q.Int64(), remainder: r})
		assigned += q.Int64()
	}

	// Hand out the units lost to rounding, largest discarded fraction first.
	// Because total <= the sum, a share with a non-zero fraction is strictly below
	// its owner's capacity, so adding one never exceeds it.
	order := make([]int, len(shares))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return shares[order[a]].remainder.Cmp(shares[order[b]].remainder) > 0
	})
	for left, k := total-assigned, 0; left > 0 && k < len(order); left, k = left-1, k+1 {
		shares[order[k]].base++
	}

	out := make([]Debit, 0, len(shares))
	for _, s := range shares {
		if s.base > 0 {
			out = append(out, Debit{Wallet: owners[s.idx].Wallet, Micro: s.base})
		}
	}
	return out, nil
}

// maxInt64 is the largest int64.
const maxInt64 = int64(^uint64(0) >> 1)
