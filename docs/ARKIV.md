# Public audit mirror on Arkiv

Vyomanaut checks that providers still hold their shards by auditing them
continuously: more than 73,000 audits ran in the 10-machine campus demo. Those
receipts live in the coordinator's PostgreSQL database, so until now anyone who
wanted to trust them had to trust the operator.

This feature publishes a signed, public copy of that history to
[Arkiv](https://arkiv.network), a queryable entity database on an Ethereum
chain (the Rabdi testnet). Anyone can fetch the copy, check it on its own, and
compare it with what the operator's database says today. If the operator edits
a receipt after it was mirrored, the check turns red and names the receipt and
the field that changed.

PostgreSQL stays the source of truth. The mirror is off by default, and when it
is on it never blocks uploads, audits, repair or payments: if Arkiv, the RPC or
the wallet is unavailable, the coordinator keeps working and the publisher
retries in the background.

## What gets published

| Kind | One entity per | What it holds |
|---|---|---|
| `audit_batch` | provider, per audit period (2 minutes in the demo), split at 100 receipts | Every finished receipt of that provider in that period, a Merkle root over them, and the coordinator's signature |
| `file_manifest` | registered file | SHA-256 of the stored pointer, a root over the file's shard ids, segment and shard counts, RS(k, n), registration time, signature |
| `file_tombstone` | deleted file | The file id, when the deletion was seen, signature |

Every entity carries indexed attributes (`app`, `kind`, `provider_id`,
`period_start`, `seq`, `count`, `pass`, `fail`, `timeout`, `merkle_root`,
`coord_key`, ...) so it can be found with `arkiv_query`, and a `body` holding the
full data as JSON. Entities live for 30 days (`VYOMANAUT_ARKIV_LIFETIME_BLOCKS`).

### What is deliberately not published

- **`file_id` next to a provider.** Audit receipts are published without their
  file, exactly as `internal/receiptleaf` defines a receipt: publishing both
  would reveal which provider holds which file.
- **The encrypted pointer, owner, size or name of a file.** The pointer key is
  derived from the owner's passphrase (Argon2id), so public ciphertext would
  allow offline guessing. Manifests carry only a hash of the stored pointer.

## How the pieces fit

```
audit_receipts (PostgreSQL)                     Arkiv (Rabdi testnet)
  finished, settled 30 s  ──►  publisher  ──►   audit_batch entity
  one period at a time         (coordinator      body: receipts (JSON)
                                extension)       merkle_root, coord_sig
files / segments / chunks  ──►             ──►   file_manifest / file_tombstone

arkivcheck:  fetch entity by key ──► verify on its own ──► rebuild from PostgreSQL ──► compare
```

- **Receipts and roots** come from `internal/receiptleaf`, the same leaf encoding
  and RFC 6962 tree the Whitechain `AuditAnchor` uses. Only finished receipts
  are read, in the canonical order, 30 seconds after their countersign time.
- **Periods** are aligned to the Unix epoch (`[k*P, (k+1)*P)`), so every
  coordinator and every verifier computes the same boundaries. A period is
  published once it has closed and settled.
- **Signatures** are Ed25519 by the coordinator's key over a fixed-layout
  summary (domain tag, provider, period, seq, counts, root), never over JSON.
  The layouts are documented in `internal/arkiv/batch.go` and `manifest.go`.
- **Crash safety.** The publisher records each batch as `SENT` in
  `arkiv_publications` together with the cursor move, in one transaction, before
  sending it, then marks it `CONFIRMED` with the entity key and transaction hash.
  A row left in `SENT` by a crash is rebuilt from PostgreSQL and sent again; a
  duplicate on chain is harmless because batches are immutable and identical.
  Failures back off (30 s doubling to 10 min) and give up after 8 attempts.
- **Cost.** About 145k gas fixed plus about 57 gas per byte (measured on Rabdi):
  a full 100-receipt batch is about 83 KiB, about 5 M gas. Batching per provider
  per period keeps the fixed part small.

### Honest limits

- **TIMEOUT receipts** carry only the coordinator's signature: the provider
  never answered, so there is nothing for it to sign. PASS and FAIL receipts
  carry the provider's signature too.
- **The mirror proves the history did not change after publication**, not that
  it was true when published. A coordinator could publish false receipts; the
  provider signatures on PASS/FAIL make that detectable by the provider.
- **Lifetime.** Entities expire (30 days by default, at most about 182 days).
- **Queries.** Fetching by entity key (spike A0), the attribute query
  `app = str('…') AND kind = str('…')` and `offset` paging were all checked on
  Rabdi. `arkivcheck public` reads the first page only (up to 100 entities).

## Running it

### 1. A wallet for the publisher

Use a wallet that is separate from the Whitechain coordinator key. Create a key
file holding 32 bytes of hex on one line, outside the repository:

```bash
mkdir -p ~/vyoma-keys && chmod 700 ~/vyoma-keys
# any secp256k1 key generator works; with Foundry: cast wallet new
printf '%s\n' '<64 hex digits>' > ~/vyoma-keys/arkiv.key
chmod 600 ~/vyoma-keys/arkiv.key
```

Fund its address with tGLM from the faucet at `hub.arkiv.network`. If you have an
RPC access key, put it in `~/vyoma-keys/arkiv_access.key` (chmod 600); it is sent
as the `X-API-KEY` header and never logged.

### 2. Start the network with the mirror on

```bash
eval "$(scripts/demo/arkiv_env.sh)"          # prints export lines; notes go to stderr
scripts/demo/up.sh --providers 7 --advertise-host "$MY_IP" --expose-metrics
grep -E "EXTENSION|ARKIV" /tmp/vyomanaut-demo/logs/microservice.log
```

Expect `[EXTENSION] arkiv started`, then `[ARKIV] mirror running: ...`, then
every period `[ARKIV] period ...: N receipt(s) in M batch(es) queued` and one
`[ARKIV] audit_batch ... published: entity 0x…` line per batch. It combines with
`scripts/demo/chain_env.sh`; each feature has its own switch and key.

### 3. Check it

```bash
PGUSER=vyomanaut_app PGPASSWORD=devpass PGDATABASE=vyomanaut_demo \
  go run ./cmd/arkivcheck audits      # newest batches: Arkiv vs PostgreSQL
go run ./cmd/arkivcheck entity 0x…    # one entity, no database needed
scripts/demo/arkiv_tamper.sh          # green, edit one receipt, red
scripts/demo/arkiv_tamper.sh --restore
```

`arkivcheck` reads Arkiv directly, never through the coordinator. Pass
`--coord-key <hex>` to pin the coordinator's public key; without it, each
entity's own key is used and printed so you can compare it.

The Grafana dashboard **Vyomanaut — Arkiv public mirror**
(`deployments/grafana/dashboards/arkiv.json`) shows receipts mirrored, entities
by state, queue length, mirror lag, wallet balance, fees and errors.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `VYOMANAUT_ARKIV_ENABLED` | off | `1` turns the mirror on |
| `VYOMANAUT_ARKIV_PRIVATE_KEY_FILE` | required | Wallet key file (hex, chmod 600) |
| `VYOMANAUT_ARKIV_RPC_URL` | public Rabdi RPC | Arkiv JSON-RPC endpoint |
| `VYOMANAUT_ARKIV_ACCESS_KEY_FILE` | none | File holding an RPC access key |
| `VYOMANAUT_ARKIV_CHAIN_ID` | 7794095 | Expected chain; `0` skips the check |
| `VYOMANAUT_ARKIV_PERIOD_SECONDS` | the profile's audit period | Batch period |
| `VYOMANAUT_ARKIV_POLL_SECONDS` | 15 | How often the publisher looks for work |
| `VYOMANAUT_ARKIV_BACKFILL_SECONDS` | 0 | Start a fresh cursor this far in the past |
| `VYOMANAUT_ARKIV_MANIFESTS` | 1 | Publish file manifests and tombstones |
| `VYOMANAUT_ARKIV_APP_TAG` | `vyomanaut-demo` | `app` attribute on every entity |
| `VYOMANAUT_ARKIV_LIFETIME_BLOCKS` | 1,296,000 (30 days) | Entity lifetime |

## Where the code is

| Path | What |
|---|---|
| `internal/arkiv/client.go`, `entity.go`, `attribute.go` | Arkiv JSON-RPC client: create, query, encoding |
| `internal/arkiv/batch.go`, `manifest.go` | What is published, and how it is signed and verified |
| `internal/arkiv/publisher.go`, `store.go` | The background publisher and its PostgreSQL bookkeeping |
| `internal/arkiv/verify.go` | Checks used by `arkivcheck` |
| `cmd/microservice/arkiv_ext.go`, `config_arkiv.go` | Coordinator wiring (registers from `init()`) and settings |
| `cmd/arkivcheck` | The verifier |
| `migrations/003_arkiv.sql` | `arkiv_publications`, `arkiv_cursors` |
| `internal/metrics/arkiv.go`, `deployments/grafana/dashboards/arkiv.json` | Metrics and dashboard |
| `scripts/demo/arkiv_env.sh`, `arkiv_tamper.sh` | Demo helpers |

go-ethereum is imported only by `internal/arkiv`, which only `cmd/microservice`
and `cmd/arkivcheck` import: the provider and client binaries do not link it.
