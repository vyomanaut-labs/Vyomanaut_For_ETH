# On-chain settlement on Whitechain

Vyomanaut keeps its payment ledger in paise in PostgreSQL. This feature lets
owners fund storage with USDT and lets providers be paid in USDT from an escrow
contract on Whitechain Sepolia (chain ID 1874), without changing the ledger.

## What happens

1. **Deposit.** An owner deposits USDT into `VyomaEscrow` from a wallet. The
   coordinator reads the `Deposited` log and credits the owner whose wallet it
   was, once, as a normal ledger deposit.
2. **Charge and release.** The ledger charges owners and releases earnings to
   providers. Each release tick first closes an audit period for every ACTIVE
   provider (`payment.CloseAuditPeriods`); before this build no code created
   those periods, so no release ever happened in a live run.
3. **Post.** Released earnings are grouped by payout wallet into a period and
   posted to the contract, together with the owners' matching debits.
4. **Challenge window.** A separate guardian key can veto a period. A veto
   returns the owners' credit and the releases are paid in a later period.
5. **Claim.** After the window the coordinator submits the claims and pays the
   gas. Funds always go to the provider's wallet, never to the caller, so a
   provider needs no wallet software, no gas token and no RPC access.

If the coordinator stops posting, anyone can return an owner's unspent credit to
that owner, and periods already posted stay claimable by anyone. The coordinator
reads every `Refunded` log and records it as an owner WITHDRAWAL in the ledger
(migration 004), so a refunded deposit is never left credited.

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
| `VYOMANAUT_CHAIN_ANCHOR_ADDRESS` | the `AuditAnchor` address; audit anchoring is off unless this is set |
| `VYOMANAUT_CHAIN_ANCHOR_INTERVAL_SECONDS` | optional, default 600 |
| `VYOMANAUT_CHAIN_ANCHOR_MAX_LEAVES` | optional, default 20000 receipts per epoch |
| `VYOMANAUT_CHAIN_API_ADDR` | optional listen address of the wallet-binding API, e.g. `:8090` |
| `VYOMANAUT_CHAIN_AUTO_PAYOUT_WALLETS` | `1` to give providers derived (custodial) demo payout wallets |
| `VYOMANAUT_CHAIN_EXPLORER_URL` | optional explorer base URL shown to clients |

The coordinator wallet needs a little WBT for gas. Migration
`migrations/002_whitechain.sql` is applied by `scripts/demo/up.sh`.

## Anchoring the audit history

Every interval the coordinator takes the finished audit receipts it has not yet
anchored, builds one Merkle root over them (the shared leaf and tree rules in
`internal/receiptleaf`) and writes that 32-byte root and the receipt count to the
`AuditAnchor` contract as the next epoch. Receipts stay in the database.

Once an epoch is on chain the operator cannot rewrite, drop or add a receipt
inside it without the rebuilt root changing.

An epoch is reserved in the database with its root and exact range before it is
sent, and the chain is asked whether the epoch is already there before every
send, so a lost reply is adopted rather than repeated. Epoch numbers follow the
chain, so they keep rising across database resets. A failed epoch leaves the
next one starting from the same place.

### Checking the history against the chain

`verifyreceipt` is an operator-side tool (providers never run it). It rebuilds a
root from the database and compares it with the root it reads **from the chain's
RPC, never through the coordinator**: a verifier that asked the coordinator for
the root would prove nothing.

```bash
go build -o verifyreceipt ./cmd/verifyreceipt
export VYOMANAUT_CHAIN_RPC_URL=...  VYOMANAUT_CHAIN_ANCHOR_ADDRESS=0x...
export PGHOST=localhost PGUSER=vyomanaut_app PGPASSWORD=... PGDATABASE=vyomanaut_demo

verifyreceipt list                    # epochs the database says it anchored
verifyreceipt epoch latest            # rebuild the newest epoch and compare
verifyreceipt receipt <receipt_id>    # prove one receipt is inside its epoch
```

