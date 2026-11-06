package chainsettle

import (
	"context"
	"fmt"
	"log"
)

// Refunds: VyomaEscrow.refund(owner) returns an owner's unspent on-chain credit
// once the coordinator has been silent for the contract's silence timeout.
// Anyone may send it, and the coordinator is not involved, so the money can
// leave the escrow while the ledger still shows the deposit it came from. The
// service reads every Refunded log and turns it into one owner WITHDRAWAL, so
// the ledger never credits an owner with USDT that is no longer in the escrow.
//
// Refunds use their own log cursor and table (chain_refunds, migration 004) and
// otherwise follow the deposit path exactly: recorded once whether or not the
// wallet is bound, applied once an owner is known, idempotent by
// (tx hash, log index).

// scanRefunds reads Refunded logs from the refund cursor up to a few blocks
// behind the head and records each once.
func (s *Service) scanRefunds(ctx context.Context) error {
	head, err := s.head.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("read head: %w", err)
	}
	if head < s.cfg.Confirmations {
		return nil
	}
	safe := head - s.cfg.Confirmations

	next := s.cfg.StartBlock
	if cur, found, cerr := s.store.cursor(ctx, cursorRefunds); cerr != nil {
		return cerr
	} else if found {
		next = cur + 1
	}

	for next <= safe {
		to := min(next+s.cfg.LogChunk-1, safe)
		events, err := s.escrow.RefundedEvents(ctx, next, to)
		if err != nil {
			return fmt.Errorf("read refund logs %d-%d: %w", next, to, err)
		}
		for _, ev := range events {
			isNew, rerr := s.store.recordRefund(ctx, ev)
			if rerr != nil {
				return rerr
			}
			if !isNew && (!ev.Amount.IsInt64() || ev.Amount.Sign() <= 0) {
				log.Printf("[CHAIN] ignoring refund in block %d with an unusable amount", ev.BlockNumber)
			}
		}
		if err := s.store.setCursor(ctx, cursorRefunds, to); err != nil {
			return err
		}
		next = to + 1
	}
	return nil
}

// applyRefunds writes a ledger WITHDRAWAL for every recorded refund whose
// wallet is bound.
func (s *Service) applyRefunds(ctx context.Context) error {
	applied, err := s.store.applyPendingRefunds(ctx, s.cfg.MicroPerPaise)
	if err != nil {
		return err
	}
	for _, r := range applied {
		log.Printf("[CHAIN] debited %d paise from owner %s for an on-chain refund of %d micro-units",
			r.Paise, r.Owner, r.Micro)
	}
	return nil
}
