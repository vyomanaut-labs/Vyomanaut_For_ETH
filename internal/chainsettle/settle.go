package chainsettle

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

// settle advances every payout period one step: post what is reserved, reserve
// what the ledger has released since, post that, then claim what is claimable.
// Periods are posted strictly in order, because the contract requires
// increasing period numbers; a transient failure stops the cycle rather than
// letting a later period overtake an earlier one.
func (s *Service) settle(ctx context.Context) error {
	if n, err := s.store.unboundReleases(ctx); err == nil {
		s.obs.UnboundReleases(n)
	}
	if err := s.postReserved(ctx); err != nil {
		return err
	}
	if err := s.reserveNew(ctx); err != nil {
		return err
	}
	if err := s.postReserved(ctx); err != nil {
		return err
	}
	return s.claimPosted(ctx)
}

// ── reserve ──────────────────────────────────────────────────────────────

func (s *Service) reserveNew(ctx context.Context) error {
	cutoff := s.now().Add(-s.cfg.SettleLag)
	releases, err := s.store.unsettledReleases(ctx, cutoff, s.cfg.MaxItems)
	if err != nil || len(releases) == 0 {
		return err
	}

	owners, err := s.ownerCapacities(ctx)
	if err != nil {
		return err
	}
	var capacity int64
	for _, o := range owners {
		capacity += o.effective()
	}

	items, payouts, total := s.batch(releases, capacity)
	if total == 0 {
		s.logWaiting(len(releases))
		return nil
	}
	debits, err := SplitDebits(total, owners)
	if err != nil {
		return err
	}

	chainLast, err := s.escrow.LastPeriod(ctx)
	if err != nil {
		return fmt.Errorf("read last period: %w", err)
	}
	dbMax, err := s.store.maxPeriod(ctx)
	if err != nil {
		return err
	}
	period := max(chainLast, dbMax) + 1

	r := reservation{Period: period, Ref: referenceOf(items), TotalMicro: total, Items: items, Payouts: payouts, Debits: debits}
	if err := s.store.reserve(ctx, r); err != nil {
		return err
	}
	s.obs.PeriodReserved(total, len(payouts))
	log.Printf("[CHAIN] reserved period %d: %d releases, %d payees, %d micro-units", period, len(items), len(payouts), total)
	return nil
}

// waitLogEvery is how often the waiting line may repeat.
const waitLogEvery = time.Minute

// logWaiting says that earned releases are waiting for on-chain USDT. A demo
// that runs for an hour before the first deposit would otherwise print it every
// poll cycle, so it repeats at most once a minute.
func (s *Service) logWaiting(n int) {
	now := s.now()
	if !s.lastWaitLog.IsZero() && now.Sub(s.lastWaitLog) < waitLogEvery {
		return
	}
	s.lastWaitLog = now
	log.Printf("[CHAIN] %d earned release(s) are waiting: the owners have no on-chain USDT to pay them from yet (deposit with vyomachain)", n)
}

// batch picks releases, oldest first, until the payee limit or the owners'
// capacity is reached, and sums them per wallet.
func (s *Service) batch(releases []pendingRelease, capacity int64) (items []pendingRelease, payouts []payout, total int64) {
	index := map[chain.Address]int{}
	for _, r := range releases {
		micro := r.AmountPaise * s.cfg.MicroPerPaise
		if total+micro > capacity {
			break
		}
		pos, known := index[r.Wallet]
		if !known {
			if len(payouts) >= s.cfg.MaxPayees {
				break
			}
			index[r.Wallet] = len(payouts)
			payouts = append(payouts, payout{Wallet: r.Wallet})
			pos = len(payouts) - 1
		}
		payouts[pos].Micro += micro
		items = append(items, r)
		total += micro
	}
	return items, payouts, total
}

// ownerCapacities combines what each owner still owes with the credit they hold
// on chain.
func (s *Service) ownerCapacities(ctx context.Context) ([]OwnerCapacity, error) {
	weights, err := s.store.ownerWeights(ctx, s.cfg.MicroPerPaise)
	if err != nil {
		return nil, err
	}
	out := make([]OwnerCapacity, 0, len(weights))
	for _, w := range weights {
		credit, err := s.escrow.Credit(ctx, w.Wallet)
		if err != nil {
			return nil, fmt.Errorf("read credit of %s: %w", w.Wallet, err)
		}
		c := int64(0)
		if credit.IsInt64() {
			c = credit.Int64()
		} else if credit.Sign() > 0 {
			c = maxInt64
		}
		out = append(out, OwnerCapacity{Wallet: w.Wallet, Weight: w.Weight, Credit: c})
	}
	return out, nil
}

// ── post ─────────────────────────────────────────────────────────────────

