package chainsettle

import (
	"math/big"
	"testing"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/chain"
)

func TestARefundBecomesOneLedgerWithdrawal(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	owner, w := e.newOwner(t), randWallet()
	e.bindOwner(t, owner, w)
	f.events = []chain.DepositEvent{{Owner: w, Amount: big.NewInt(1_000_000), BlockNumber: 10, TxHash: randTx()}}
	f.refunds = []chain.RefundEvent{{Owner: w, Caller: randWallet(), Amount: big.NewInt(1_000_000), BlockNumber: 20, TxHash: randTx(), LogIndex: 1}}
	svc := e.service(t, f, testConfig())

	mustRun(t, svc)
	if n, paise := e.ledgerDeposits(t, owner); n != 1 || paise != 100_000 {
		t.Fatalf("deposits = %d rows, %d paise; want 1 of 100000", n, paise)
	}
	if n, paise := e.ledgerWithdrawals(t, owner); n != 1 || paise != 100_000 {
		t.Fatalf("withdrawals = %d rows, %d paise; want 1 of 100000 (the refund must cancel the deposit)", n, paise)
	}

	// the same log again, and further cycles, must not debit twice
	f.refunds = append(f.refunds, f.refunds[0])
	f.head += 50
	mustRun(t, svc)
	mustRun(t, svc)
	if n, _ := e.ledgerWithdrawals(t, owner); n != 1 {
		t.Fatalf("a refund was debited %d times", n)
	}
}

func TestARefundFromAnUnboundWalletIsAppliedWhenTheWalletIsBound(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	owner, w := e.newOwner(t), randWallet()
	f.events = []chain.DepositEvent{{Owner: w, Amount: big.NewInt(2_000_000), BlockNumber: 10, TxHash: randTx()}}
	f.refunds = []chain.RefundEvent{{Owner: w, Caller: w, Amount: big.NewInt(2_000_000), BlockNumber: 30, TxHash: randTx()}}
	svc := e.service(t, f, testConfig())

	mustRun(t, svc)
	if e.count(t, `SELECT COUNT(*) FROM chain_refunds WHERE applied_at IS NULL`) != 1 {
		t.Fatal("the refund should be recorded, unapplied, before the wallet is bound")
	}
	if n, _ := e.ledgerWithdrawals(t, owner); n != 0 {
		t.Fatal("an unbound wallet must not be debited")
	}

	// binding later applies both: the deposit that left and the refund that took it
	e.bindOwner(t, owner, w)
	mustRun(t, svc)
	_, dep := e.ledgerDeposits(t, owner)
	_, wd := e.ledgerWithdrawals(t, owner)
	if dep != 200_000 || wd != 200_000 {
		t.Fatalf("deposit %d paise, withdrawal %d paise; want both 200000 so the owner gains nothing", dep, wd)
	}
}

func TestAPartialRefundRoundsDownLikeADeposit(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	owner, w := e.newOwner(t), randWallet()
	e.bindOwner(t, owner, w)
	// 1 USDT deposited, 10,000 micro debited by a period, 990,005 micro refunded
	f.refunds = []chain.RefundEvent{
		{Owner: w, Amount: big.NewInt(990_005), BlockNumber: 12, TxHash: randTx()},
		{Owner: w, Amount: big.NewInt(7), BlockNumber: 13, TxHash: randTx()}, // dust: no ledger row
	}
	svc := e.service(t, f, testConfig())
	mustRun(t, svc)
	if n, paise := e.ledgerWithdrawals(t, owner); n != 1 || paise != 99_000 {
		t.Fatalf("withdrawals = %d rows, %d paise; want 1 of 99000", n, paise)
	}
	if e.count(t, `SELECT COUNT(*) FROM chain_refunds WHERE applied_at IS NOT NULL`) != 2 {
		t.Fatal("both refunds, the dust one included, should be marked applied")
	}
}

func TestRefundsAndDepositsKeepSeparateCursors(t *testing.T) {
	e, f := newTestEnv(t), newFakeEscrow()
	cfg := testConfig()
	cfg.StartBlock = 1
	f.head = 30
	svc := e.service(t, f, cfg)
	mustRun(t, svc)

	var dep, ref int64
	if err := e.db.QueryRow(`SELECT block_number FROM chain_cursors WHERE name = 'deposits'`).Scan(&dep); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(`SELECT block_number FROM chain_cursors WHERE name = 'refunds'`).Scan(&ref); err != nil {
		t.Fatal(err)
	}
	if dep != 28 || ref != 28 {
		t.Fatalf("cursors deposits=%d refunds=%d; want both 28 (two confirmations held back)", dep, ref)
	}

	// a refund in a block the deposit scan has already passed is still found
	owner, w := e.newOwner(t), randWallet()
	e.bindOwner(t, owner, w)
	f.refunds = []chain.RefundEvent{{Owner: w, Amount: big.NewInt(5000), BlockNumber: 29, TxHash: randTx()}}
	f.head = 31
	mustRun(t, svc)
	if n, paise := e.ledgerWithdrawals(t, owner); n != 1 || paise != 500 {
		t.Fatalf("withdrawals = %d rows, %d paise; want 1 of 500", n, paise)
	}
}