It prints MATCH or MISMATCH and how final the anchoring transaction is (soft:
the sequencer's word; finalized: settled on Ethereum), and exits 0 on a match and
1 on a mismatch. `scripts/demo/chain_tamper.sh` is the demo: it alters one stored
receipt as a database owner could, and the check fails; `--restore` puts it back.

What this does not prove: that a receipt was ever written. That is what the
provider's own signature on a PASS is for. A receipt that commits later than the
30-second settle lag after its timestamp can be missed by an epoch and will then
look like an addition; the lag exists to make that rare.

## Binding wallets

**Providers need nothing.** With `VYOMANAUT_CHAIN_AUTO_PAYOUT_WALLETS=1` the
coordinator gives every provider that has not left a payout address derived from
its own secret, as providers appear. A provider sees no new step, command or
question. These demo wallets are **custodial**: the coordinator can reproduce the
keys. A real provider would register their own address instead.

**Owners bring their own wallet and prove they control it.** Set
`VYOMANAUT_CHAIN_API_ADDR` (for example `:8090`) and the coordinator serves a small
API on that address (plain HTTP, for the demo network only):

| Route | Needs | What it does |
|---|---|---|
| `GET /chain/v1/info` | nothing | chain id, escrow and token addresses |
| `GET /chain/v1/wallet` | owner session | the owner's bound wallet, if any |
| `GET /chain/v1/wallet/message?wallet=0x…` | owner session | the exact text to sign |
| `POST /chain/v1/wallet` | owner session | `{wallet, signature}`: binds the wallet |

The owner's normal session token says who is asking; the wallet's signature over
the binding message (EIP-191, naming the owner, the wallet and the chain) says
who controls the wallet. Both are required, so a stranger cannot attach a wallet
to someone else's account and an owner cannot claim a wallet they do not hold.
A bound wallet is never silently replaced, and a wallet belongs to one owner.

A deposit from an unbound wallet is recorded and credited as soon as its wallet
is bound. A release for a provider with no payout wallet waits.

Without the API, wallets can still be bound with SQL:

```sql
INSERT INTO owner_wallets (owner_id, wallet) VALUES ('<owner uuid>', decode('<40 hex digits>', 'hex'));
INSERT INTO provider_payout_addresses (provider_id, wallet) VALUES ('<provider uuid>', decode('<40 hex digits>', 'hex'));
```

## The owner and escape-hatch command

`vyomachain` (`cmd/vyomachain`) is for data owners, and for anyone who has to move
money without the coordinator. Providers never run it. It talks to the chain
directly, so the last two commands need only an RPC endpoint and a wallet holding a
little gas token:

```bash
go build -o vyomachain ./cmd/vyomachain
export VYOMANAUT_CHAIN_RPC_URL=https://rpc.testnet.whitechain.io
export VYOMANAUT_CHAIN_API_URL=http://<coordinator>:8090     # supplies the contract addresses

vyomachain wallet new --out ~/vyoma-keys/me.key              # a new wallet; never overwrites
vyomachain wallet bind --key-file ~/vyoma-keys/me.key --data-dir ~/.vyomanaut-owner
vyomachain deposit --amount 1 --key-file ~/vyoma-keys/me.key # approve (if needed) + deposit
vyomachain balance --address 0x...                           # gas, USDT and escrow credit

vyomachain period 3 --account 0x...                          # what a payout period holds
vyomachain claim --period 3 --account 0x... --key-file any.key   # pays the ACCOUNT, not the caller
vyomachain refund --owner 0x... --key-file any.key           # after the coordinator went quiet
```

Too-early claims and refunds are explained with the time they open. A deposit
larger than the wallet's USDT balance fails before any gas is spent.

## Limits

- The payout list is stored on chain, so posting costs gas linear in the number
  of providers paid in a period. That suits tens to low hundreds of providers.
- The ledger remains authoritative. On chain, owners are debited only for what
  providers are actually paid; earnings withheld by a reliability tier stay as
  the owner's escrow credit.
- Withdrawing unspent credit while the coordinator is active is not supported.
- The silence timeout counts from the last posted period, or from deployment if
  none was ever posted. Until the first period is posted, an owner can refund at
  any time.
- Audit periods are closed on the release tick, so an announced departure finds
  no open period to prorate and releases nothing at departure; the held balance
  follows the existing departure rules.
- Test tokens have no monetary value.
