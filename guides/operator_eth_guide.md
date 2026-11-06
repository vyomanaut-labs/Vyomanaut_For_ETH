# Operator & Data Owner Guide — Whitechain payments (macOS)

This guide adds one thing to `operator_unix_guide.md`: **real USDT payments on the Whitechain
test network, and a public fingerprint of the audit history**. Read that guide first.
Everything in it still works exactly the same. This page only covers what is new.

Two roles again, and they can be one Mac:

- **Operator** — runs the coordinator, watches money move, runs the demos.
- **Data owner** — puts USDT in, uploads a file, pays for storage.

> **Providers do nothing new.** No wallet, no coins, no new command, no new question at
> sign-up. Their own guide (`provider_unix_guide.md` / `provider_windows_guide.md`) is
> unchanged. Part 10 says this again in one place.

---

## Part 1 — What this adds, in plain words

Before: the coordinator keeps a ledger (a list of who owes whom) and pays providers with
pretend money. Providers have to *trust* the coordinator to pay them, and to keep an honest
record of their audits.

After, three new things:

1. **Real test USDT.** Owners put USDT into a **contract** (a small program on a public
   blockchain that nobody can change). The coordinator tells the contract "pay these
   providers this much". Once told, the contract pays — **even if the coordinator is
   switched off afterwards**.
2. **A fingerprint of the audit history.** Every couple of minutes the coordinator writes one
   short fingerprint of all new audit records to the blockchain. If anyone later changes an
   old record, the fingerprint no longer matches, and a checker shows it.
3. **Escape hatches.** Anyone can finish a posted payout. An owner can take back unspent
   money after the coordinator has been silent for fifteen minutes.

| Word | What it means here |
| --- | --- |
| **Wallet** | An address (`0x…`) plus a secret key file that controls it. Like a bank account number plus a pen that signs. |
| **USDT** | A coin worth one US dollar. Here it is *test* money with no real value. Demo scale: **1 USDT = ₹1,000**. |
| **WBT** | Whitechain's fuel. Every transaction costs a tiny bit of it. It is separate from USDT. |
| **Escrow contract** | The locked box. Owners put USDT in. Only the rules in the contract can take it out. |
| **Period** | One batch of payouts, numbered 1, 2, 3… Each period is posted to the contract once. |
| **Challenge window** | 120 seconds after posting. A separate "guardian" key can cancel a bad period in this time. After it, the payout is final. |
| **Claim** | The step that actually sends USDT to a provider's wallet. **Anyone** can do it. The money always goes to the provider, never to whoever pressed the button. |
| **Refund** | If the coordinator posts nothing for 900 seconds, anyone can send an owner's unspent USDT back to that owner. |
| **Epoch** | One batch of audit records, summed into one fingerprint (a Merkle root) and written to the chain. |

> The ledger is still the boss. The chain only follows it. If the chain is down, uploads,
> audits and repair carry on, and payouts and fingerprints catch up when it returns.

---

## Part 2 — Setup (new things only)

### 2.1 Tools

Nothing to install beyond the normal operator setup. The new commands are small Go programs
built from the repo:

```bash
go build -o "$BIN_DIR/vyomachain"    ./cmd/vyomachain       # the owner / escape-hatch tool
go build -o "$BIN_DIR/verifyreceipt" ./cmd/verifyreceipt    # the audit-history checker
```

> `$BIN_DIR` comes from `source /tmp/vyomanaut-demo/env` (Part 4 of the normal guide). The two
> demo scripts below build these for you, so you only need this if you run them by hand.
> Neither needs RocksDB.
>
> `cast` (Foundry) still works for reading the chain, but you no longer need it.

### 2.2 The three wallets

Their key files live in `~/vyoma-keys/` on the operator's Mac. **Never upload, paste or
commit a key file.** Only the addresses below are safe to share.

| Role | Address | Key file | Needs |
| --- | --- | --- | --- |
| Coordinator (pays gas, posts periods) | `0xc86553A4fC45352Bd79F1A45a0c338E598B49888` | `deployer.key` | WBT |
| Data owner (puts USDT in) | `0x7CBEe871deaDF21C3f8506283a2b96544b27FEb1` | `owner.key` | USDT and a little WBT |
| Guardian (can cancel a period) | `0xf4d6D323970cc80DC39461b29A272b34c4767cb9` | `guardian.key` | a few cents of WBT, only if you demo a cancel |

Check a wallet's gas, USDT and unspent escrow credit:

```bash
"$BIN_DIR/vyomachain" balance --address 0xc86553A4fC45352Bd79F1A45a0c338E598B49888
```

