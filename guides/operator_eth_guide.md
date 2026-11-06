# Operator & Data Owner Guide — the Whitechain finale (macOS)

This guide adds a finale to `operator_unix_guide.md`. **Run the original demonstration exactly
as it is written there (Act 1). Then use the same running network to show what a blockchain
adds (Act 2).** Read that guide first; nothing in it changes except one extra line before
`up.sh`.

| | What runs | What the audience sees |
| --- | --- | --- |
| **Act 1** | The original demo, unchanged | Files stored as noise, audits, a machine dying, repair, payments in the ledger |
| **Act 2** | The same network, now using Whitechain | Three guarantees that a database alone cannot give |

The three guarantees, and the scene that shows each:

| Trust problem | Before (Act 1) | With the blockchain (Act 2) | Scene |
| --- | --- | --- | --- |
| Can the operator quietly rewrite the audit history? | Records sit in a database we control | A fingerprint of the records is written to a public chain; any change is caught | 2.1 |
| Where does a provider actually get paid? | A number in our ledger | Each provider has a wallet address on a public chain; real USDT arrives there | 2.2 |
| What if the operator disappears? | Unpaid earnings vanish with us | An escrow contract pays from a posted list, and anyone can finish a payout | 2.3, 2.4 |

> **Providers do nothing new**, in either act. No wallet, no coins, no new command, no new
> question at sign-up. Their guides are unchanged.
>
> The chain features run quietly from the first minute. Nothing on the screen in Act 1 looks
> different. That is deliberate: Act 2 is the reveal.

---

## Part 1 — The words

| Word | What it means here |
| --- | --- |
| **Wallet** | An address (`0x…`) plus a secret key file that controls it. Like an account number plus a pen that signs. |
| **USDT** | A coin worth one US dollar. Here it is *test* money with no value. Demo scale: **1 USDT = ₹1,000**. |
| **WBT** | Whitechain's fuel. Every transaction costs a tiny bit. Separate from USDT. |
| **Escrow contract** | A small program on the public chain that holds owners' USDT. Only its fixed rules can move the money. |
| **Period** | One batch of payouts, numbered 1, 2, 3… posted to the contract once. |
| **Challenge window** | 120 seconds after posting, in which a separate "guardian" key can cancel a bad period. After that it is final. |
| **Claim** | The step that sends USDT to a provider's wallet. **Anyone** can do it; the money always goes to the provider, never to whoever pressed the button. |
| **Refund** | If the coordinator posts nothing for 900 seconds, anyone can send an owner's unspent USDT back to that owner. |
| **Epoch** | One batch of audit records, summed into one fingerprint (a Merkle root) and written to the chain. |

> The ledger is still the boss. The chain follows it. If the chain is down, uploads, audits and
> repair carry on, and payouts and fingerprints catch up when it returns.

---

## Part 2 — Before the demo (once)

### 2.1 Build the two small tools

```bash
go build -o "$BIN_DIR/vyomachain"    ./cmd/vyomachain       # owner + escape-hatch tool
go build -o "$BIN_DIR/verifyreceipt" ./cmd/verifyreceipt    # audit-history checker
```

> `$BIN_DIR` comes from `source /tmp/vyomanaut-demo/env` (after `up.sh` has run). The demo scripts
> in Act 2 build these for you if you skip this. Neither needs RocksDB.

### 2.2 The three wallets

Key files live in `~/vyoma-keys/` on the operator's Mac. **Never upload, paste or commit a key
file.** Only the addresses are safe to share.

| Role | Address | Key file | Needs |
| --- | --- | --- | --- |
| Coordinator (posts periods, pays fees) | `0xc86553A4fC45352Bd79F1A45a0c338E598B49888` | `deployer.key` | WBT |
| Data owner (puts USDT in) | `0x7CBEe871deaDF21C3f8506283a2b96544b27FEb1` | `owner.key` | USDT and a little WBT |
| Guardian (can cancel a period) | `0xf4d6D323970cc80DC39461b29A272b34c4767cb9` | `guardian.key` | a few cents of WBT, only for a cancel demo |

> Out of WBT? The faucet gives 2 WBT a day per GitHub account: <https://faucet.testnet.whitechain.io>.
> Check balances any time with `vyomachain balance --address <wallet>` (Part 5).

### 2.3 Observability with `--providers 7`

