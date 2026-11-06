// Package chainsettle mirrors the paise ledger onto an EVM chain so providers
// are paid in a stablecoin from an escrow contract, and so owners can fund that
// escrow directly from a wallet.
//
// The ledger stays the source of truth. This package only follows it:
//
//   - Deposits. It reads Deposited logs from the escrow contract and credits
//     the owner whose wallet made the deposit (a normal DEPOSIT row in
//     owner_escrow_events), exactly once per log.
//   - Payouts. It takes finished ledger RELEASE rows, groups them by payout
//     wallet into a period, posts the period to the contract, waits out the
//     challenge window, and submits the claims so providers need no gas.
//
// Every step is idempotent and recorded in the chain_* tables (migration 002).
// A period is first RESERVED in the database with its full payout list, then
// posted, so a crash at any point resumes without paying a release twice: on
// restart the period is checked against the chain and adopted if it is already
// there.
//
// The package never blocks the audit, upload or repair paths. It runs in its
// own goroutine, takes a Postgres advisory lock so only one coordinator
// settles at a time, and treats every chain error as retryable.
package chainsettle