> Needs the chain variables from Part 4 in the same terminal. `gas (WBT)` above `0.01` is plenty
> for many runs. Out of WBT? The faucet gives 2 WBT a day per GitHub account:
> <https://faucet.testnet.whitechain.io>

---

## Part 3 — Every command you will use

```bash
# ── operator: once per terminal, before starting the network ─────────────
eval "$(scripts/demo/chain_env.sh)"          # turns the chain features on for the coordinator

# ── operator: the demos ──────────────────────────────────────────────────
scripts/demo/chain_tamper.sh                 # alter one audit record; the check catches it
scripts/demo/chain_tamper.sh --restore       # put it back
scripts/demo/chain_escape.sh                 # stop the coordinator; a stranger gets a provider paid

# ── operator: check the audit history against the chain ──────────────────
"$BIN_DIR/verifyreceipt" list
"$BIN_DIR/verifyreceipt" epoch latest
"$BIN_DIR/verifyreceipt" receipt <receipt_id>

# ── data owner ───────────────────────────────────────────────────────────
vyomachain wallet bind    --api "http://<operator>:8090" --data-dir "$OWNER_DIR" --key-file ~/vyoma-keys/owner.key
vyomachain deposit        --amount 1 --key-file ~/vyoma-keys/owner.key
vyomachain balance        --address <wallet>

# ── anyone, even with no coordinator ─────────────────────────────────────
vyomachain period 3 --account <provider_wallet>
vyomachain claim    --period 3 --account <provider_wallet> --key-file <any wallet with a little WBT>
vyomachain refund   --owner <owner_wallet>                 --key-file <any wallet with a little WBT>
```

> **Do not use `client deposit` in this flow.** That command adds pretend money to the ledger.
> The ledger would then show more than the contract holds, and payouts would stall. Use
> `vyomachain deposit` instead.
>
> `vyomachain` finds the chain from environment variables (`VYOMANAUT_CHAIN_RPC_URL`, and
> either `VYOMANAUT_CHAIN_ESCROW_ADDRESS` or `VYOMANAUT_CHAIN_API_URL`). `chain_env.sh` sets the
> first two, so on the operator's Mac it just works. On a separate owner machine see 6.1.

---

## Part 4 — Start the network with the chain on

Do Parts 1 to 4.3 of `operator_unix_guide.md` as usual (ZeroTier, CGO flags, Postgres,
observability). Then, in the **same terminal** you will run `up.sh` in:

```bash
eval "$(scripts/demo/chain_env.sh)"
```

> It prints what it set. In plain words: *turn the chain on; where the blockchain is; which key
> signs for the coordinator; which contracts; refuse to run on the wrong chain; where to start
> reading deposits; turn audit anchoring on (every 2 minutes); open a small sign-up page for
> owner wallets on port 8090; give providers a payout address automatically.*
>
> `START_BLOCK` is "right now", so old deposits from earlier runs are ignored. **Make your
> deposit after this line, never before.**
>
> It never reads or prints a key, only the path. If `~/vyoma-keys/deployer.key` is somewhere
> else, `export VYOMANAUT_CHAIN_PRIVATE_KEY_FILE=<path>` first.

Then start the network exactly like the normal guide. For a practice run on one Mac:

```bash
scripts/demo/down.sh
rm -rf /tmp/vyomanaut-demo
scripts/demo/up.sh --providers 7 --advertise-host "$MY_IP" --expose-metrics
```

> `--providers 7` starts seven providers on this same Mac. They are separate programs, not
> fakes, so the chain flow behaves exactly as with seven laptops. For the real rehearsal use
> `--providers 0` and let machines join (Part 5 of the normal guide). `--expose-metrics` lets
> Grafana show the chain dashboard (Part 11).

### 4.1 Check everything woke up

```bash
grep -E "CHAIN|ANCHOR|EXTENSION" /tmp/vyomanaut-demo/logs/microservice.log
```

> You want these lines, and no `WARNING`:
>
> - `[EXTENSION] chain started`
> - `[CHAIN] demo payout wallets: providers get an address derived from the coordinator's key (custodial)`
> - `[CHAIN] settlement running: endpoint rpc.testnet.whitechain.io, escrow 0xBcE8…, coordinator wallet 0xc865…`
> - `[ANCHOR] anchoring running: anchor contract 0x1b2b…, every 2m0s`
> - `[CHAIN] chain API listening on :8090 …`
> - `[CHAIN] assigned demo payout wallets to N provider(s)` (once providers appear)
>
> `[CHAIN] cannot start yet (retrying)` means it keeps trying — see Part 12.

Leave this open in its own window for the rest of the guide:

```bash
tail -f /tmp/vyomanaut-demo/logs/microservice.log | grep -E "CHAIN|ANCHOR"
```

---

## Part 5 — Providers get wallets by themselves

Nothing to do. As each provider appears, the coordinator gives it a **payout address** it
derives from its own secret. See who got what:

```bash
export PGPASSWORD=devpass
psql -h localhost -U vyomanaut_migrator -d vyomanaut_demo -c "SELECT p.phone_number, '0x'||encode(a.wallet,'hex') AS wallet FROM provider_payout_addresses a JOIN providers p USING (provider_id) ORDER BY 1;"
```

> **Be honest about this on stage:** in the demo, these wallets are **custodial**. The
> coordinator can reproduce their keys, so the provider never touches a wallet. A real provider
> would bring their own address. The money still arrives at that address on the blockchain,
> and you can open it on the explorer.
>
> A provider with no wallet is not blocked. Its earnings simply wait until it has one.

---

## Part 6 — The owner puts USDT in

### 6.1 Set up (owner's side)

On the operator's Mac, the variables from Part 4 are already set. On a **separate owner
machine**:

```bash
export VYOMANAUT_CHAIN_RPC_URL="https://rpc.testnet.whitechain.io"
export VYOMANAUT_CHAIN_API_URL="http://<operator's 10.x address>:8090"
go build -o vyomachain ./cmd/vyomachain
```

> The second line is the small sign-up page from Part 4. It tells `vyomachain` which contracts
> the coordinator uses, so the owner needs no addresses.

### 6.2 Register, then bind a wallet

Register the owner as in Part 6 of the normal guide. Then connect a wallet to that owner:

```bash
"$BIN_DIR/vyomachain" wallet bind --api "http://$MY_IP:8090" --data-dir "$OWNER_DIR" --key-file ~/vyoma-keys/owner.key
```

> What this does: it signs a short sentence with the wallet — "this owner, this wallet, this
> chain" — to prove the owner really holds the wallet, and sends it with the owner's normal
> login. The coordinator checks both. A stranger cannot attach a wallet to someone else's
> account, and nobody can claim a wallet they do not control. You should see `bound: 0x7CBE…`.
>
> Want a fresh wallet instead? `vyomachain wallet new --out ~/vyoma-keys/me.key` makes one (it
> never overwrites a key). It then needs USDT and a little WBT before it can deposit.
>
> One wallet per owner, and a wallet belongs to one owner. A second, different wallet for the
> same owner is refused on purpose.

### 6.3 Deposit

```bash
"$BIN_DIR/vyomachain" deposit --amount 1 --key-file ~/vyoma-keys/owner.key
```

> It checks the wallet holds enough, asks the USDT to allow the escrow to take it (only the
> first time), then deposits. It prints two links. Open the `deposited` one:
> `https://explorer.testnet.whitechain.io/tx/0x…` — **this is a qualifying link** for the
> Whitechain prize: a USDT payment on Whitechain in the main flow. Save it for the README.
>
> `1` USDT is `₹1,000` at demo scale. Too little USDT stops it before any fee is spent.

Within about 30 seconds the coordinator window shows:

```
[CHAIN] credited 100000 paise to owner <id> for an on-chain deposit of 1000000 micro-units
```

```bash
"$BIN_DIR/client" balance --mode=demo --microservice-url="$MSURL" --data-dir="$OWNER_DIR"
```

> Expect ₹1,000. The wait is two blocks of safety plus the ten-second check cycle.

---

## Part 7 — Upload and watch the money move

Upload a plain text file as in Part 6.4 of the normal guide. Then watch the `[CHAIN]` window.
The journey takes roughly **eight to ten minutes** in the demo, because of the two-minute
charge and release timers and the one-minute hold:

| Step | You will see | What it means |
| --- | --- | --- |
| 1 | `reserved period 1: … releases, … payees` | The coordinator wrote the payout list down, in its own database. |
| 2 | `period 1 posted, claimable at <time>` | The list is now **on the blockchain**. The 120-second window has started. |
| 3 | `period 1 claimed: N payees paid` | USDT was sent to each provider wallet. |

```bash
"$BIN_DIR/vyomachain" period 1
"$BIN_DIR/vyomachain" balance --address <a provider wallet from Part 5>
```

> `period 1` shows its total, what is still unclaimed, and whether the window is open. On a
> provider's wallet page on the explorer, open **Token transfers** to see the USDT arrive.
> The provider laptop shows nothing different. Its owner will not notice a thing, which is the
> point.

---

## Part 8 — The audit fingerprint, and catching a cheat

While audits run, every two minutes you will see:

```
[ANCHOR] reserved epoch 1: 142 receipts
[ANCHOR] epoch 1 anchored: 142 receipts, root 6e4a19eb…
```

Check the database against the chain:

```bash
"$BIN_DIR/verifyreceipt" list
"$BIN_DIR/verifyreceipt" epoch latest
```

> `list` shows every epoch the coordinator wrote. `epoch latest` rebuilds the fingerprint from the
> database and compares it with the one **read from the blockchain**, never through the
> coordinator. You want `RESULT : MATCH`. It also says how final the write is: *soft* means
> the Whitechain operator confirmed it; *finalized* means it has settled on Ethereum.
>
> It needs the chain variables from Part 4 and the database password:
> `export PGUSER=vyomanaut_app PGPASSWORD=devpass` first (`chain_tamper.sh` does this for you).

### 8.1 The tamper demo

```bash
scripts/demo/chain_tamper.sh
```

> Three steps, printed one after another: the honest history **matches**; then one stored audit
> record is changed the way an operator with database access could; then the same check now says
> `RESULT : MISMATCH`. The script exits only when it has proven the change is caught.
>
> Put it back with `scripts/demo/chain_tamper.sh --restore`, so you can repeat the demo.
>
> Be straight about what this proves: that records **already anchored** cannot be quietly
> changed. It does not prove a record was ever written. That is what each provider's own
> signature on a PASS is for.

---

## Part 9 — The big moment: the coordinator disappears

This shows: **once a payout is posted, it will be paid even if we are gone.**

Upload another small file and wait for `period N posted` in the `[CHAIN]` window. Then, within
about two minutes (the coordinator would otherwise claim by itself):

```bash
scripts/demo/chain_escape.sh
```

> It does the whole demo, narrating each step:
>
> 1. **Stops only the coordinator.** Not Postgres, not the providers.
> 2. **Waits for the 120-second window to end.**
> 3. **A stranger claims.** It uses `~/vyoma-keys/owner.key` as "any wallet with a little WBT"
>    (override with `ESCAPE_KEY_FILE`). The money goes to the providers' wallets, not to the
>    stranger.
> 4. **Shows each provider's USDT balance.**
>
> Try it without doing anything first: `scripts/demo/chain_escape.sh --dry`.
>
> On the explorer, the `Claimed` event lists **two** addresses: the *account* (the provider,
> who got the money) and the *caller* (the stranger, who paid the small fee). They differ. That
> is the whole story on one screen.

By hand, the same thing:

```bash
kill "$(head -n1 /tmp/vyomanaut-demo/pids)"                       # stops only the coordinator
"$BIN_DIR/vyomachain" period <N> --account <provider_wallet>      # until it says "claimable now"
"$BIN_DIR/vyomachain" claim --period <N> --account <provider_wallet> --key-file ~/vyoma-keys/owner.key
```

**Be straight about the limit:** only money that was **already posted** is protected. Earnings
the coordinator had not posted yet when it died are not. Say this before anyone asks.

---

## Part 10 — The owner gets unspent money back

If the coordinator posts nothing for **900 seconds** (fifteen minutes), anyone can send an
owner's unspent USDT back to that owner.

```bash
"$BIN_DIR/vyomachain" balance --address <owner_wallet>      # "escrow credit" is what is waiting
"$BIN_DIR/vyomachain" refund --owner <owner_wallet> --key-file ~/vyoma-keys/owner.key
```

> Too early, it says when refunds open (`refunds open at 14:42:10 UTC`) — that is the contract
> working, not a bug. Every time the coordinator posts a period, the 900 seconds start again.
> Only the unspent part comes back; what was already posted for providers stays theirs. If the
> owner spent nothing, deposit again first so there is something left to return.

### Providers: what changes for them

| Question | Answer |
| --- | --- |
| New commands for providers? | **None.** `vyomachain` and `verifyreceipt` are operator and owner tools. |
| Do they need a wallet, WBT or USDT? | **No.** The coordinator assigns an address and pays the fees. |
| New question when they join? | **No.** |
| Do they run anything different from the provider guide? | **No.** Same `join.sh`, same `provider inspect`, same `provider depart`. |
| How would a provider see money? | Only through the operator, who can show their wallet page on the explorer. |

---

## Part 11 — Watching it on the dashboard

With `--expose-metrics` on `up.sh` and the coordinator's address in
`deployments/observability/targets/microservice.json` (Part 4.3 of the normal guide), Grafana
shows a second dashboard next to the main one:

> Dashboards → **Vyomanaut** folder → **"Vyomanaut — Whitechain settlement and audit anchoring"**