Part 4.3 of the normal guide starts Prometheus and Grafana. With seven providers on **one Mac**,
the scrape-target files under `deployments/observability/targets/` should be:

`microservice.json` — the coordinator. Use your own ZeroTier address, `$MY_IP`, and port `8080`:

```json
[
  {
    "targets": ["10.35.114.52:8080"],
    "labels": { "desk": "COORDINATOR" }
  }
]
```

`providers.json` — leave empty:

```json
[]
```

`hosts.json` — clear it. It still lists the lab PCs from an earlier run, which would only show up
red:

```json
[]
```

> **Why `providers.json` stays empty.** Each provider's metrics page is private to its own machine
> by default (`127.0.0.1:9091`), and all seven local providers share one machine, so only one could
> even claim that port. Prometheus runs inside Docker and cannot see a private page on the host
> anyway. The per-machine panels in the lower half of the main dashboard therefore stay empty in
> this run. **That is expected, not broken.** They fill only with real laptops started with the
> metrics flag, as in the normal guide.
>
> **The coordinator is the one that matters here.** Everything on the chain dashboard (Part 7) and the
> top nine panels of the main dashboard come from the coordinator's own page. That needs
> `--expose-metrics` on `up.sh` (Part 3) and the file above.
>
> Check it: open <http://localhost:9090/targets>. `vyomanaut-microservice` should turn **UP** within
> about 30 seconds of the coordinator starting. If it stays red, replace the address with
> `host.docker.internal:8080` (some Docker setups cannot reach the Mac's ZeroTier address).
>
> These files are tracked in git. Undo your edits afterwards so they are not committed:
> `git checkout deployments/observability/targets/`.

---

## Part 3 — Start the network

Do Parts 1 to 4.3 of `operator_unix_guide.md` as usual (ZeroTier, CGO flags, Postgres,
observability). Then, in the **same terminal** you will run `up.sh` in:

```bash
export NETWORK_ID="b103a835d24e3e5f"   # your ZeroTier network ID
export MY_IP="$(sudo zerotier-cli listnetworks | grep "$NETWORK_ID" | awk '{print $NF}' | cut -d'/' -f1)"
echo "$MY_IP"

scripts/demo/down.sh
rm -rf /tmp/vyomanaut-demo

eval "$(scripts/demo/chain_env.sh)"      # the one new line
scripts/demo/up.sh --providers 7 --advertise-host "$MY_IP" --expose-metrics
```

> **The `eval` line must be in the same terminal as `up.sh`, and before it.** The coordinator
> inherits whatever is exported. Skip it and the demo still runs, with no blockchain.
>
> `chain_env.sh` prints what it set. In plain words: *turn the chain on; where the blockchain is;
> which key signs for the coordinator; which contracts; refuse to run on the wrong chain; where to
> start reading deposits (right now, so old runs are ignored); anchor audit records every two
> minutes; open a small wallet sign-up page on port 8090; give providers a payout address
> automatically.* It never reads or prints a key, only the path. If `deployer.key` is elsewhere,
> `export VYOMANAUT_CHAIN_PRIVATE_KEY_FILE=<path>` first.
>
> `--providers 7` onboards seven providers on this Mac by itself. There are no machines to join and
> no codes to read out. They are separate programs, not fakes. As in the normal guide, wait about
> **ten to twelve minutes** after the last one joins for the network to become ready.

### 3.1 Check that the chain woke up

```bash
grep -E "CHAIN|ANCHOR|EXTENSION" /tmp/vyomanaut-demo/logs/microservice.log
```

> Look for these, and no `WARNING`:
>
> - `[EXTENSION] chain started`
> - `[CHAIN] demo payout wallets: providers get an address derived from the coordinator's key (custodial)`
> - `[CHAIN] settlement running: endpoint rpc.testnet.whitechain.io, escrow 0xBcE8…, coordinator wallet 0xc865…`
> - `[ANCHOR] anchoring running: anchor contract 0x1b2b…, every 2m0s`
> - `[CHAIN] chain API listening on :8090 …`
> - `[CHAIN] assigned demo payout wallets to N provider(s)` once providers appear
>
> No `[CHAIN]` lines at all means the `eval` was skipped or run in another terminal. Stop here and
> start again. `[CHAIN] cannot start yet (retrying)` means it keeps trying; see Part 9.

Keep this open in its own window for the whole demo:

```bash
tail -f /tmp/vyomanaut-demo/logs/microservice.log | grep -E "CHAIN|ANCHOR"
```

---

## Part 4 — Act 1: the original demonstration, unchanged

Run it exactly as `operator_unix_guide.md` describes. Nothing about the owner's side changes: they
register, **add pretend money with `client deposit` the normal way**, upload, compare the two
views, retrieve, and recover on a "new device".

| Step | In the normal guide | Note for this run |
| --- | --- | --- |
| Open the live console | 4.6 | Same |
| Wait for the network to be ready | Part 5 | Seven local providers join by themselves |
| Owner registers, deposits, uploads, compares views, retrieves, recovers | Part 6 | Same. Use `client deposit` as written there |
| Kill a machine and show repair | Part 7 | Use `scripts/demo/kill_provider.sh <provider_id>` instead of asking a teammate (read its header) |
| Delete a file | Part 8 | **Skip it until after Act 2.** Deleting stops the charges that fund the finale's payouts |

> **Leave at least ten minutes between the upload and Act 2.** Charges and releases run on two-minute
> timers with a one-minute hold, and the finale pays out what has been earned by then.

What happens quietly in the background, with no action from you:

- Every two minutes the audit records are fingerprinted and written to the chain
  (`[ANCHOR] epoch N anchored`).
- Each provider gets a payout address.
- Once a minute the coordinator notes that earned payouts are waiting for on-chain USDT
  (`N earned release(s) are waiting …`). That line is what makes Act 2 possible. It is not an error.

### 4.1 Check you are ready for Act 2

```bash
grep ANCHOR /tmp/vyomanaut-demo/logs/microservice.log | tail -2
grep "earned release" /tmp/vyomanaut-demo/logs/microservice.log | tail -1
```

> You want at least one `epoch N anchored` and the `earned release(s) are waiting` line. If both are
> there, everything is in place.

---

## Part 5 — Act 2: the blockchain finale

Open a **new terminal** and set it up once:

```bash
cd ~/Vyomanaut_For_ETH
source /tmp/vyomanaut-demo/env              # BIN_DIR, MICROSERVICE_URL
eval "$(scripts/demo/chain_env.sh)"          # chain variables for THESE commands (the coordinator is already running)
export OWNER_DIR="$HOME/.vyomanaut-owner"
export PGPASSWORD=devpass
export PGUSER=vyomanaut_app                  # verifyreceipt reads the audit records as this role
go build -o "$BIN_DIR/vyomachain"    ./cmd/vyomachain
go build -o "$BIN_DIR/verifyreceipt" ./cmd/verifyreceipt
```

### Scene 2.1 — Audit-root anchoring: "can the operator rewrite history?"

```bash
"$BIN_DIR/verifyreceipt" list
"$BIN_DIR/verifyreceipt" epoch latest
```

> `list` shows every batch of audit records the coordinator fingerprinted during Act 1.
> `epoch latest` rebuilds the fingerprint from the database and compares it with the one **read from
> the blockchain**, never through the coordinator. You want `RESULT : MATCH`. It also says how final
> the write is: *soft* means Whitechain's operator confirmed it; *finalized* means it has settled on
> Ethereum.

Now the cheat:

```bash
scripts/demo/chain_tamper.sh
```

> Three steps print one after another: the honest history **matches**; one stored audit record is
> changed the way an operator with database access could; the same check now says
> `RESULT : MISMATCH`. Then put it back so the demo can repeat:
>
> ```bash
> scripts/demo/chain_tamper.sh --restore
> ```
>
> Be straight about what this proves: records **already anchored** cannot be quietly changed. It does
> not prove a record was ever written; each provider's own signature on a PASS does that.

### Scene 2.2 — Wallet payout address: "where does a provider get paid?"

Show that every provider already has an address on a public chain:

```bash
psql -h localhost -U vyomanaut_migrator -d vyomanaut_demo -c "SELECT p.phone_number, '0x'||encode(a.wallet,'hex') AS payout_wallet FROM provider_payout_addresses a JOIN providers p USING (provider_id) ORDER BY 1;"
```

Pick one address and look at it:

```bash
"$BIN_DIR/vyomachain" balance --address <payout_wallet>
```

> It prints the wallet's USDT (`0` for now) and an explorer link. Open the link: a public page for a
> wallet that belongs to a provider who has never installed a wallet or touched a coin.
>
> **Be honest:** in the demo these wallets are **custodial**. The coordinator derives them from its own
> secret, so the provider never sees one. A real provider would bring their own address. The money
> still lands at that address on the public chain.

Now the owner connects their own wallet, proving they hold it:

```bash
"$BIN_DIR/vyomachain" wallet bind --api "http://$MY_IP:8090" --data-dir "$OWNER_DIR" --key-file ~/vyoma-keys/owner.key
```

> It signs one short sentence with the wallet — "this owner, this wallet, this chain" — and sends it
> with the owner's normal login. The coordinator checks both, so nobody can attach a wallet to
> someone else's account, or claim a wallet they do not control. You want `bound: 0x7CBE…`.

### Scene 2.3 — Stablecoin escrow: real money pays the earnings

The owner puts real test USDT in:

```bash
"$BIN_DIR/vyomachain" deposit --amount 1 --key-file ~/vyoma-keys/owner.key
```

> It checks the wallet holds enough, lets the escrow take the USDT (first time only), then deposits, and
> prints two links. Open the `deposited` one: `https://explorer.testnet.whitechain.io/tx/0x…`. **That
> is a qualifying link for the Whitechain prize:** a USDT payment on Whitechain in the main flow. Save
> it for the README. `1` USDT is `₹1,000` at demo scale.

Watch the `[CHAIN]` window. Within about a minute:

```
[CHAIN] credited 100000 paise to owner <id> for an on-chain deposit of 1000000 micro-units
[CHAIN] reserved period 1: … releases, … payees, … micro-units
[CHAIN] period 1 posted, claimable at <time>
```

Two minutes after `posted`:

```
[CHAIN] period 1 claimed: N payees paid
```

> What just happened: the owner's real USDT is now paying what the providers **earned during Act 1**. The
> coordinator wrote the payout list onto the blockchain (`posted`), waited out the 120-second challenge
> window, then sent each provider its USDT (`claimed`), paying the small fee itself. The providers did
> nothing.
>
> **Expect fractions of a USDT.** The demo file is small and its charges are a few paise, so a provider
> receives something like `0.004` USDT. The flow is the point, not the size.

```bash
"$BIN_DIR/vyomachain" period 1
"$BIN_DIR/vyomachain" balance --address <payout_wallet>
```

> `period 1` shows its total and whether it is claimable or fully claimed. The balance of the wallet you
> looked at in Scene 2.2 has gone from `0` to something. Open its **Token transfers** tab on the
> explorer to see the USDT arrive.

### Scene 2.4 — The capstone: the operator disappears

New earnings keep arriving every two minutes, and the owner's leftover USDT still funds them. Wait for
the **next** `period N posted` line in the `[CHAIN]` window. Then, within about two minutes:

```bash
scripts/demo/chain_escape.sh
```

> It narrates four steps:
>
> 1. **Stops only the coordinator.** Not Postgres, not the providers.
> 2. **Waits for the 120-second window** to end (anyone can claim after it).
> 3. **A stranger claims.** It uses `~/vyoma-keys/owner.key` as "any wallet with a little WBT" (override
>    with `ESCAPE_KEY_FILE`). The money goes to the providers' wallets, not to the stranger.
> 4. **Shows each provider's USDT balance.**
>
> Run `scripts/demo/chain_escape.sh --dry` first if you want to see what it would do. You have about two
> minutes after `posted`; after that the coordinator has already paid the period itself and there is
> nothing left to show, so wait for the next one.
>
> On the explorer, the `Claimed` event lists **two** addresses: the *account* (the provider, who got the
> money) and the *caller* (the stranger, who paid the fee). They differ. That is the whole story on one
> screen.

**Say the limit out loud:** only money that was **already posted** is protected. Earnings the coordinator
had not posted yet when it stopped are not. And fetching files back still needs a coordinator.

Optional, if time allows. The owner can leave with what is unspent once the coordinator has been silent for
**900 seconds** (fifteen minutes) after its last post:

```bash
"$BIN_DIR/vyomachain" balance --address 0x7CBEe871deaDF21C3f8506283a2b96544b27FEb1   # "escrow credit" is what is waiting
"$BIN_DIR/vyomachain" refund  --owner   0x7CBEe871deaDF21C3f8506283a2b96544b27FEb1 --key-file ~/vyoma-keys/owner.key
```

> Too early, it says when refunds open (`refunds open at 14:42:10 UTC`). That is the contract working, not
> a bug.

The demo is now over: the coordinator is stopped. Clean up with `scripts/demo/down.sh` (Part 8).

---

## Part 6 — Every command you will use

```bash
# ── operator, once per run, before up.sh ─────────────────────────────────
eval "$(scripts/demo/chain_env.sh)"

# ── Act 2 scenes ─────────────────────────────────────────────────────────
"$BIN_DIR/verifyreceipt" list                 # epochs the database says it anchored
"$BIN_DIR/verifyreceipt" epoch latest         # rebuild and compare with the chain
"$BIN_DIR/verifyreceipt" receipt <receipt_id> # prove one receipt is inside the on-chain root
scripts/demo/chain_tamper.sh                  # alter one record; the check catches it
scripts/demo/chain_tamper.sh --restore        # put it back
"$BIN_DIR/vyomachain" wallet bind --api "http://$MY_IP:8090" --data-dir "$OWNER_DIR" --key-file ~/vyoma-keys/owner.key
"$BIN_DIR/vyomachain" deposit --amount 1 --key-file ~/vyoma-keys/owner.key
"$BIN_DIR/vyomachain" balance --address <wallet>
"$BIN_DIR/vyomachain" period <N> [--account <wallet>]
scripts/demo/chain_escape.sh [--dry]          # stop the coordinator; a stranger gets a provider paid

# ── anyone, even with no coordinator ─────────────────────────────────────
"$BIN_DIR/vyomachain" claim  --period <N> --account <provider_wallet> --key-file <any wallet with a little WBT>
"$BIN_DIR/vyomachain" refund --owner <owner_wallet> --key-file <any wallet with a little WBT>
```

> `vyomachain` finds the chain from `VYOMANAUT_CHAIN_RPC_URL` and `VYOMANAUT_CHAIN_ESCROW_ADDRESS`
> (both set by `chain_env.sh`). On a separate owner machine, set `VYOMANAUT_CHAIN_RPC_URL` and
> `VYOMANAUT_CHAIN_API_URL=http://<operator's 10.x address>:8090`; the second one tells it which
> contracts to use. Build it there with `go build -o vyomachain ./cmd/vyomachain`.
>
> Act 1 uses `client deposit` (pretend money) and Act 2 uses `vyomachain deposit` (real test USDT). Both
> are fine in the same run: the ledger may hold more than the contract, and payouts simply wait for
> on-chain USDT. That wait is exactly what Scene 2.3 resolves.

---

## Part 7 — The dashboard

With `--expose-metrics` and the coordinator in `microservice.json` (Part 2.3), Grafana at
<http://localhost:3000> shows a second dashboard next to the main one:

> Dashboards → **Vyomanaut** folder → **"Vyomanaut — Whitechain settlement and audit anchoring"**

| Panel | Healthy looks like |
| --- | --- |
| USDT deposited by owners / paid to providers | Stay at `0` through Act 1, then climb in Scene 2.3 |
| Payout periods by state | `reserved → posted → claimed` appear in order |
| Releases waiting for a wallet | `0`. Above `0` means a provider has no wallet yet |
| Newest audit epoch anchored | Rises every two minutes from the start of Act 1 |
| Chain step errors | Flat. A few in a row during a network blip is normal; a steady stream means the node is unreachable or the coordinator wallet is out of WBT |

---

## Part 8 — Shutting down, and running again

```bash
scripts/demo/down.sh
rm -rf /tmp/vyomanaut-demo
git checkout deployments/observability/targets/      # undo the edits from 2.3
```

`up.sh` wipes the database every time. The blockchain remembers everything.

- Run `eval "$(scripts/demo/chain_env.sh)"` again before `up.sh`, so `START_BLOCK` is fresh and old
  deposits are not counted again.
- Period and epoch numbers keep rising across runs. The coordinator handles that on its own.
- Provider wallets are assigned again automatically. The owner must **bind again** (Scene 2.2): the
  database was wiped.
- Unspent USDT from an earlier run stays inside the contract. Take it back with the refund command.
- There is **no limit** on how many times you can run this. The real limits are fuel and coins: WBT for
  fees (about a thousandth of a WBT per transaction; anchoring every two minutes is about thirty
  transactions an hour) and USDT (110 in the owner wallet; a run uses very little).

---

## Part 9 — When something goes wrong

| Symptom | Cause | Fix |
| --- | --- | --- |
| `up.sh` stops with `declare: -A: invalid option` | Old `apply_extra_migrations.sh`: macOS bash 3.2 has no associative arrays | Apply patch 0015, then `down.sh` and start again |
| No `[CHAIN]` lines at all | `chain_env.sh` was not run, or was run in a different terminal from `up.sh` | `down.sh`, `eval` again in the same terminal, `up.sh` |
| `[CHAIN] cannot start yet (retrying)` | Node unreachable, or the key file is readable by others | Check internet; `chmod 600 ~/vyoma-keys/deployer.key` |
| `WARNING: this key … is not the escrow's coordinator` / `… the anchor's coordinator` | Wrong key file | Use `deployer.key` |
| `vyomanaut-microservice` red on <http://localhost:9090/targets> | Prometheus cannot reach the coordinator | Check `--expose-metrics`; try `host.docker.internal:8080` (Part 2.3) |
| Lower half of the main dashboard is empty | Expected with `--providers 7` | Nothing to fix; see Part 2.3 |
| `wallet bind`: `cannot reach the coordinator's chain API` | Wrong address, or the coordinator was not started with the chain on | Check `--api`; look for the `chain API listening` line |
| `wallet bind`: `refused (unauthorized)` | The owner's login expired (24 hours) | `client recover`, or register again, then bind |
| `wallet bind`: `already_bound` / `wallet_taken` | That owner has a different wallet, or the wallet belongs to another owner | Use the same wallet, or a different one |
| Deposit made but no `credited` line | Wallet not bound, or the deposit was made before `chain_env.sh` ran | Bind it; the credit appears by itself. If too early, deposit again |
| `vyomachain deposit`: the wallet holds less USDT | Not enough test USDT | `vyomachain balance`; swap on YetiTrade |
| Still `earned release(s) are waiting` after the deposit | Wallet not bound, or the deposit is still confirming | Bind (Scene 2.2); wait a minute |
| Nothing new to post for Scene 2.4 | The file was deleted, so charges stopped, or all earnings were already paid | Upload a small file and wait two to four minutes |
| Period `RESERVED` but never `posted` | Coordinator wallet out of WBT | `vyomachain balance` on the coordinator wallet; faucet |
| `verifyreceipt epoch latest`: `no epoch has been anchored yet` | Anchoring needs audits older than 30 seconds, then the interval | Wait; check the `[ANCHOR]` lines |
| `verifyreceipt` says MISMATCH with no tampering | A very late-written audit record landed inside an old epoch | Rare. Check the next epoch and say so honestly |
| `too early: the challenge window is still open` | 120 seconds have not passed | Wait for the time it prints |
| `too early: the coordinator posted recently` | 900 seconds of silence have not passed | Wait for the time it prints |
| `chain_escape.sh` says the coordinator already claimed | You were slower than two minutes | Wait for the next `posted` line and run it at once |
| Every number on the console looks wrong | `--mode=demo` missing | Add it |

> A private key pasted into a chat or a public file cannot be taken back. Key files stay on the machine
> that owns them, and `vyomachain` only ever asks for the file's path.

---

## Part 10 — Providers: what changes for them

| Question | Answer |
| --- | --- |
| New commands for providers? | **None.** `vyomachain` and `verifyreceipt` are operator and owner tools. |
| Do they need a wallet, WBT or USDT? | **No.** The coordinator assigns an address and pays the fees. |
| New question when they join? | **No.** |
| Do they run anything different from the provider guide? | **No.** Same `join.sh`, same `provider inspect`, same `provider depart`. |
| How would a provider see money? | Only through the operator, who can show their wallet page on the explorer. |

---

## Part 11 — What is not built, honestly

- Providers do not bring their own wallet. The demo gives them derived (custodial) addresses.
- The wallet sign-up page is plain HTTP on the demo network. It would need TLS anywhere else.
- Data owners deposit with `vyomachain`, not with the `client` program.
- Whitechain has a single sequencer, USDT can be frozen by its issuer, and everything runs on a **test
  network** with coins that have no value. The contracts are unaudited.
- The audit fingerprint proves old records were not changed. It does not prove a record was written;
  the provider's own signature does that.
- Getting a file back still needs a coordinator. The blockchain protects the money and the history,
  not retrieval.
