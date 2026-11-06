# On-chain settlement on Whitechain

Vyomanaut keeps its payment ledger in paise in PostgreSQL. This feature lets
owners fund storage with USDT and lets providers be paid in USDT from an escrow
contract on Whitechain Sepolia (chain ID 1874), without changing the ledger.

## What happens

1. **Deposit.** An owner deposits USDT into `VyomaEscrow` from a wallet. The
   coordinator reads the `Deposited` log and credits the owner whose wallet it
   was, once, as a normal ledger deposit.
2. **Charge and release.** Unchanged. The ledger charges owners and releases
   earnings to providers.
3. **Post.** Released earnings are grouped by payout wallet into a period and
   posted to the contract, together with the owners' matching debits.
4. **Challenge window.** A separate guardian key can veto a period. A veto
   returns the owners' credit and the releases are paid in a later period.
5. **Claim.** After the window the coordinator submits the claims and pays the
   gas. Funds always go to the provider's wallet, never to the caller, so a
   provider needs no wallet software, no gas token and no RPC access.

If the coordinator stops posting, anyone can return an owner's unspent credit to
that owner, and periods already posted stay claimable by anyone.

## Why this needs on-chain stablecoin settlement

A bank or UPI rail cannot enforce that a payout survives the operator. Here a
posted payout is claimable by the provider even if the coordinator is gone, and
an owner's unspent deposit is returnable without the operator's cooperation.

## Running it

The coordinator runs this only when asked, and it never blocks uploads, audits,
repair or the ledger: if the chain is unreachable it retries in the background.

| Variable | Meaning |
|---|---|
| `VYOMANAUT_CHAIN_ENABLED` | `1` to turn it on |
| `VYOMANAUT_CHAIN_RPC_URL` | JSON-RPC endpoint (may contain an API key; never logged) |
| `VYOMANAUT_CHAIN_PRIVATE_KEY_FILE` | file holding the coordinator wallet key, mode 0600 |
| `VYOMANAUT_CHAIN_ESCROW_ADDRESS` | the `VyomaEscrow` address |
| `VYOMANAUT_CHAIN_START_BLOCK` | the block the escrow was deployed in |
| `VYOMANAUT_CHAIN_ID` | optional; refuse to start on a different chain |
| `VYOMANAUT_CHAIN_MICRO_PER_PAISE` | optional, default 10 (Rs 1,000 = 1 USDT in the demo) |
| `VYOMANAUT_CHAIN_CONFIRMATIONS` | optional, default 2 blocks |
| `VYOMANAUT_CHAIN_POLL_SECONDS` | optional, default 10 |
| `VYOMANAUT_CHAIN_SETTLE_LAG_SECONDS` | optional, default 20 |

The coordinator wallet needs a little WBT for gas. Migration
`migrations/002_whitechain.sql` is applied by `scripts/demo/up.sh`.

## Binding wallets

Until the CLI and API for this land, bind wallets directly:

```sql
INSERT INTO owner_wallets (owner_id, wallet) VALUES ('<owner uuid>', decode('<40 hex digits>', 'hex'));
INSERT INTO provider_payout_addresses (provider_id, wallet) VALUES ('<provider uuid>', decode('<40 hex digits>', 'hex'));
```

A deposit from an unbound wallet is recorded and credited as soon as its wallet
is bound. A release for a provider with no payout wallet waits.

## Limits

- The payout list is stored on chain, so posting costs gas linear in the number
  of providers paid in a period. That suits tens to low hundreds of providers.
- The ledger remains authoritative. On chain, owners are debited only for what
  providers are actually paid; earnings withheld by a reliability tier stay as
  the owner's escrow credit.
- Withdrawing unspent credit while the coordinator is active is not supported.
- Test tokens have no monetary value.