func (s *Service) postReserved(ctx context.Context) error {
	periods, err := s.store.periodsWithStatus(ctx, statusReserved)
	if err != nil {
		return err
	}
	for _, p := range periods {
		if err := s.postOne(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) postOne(ctx context.Context, p periodRow) error {
	// A crash may have left a period on chain that the database still shows as
	// reserved. If the chain already holds this period with the same total,
	// adopt it instead of posting a second time.
	info, err := s.escrow.PeriodInfo(ctx, p.Period)
	if err != nil {
		return fmt.Errorf("period %d: read chain state: %w", p.Period, err)
	}
	if info.Total != nil && info.Total.Sign() > 0 {
		if info.Total.IsInt64() && info.Total.Int64() == p.TotalMicro && !info.Vetoed {
			log.Printf("[CHAIN] period %d is already on chain; adopting it", p.Period)
			return s.adopt(ctx, p, nil, info)
		}
		return s.abandon(ctx, p, "the period number is already used on chain with different content")
	}

	last, err := s.escrow.LastPeriod(ctx)
	if err != nil {
		return fmt.Errorf("period %d: read last period: %w", p.Period, err)
	}
	if p.Period <= last {
		return s.abandon(ctx, p, "a later period was already posted on chain")
	}

	payouts, err := s.store.payoutsOf(ctx, p.Period)
	if err != nil {
		return err
	}
	debits, err := s.store.debitsOf(ctx, p.Period)
	if err != nil {
		return err
	}
	ref, err := s.store.refOf(ctx, p.Period)
	if err != nil {
		return err
	}

	owners := make([]chain.Address, len(debits))
	debitAmounts := make([]*big.Int, len(debits))
	for i, d := range debits {
		owners[i], debitAmounts[i] = d.Wallet, big.NewInt(d.Micro)
	}
	accounts := make([]chain.Address, len(payouts))
	amounts := make([]*big.Int, len(payouts))
	for i, po := range payouts {
		accounts[i], amounts[i] = po.Wallet, big.NewInt(po.Micro)
	}

	rc, err := s.escrow.PostPeriod(ctx, p.Period, ref, owners, debitAmounts, accounts, amounts)
	if err != nil {
		if errors.Is(err, chain.ErrReverted) {
			// A rejection is deterministic: retrying would fail the same way.
			return s.abandon(ctx, p, err.Error())
		}
		s.store.recordError(ctx, p.Period, err.Error())
		return fmt.Errorf("period %d: post: %w", p.Period, err)
	}
	after, err := s.escrow.PeriodInfo(ctx, p.Period)
	if err != nil {
		return fmt.Errorf("period %d: read back: %w", p.Period, err)
	}
	return s.adopt(ctx, p, &rc.TxHash, after)
}

func (s *Service) adopt(ctx context.Context, p periodRow, tx *[32]byte, info chain.PeriodInfo) error {
	if err := s.store.markPosted(ctx, p.Period, tx, time.Unix(int64(info.ClaimableAt), 0).UTC()); err != nil {
		return err
	}
	s.obs.PeriodPosted(p.TotalMicro)
	log.Printf("[CHAIN] period %d posted, claimable at %s", p.Period, time.Unix(int64(info.ClaimableAt), 0).UTC().Format(time.RFC3339))
	return nil
}

func (s *Service) abandon(ctx context.Context, p periodRow, reason string) error {
	log.Printf("[CHAIN] abandoning period %d: %s", p.Period, reason)
	if err := s.store.failPeriod(ctx, p.Period, reason); err != nil {
		return err
	}
	s.obs.PeriodFailed(reason)
	return nil
}

// ── claim ────────────────────────────────────────────────────────────────

func (s *Service) claimPosted(ctx context.Context) error {
	periods, err := s.store.periodsWithStatus(ctx, statusPosted)
	if err != nil {
		return err
	}
	now := s.now().Add(s.cfg.ClaimMargin)
	for _, p := range periods {
		info, err := s.escrow.PeriodInfo(ctx, p.Period)
		if err != nil {
			return fmt.Errorf("period %d: read chain state: %w", p.Period, err)
		}
		if info.Vetoed {
			// The guardian cancelled it; the owners' credit came back on chain,
			// so the releases must be paid in a new period.
			if err := s.abandon(ctx, p, "vetoed by the guardian"); err != nil {
				return err
			}
			continue
		}
		if !p.ClaimableAt.Valid || p.ClaimableAt.Time.After(now) {
			continue
		}
		if err := s.claimOne(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) claimOne(ctx context.Context, p periodRow) error {
	payouts, err := s.store.payoutsOf(ctx, p.Period)
	if err != nil {
		return err
	}
	accounts := make([]chain.Address, len(payouts))
	for i, po := range payouts {
		accounts[i] = po.Wallet
	}
	rc, err := s.escrow.ClaimMany(ctx, p.Period, accounts)
	if err != nil {
		s.store.recordError(ctx, p.Period, err.Error())
		if errors.Is(err, chain.ErrReverted) {
			var re *chain.RevertError
			if errors.As(err, &re) && len(re.Reason) >= len("WindowStillOpen") && re.Reason[:len("WindowStillOpen")] == "WindowStillOpen" {
				return nil // the window is not over by the chain's clock; try again next cycle
			}
		}
		return fmt.Errorf("period %d: claim: %w", p.Period, err)
	}
	if err := s.store.markClaimed(ctx, p.Period, rc.TxHash); err != nil {
		return err
	}
	s.obs.PeriodClaimed(p.TotalMicro)
	log.Printf("[CHAIN] period %d claimed: %d payees paid", p.Period, len(accounts))
	return nil
}