| Panel | Healthy looks like |
| --- | --- |
| USDT deposited by owners / paid to providers | Climb after Parts 6 and 7 |
| Payout periods by state | `reserved → posted → claimed` appear in order |
| Releases waiting for a wallet | `0`. Above `0` means a provider has no wallet yet |
| Newest audit epoch anchored | Rises every interval |
| Chain step errors | Flat. A few in a row during a network blip is normal; a steady stream means the node is unreachable or the coordinator wallet is out of WBT |

---

## Part 12 — Running it again, and when something goes wrong

### Running again

`up.sh` wipes the database every time. The blockchain remembers everything.

- Run `eval "$(scripts/demo/chain_env.sh)"` again, so `START_BLOCK` is fresh and old deposits are
  not counted again.
- Period and epoch numbers keep going up across runs. The coordinator handles this on its own.
- Provider wallets are re-assigned automatically. The owner must **bind again** (6.2): the
  database was wiped.
- Unspent USDT from an earlier run stays inside the contract. Take it back with Part 10.
- There is **no limit** on how many times you can run this. The real limits are fuel and coins:
  WBT for fees (about a thousandth of a WBT per transaction, 2 WBT a day from the faucet) and
  USDT (110 in the owner wallet; a demo run uses very little).

### When something goes wrong

| Symptom | Cause | Fix |
| --- | --- | --- |
| No `[CHAIN]` lines at all | `chain_env.sh` was run in a different terminal from `up.sh` | `down.sh`, `eval` again in the same terminal, `up.sh` |
| `[CHAIN] cannot start yet (retrying)` | Node unreachable, or the key file is readable by others | Check internet; `chmod 600 ~/vyoma-keys/deployer.key` |
| `WARNING: this key … is not the escrow's coordinator` | Wrong key file | Use `deployer.key` |
| `[ANCHOR] … is not the anchor's coordinator` | Wrong key file | Same |
| `vyomachain wallet bind` says `cannot reach the coordinator's chain API` | Wrong address, or the coordinator was not started with the chain on | Check `--api`; look for the `chain API listening` line |
| `wallet bind` says `refused (unauthorized)` | The owner's login expired (24 hours) | `client recover` or register again, then bind |
| `wallet bind` says `already_bound` | That owner has a different wallet bound | Use that wallet, or register a new owner |
| `wallet bind` says `wallet_taken` | Another owner already uses that wallet | Use a different wallet |
| Deposit made but no `credited` line | Wallet not bound (6.2), or the deposit was made before `chain_env.sh` ran | Bind it; the credit appears by itself. If too early, deposit again |
| `vyomachain deposit` says the wallet holds less USDT | Not enough test USDT | Check `vyomachain balance`; swap on YetiTrade |
| `releases are waiting but owners' charges or on-chain credit cannot fund them yet` | Owner has no USDT inside the contract | Deposit (6.3) |
| Payout never reserved | The provider has no wallet yet | Wait ten seconds; check Part 5 |
| Period `RESERVED` but never `posted` | Coordinator wallet out of WBT | `vyomachain balance` on the coordinator wallet; faucet |
| `verifyreceipt epoch latest` says `no epoch has been anchored yet` | Anchoring needs audits older than 30 seconds, then the interval | Wait; check the `[ANCHOR]` lines |
| `verifyreceipt` says MISMATCH with no tampering | A very late-written audit record landed inside an old epoch | Rare. Re-run with the next epoch; say so honestly |
| `too early: the challenge window is still open` | 120 seconds have not passed | Wait for the time it prints |
| `too early: the coordinator posted recently` | 900 seconds of silence have not passed | Wait for the time it prints |
| `nothing to claim … already paid` | Already claimed, or wrong wallet | `vyomachain period N --account …` |
| `chain_escape.sh` says the coordinator already claimed | You were slower than two minutes | Upload again and run it the moment `posted` appears |
| Every number on the console looks wrong | `--mode=demo` missing | Add it |

> A private key pasted into a chat or a public file cannot be taken back. Key files stay on the
> machine that owns them, and `vyomachain` only ever asks for the file's path.

---

## Part 13 — What is not built, honestly

- Providers do not bring their own wallet. The demo gives them derived (custodial) addresses.
- The wallet-sign-up page is plain HTTP on the demo network. It would need TLS anywhere else.
- Data owners deposit with `vyomachain`, not with the `client` program.
- Whitechain has a single sequencer, USDT can be frozen by its issuer, and everything runs on a
  **test network** with coins that have no value. The contracts are unaudited.
- The audit fingerprint proves old records were not changed. It does not prove a record was
  written; the provider's own signature does that.
