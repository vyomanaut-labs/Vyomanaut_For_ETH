package chainsettle

import (
	"context"
	"fmt"
	"log"
)

// scanDeposits reads Deposited logs from the cursor up to a few blocks behind
// the head and records each once.
func (s *Service) scanDeposits(ctx context.Context) error {
	head, err := s.head.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("read head: %w", err)
	}
	if head < s.cfg.Confirmations {
		return nil
	}
	safe := head - s.cfg.Confirmations

	next := s.cfg.StartBlock
	if cur, found, cerr := s.store.cursor(ctx, cursorDeposits); cerr != nil {
		return cerr
	} else if found {
		next = cur + 1
	}

	for next <= safe {
		to := min(next+s.cfg.LogChunk-1, safe)
		events, err := s.escrow.DepositedEvents(ctx, next, to)
		if err != nil {
			return fmt.Errorf("read logs %d-%d: %w", next, to, err)
		}
		recorded := 0
		for _, ev := range events {
			isNew, rerr := s.store.recordDeposit(ctx, ev)
			if rerr != nil {
				return rerr
			}
			if isNew {
				recorded++
			} else if !ev.Amount.IsInt64() || ev.Amount.Sign() <= 0 {
				log.Printf("[CHAIN] ignoring deposit in block %d with an unusable amount", ev.BlockNumber)
			}
		}
		if err := s.store.setCursor(ctx, cursorDeposits, to); err != nil {
			return err
		}
		if recorded > 0 {
			s.obs.DepositsRecorded(recorded)
		}
		s.obs.ScannedTo(to)
		next = to + 1
	}
	return nil
}

// creditDeposits credits every recorded deposit whose wallet is bound.
func (s *Service) creditDeposits(ctx context.Context) error {
	credited, err := s.store.creditPending(ctx, s.cfg.MicroPerPaise)
	if err != nil {
		return err
	}
	for _, c := range credited {
		s.obs.DepositCredited(c.Micro)
		log.Printf("[CHAIN] credited %d paise to owner %s for an on-chain deposit of %d micro-units",
			c.Paise, c.Owner, c.Micro)
	}
	return nil
}
