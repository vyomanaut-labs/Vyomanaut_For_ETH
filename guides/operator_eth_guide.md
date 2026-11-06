# Operator & Data Owner Guide — Whitechain payments (macOS)

This guide adds one thing to `operator_unix_guide.md`: **real USDT payments on the Whitechain
test network**. Read that guide first. Everything in it still works exactly the same. This
page only covers what is new.

Two roles again, and they can be one Mac:

- **Operator** — runs the coordinator, binds wallets, watches money move.
- **Data owner** — puts USDT in, uploads a file, pays for storage.

> **Providers do nothing new.** No wallet, no coins, no new command, no new question at
> sign-up. Their own guide (`provider_unix_guide.md` / `provider_windows_guide.md`) is
> unchanged. Part 10 says this again in one place.

---

## Part 1 — What this adds, in plain words

Before: the coordinator keeps a ledger (a list of who owes whom) and pays providers with
pretend money. The providers have to *trust* the coordinator to pay them.

After: owners can put **real test USDT** into a **contract** (a small program that lives on a
public blockchain and cannot be changed). The coordinator tells the contract "pay these
providers this much". Once told, the contract pays — **even if the coordinator is switched
off afterwards**.

| Word | What it means here |
| --- | --- |
| **Wallet** | An address (`0x…`) plus a secret key file that controls it. Like a bank account number plus a pen that signs. |
| **USDT** | A coin worth one US dollar. Here it is *test* money with no real value. Demo scale: **1 USDT = ₹1,000**. |
| **WBT** | Whitechain's fuel. Every transaction costs a tiny bit of it. It is separate from USDT. |
| **Escrow contract** | The locked box. Owners put USDT in. Only the rules in the contract can take it out. |
| **Period** | One batch of payouts, numbered 1, 2, 3… Each period is posted to the contract once. |
| **Challenge window** | 120 seconds after posting. A separate "guardian" key can cancel a bad period in this time. After it, the payout is final. |
| **Claim** | The step that actually sends USDT to a provider's wallet. **Anyone** can do it. The money always goes to the provider, never to whoever pressed the button. |
| **Refund** | If the coordinator posts nothing for 900 seconds, owners can take their unspent USDT back. |

> The ledger is still the boss. The chain only follows it. If the chain is down, uploads,
> audits and repair carry on, and the payouts catch up when it returns.

---

## Part 2 — Setup (new things only)

### 2.1 What you need on top of the normal operator setup

```bash
cast --version
```

> `cast` is a small tool from Foundry that talks to the blockchain from the terminal. You
> already used it for the first test deposit. If it is missing: `curl -L https://foundry.paradigm.xyz | bash`
> then `foundryup`.

### 2.2 The three wallets

Their key files live in `~/vyoma-keys/` on the operator's Mac. **Never upload, paste or
commit a key file.** Only the addresses below are safe to share.

| Role | Address | Key file | Needs |
| --- | --- | --- | --- |
| Coordinator (pays gas, posts periods) | `0xc86553A4fC45352Bd79F1A45a0c338E598B49888` | `deployer.key` | WBT |
| Data owner (puts USDT in) | `0x7CBEe871deaDF21C3f8506283a2b96544b27FEb1` | `owner.key` | USDT and a little WBT |
| Guardian (can cancel a period) | `0xf4d6D323970cc80DC39461b29A272b34c4767cb9` | `guardian.key` | a few cents of WBT, only if you demo a cancel |

### 2.3 Check the balances

Set these once per terminal:

```bash
export RPC="https://rpc.testnet.whitechain.io"
export USDT="0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7"
export ESCROW="0xBcE841C78c6c40C551fc223Ba9415D4751Af5958"
export COORD_WALLET="0xc86553A4fC45352Bd79F1A45a0c338E598B49888"
export OWNER_WALLET="0x7CBEe871deaDF21C3f8506283a2b96544b27FEb1"
```

Then:

```bash
cast balance "$COORD_WALLET" --rpc-url "$RPC" --ether
cast balance "$OWNER_WALLET" --rpc-url "$RPC" --ether
cast call "$USDT" "balanceOf(address)(uint256)" "$OWNER_WALLET" --rpc-url "$RPC"
```

> The first two are WBT. Anything above `0.01` is plenty for many runs. The third is USDT in
> *millionths* — `110011323` means 110.011323 USDT.
> Out of WBT? The faucet gives 2 WBT a day per GitHub account: <https://faucet.testnet.whitechain.io>

---

## Part 3 — Every command you will use

```bash
# ── set once per terminal (see 2.3 for the first five) ───────────────────
export OWNER_KEY_FILE="$HOME/vyoma-keys/owner.key"
export EXPLORER="https://explorer.testnet.whitechain.io"

# ── a helper to talk to the database (works in zsh and bash) ─────────────
export PGPASSWORD=devpass
db() { psql -h localhost -U vyomanaut_migrator -d vyomanaut_demo "$@"; }

# ── owner puts USDT in: two steps, "allow" then "deposit" ────────────────
cast send "$USDT"   "approve(address,uint256)" "$ESCROW" 1000000 --rpc-url "$RPC" --private-key "$(cat "$OWNER_KEY_FILE")"
cast send "$ESCROW" "deposit(uint256)"         1000000           --rpc-url "$RPC" --private-key "$(cat "$OWNER_KEY_FILE")"

# ── read the contract (free, changes nothing) ────────────────────────────
cast call "$ESCROW" "credit(address)(uint256)" "$OWNER_WALLET" --rpc-url "$RPC"          # owner's unspent USDT inside
cast call "$ESCROW" "lastPeriod()(uint64)" --rpc-url "$RPC"                              # newest period number
cast call "$ESCROW" "periodInfo(uint64)(uint256,uint256,uint64,bool)" <period> --rpc-url "$RPC"
cast call "$ESCROW" "allocationOf(uint64,address)(uint256)" <period> <wallet> --rpc-url "$RPC"
cast call "$ESCROW" "refundableAt()(uint256)" --rpc-url "$RPC"                           # when a refund opens (unix time)

# ── "the coordinator is gone" commands ───────────────────────────────────
cast send "$ESCROW" "claim(uint64,address)" <period> <provider_wallet> --rpc-url "$RPC" --private-key "<any wallet with a little WBT>"
cast send "$ESCROW" "refund(address)" "$OWNER_WALLET" --rpc-url "$RPC" --private-key "<any wallet with a little WBT>"
```

> What the two-step deposit means: the first line says "the escrow may take up to 1 USDT
> from me"; the second line makes it take 1 USDT. `1000000` is 1 USDT because USDT has six
> decimals.
>
> **Do not use `client deposit` in this flow.** That command adds pretend money to the
> ledger. The ledger would then show more than the contract holds, and payouts would stall.

---

## Part 4 — Start the network with Whitechain on

Do Parts 1 to 4.3 of `operator_unix_guide.md` as usual (ZeroTier, CGO flags, Postgres,
observability). Then, in the **same terminal** you will run `up.sh` in:

```bash
export VYOMANAUT_CHAIN_ENABLED=1
export VYOMANAUT_CHAIN_RPC_URL="$RPC"
export VYOMANAUT_CHAIN_PRIVATE_KEY_FILE="$HOME/vyoma-keys/deployer.key"
export VYOMANAUT_CHAIN_ESCROW_ADDRESS="$ESCROW"
export VYOMANAUT_CHAIN_ID=1874
export VYOMANAUT_CHAIN_START_BLOCK="$(cast block-number --rpc-url "$RPC")"
```

> Line by line: *turn it on; where the blockchain is; which key signs for the coordinator;
> which contract; refuse to run on the wrong chain; and where to start reading deposits.*
>
> `START_BLOCK` is "right now". It means the coordinator ignores old deposits from earlier
> runs. **Make your deposit after this line, never before.**

Then start the network exactly like the normal guide. For a practice run on one Mac:

```bash
scripts/demo/down.sh
rm -rf /tmp/vyomanaut-demo
scripts/demo/up.sh --providers 7 --advertise-host "$MY_IP"
```

> `--providers 7` starts seven **pretend-real** providers on this same Mac. They are separate
> programs, not fakes, so the chain flow behaves exactly as with seven laptops. For the real
> rehearsal use `--providers 0` and let machines join (Part 5 of the normal guide).

### 4.1 Check the chain part woke up

```bash
grep CHAIN /tmp/vyomanaut-demo/logs/microservice.log
```

> You want exactly this kind of line, and no `WARNING`:
> `[CHAIN] settlement running: endpoint rpc.testnet.whitechain.io, escrow 0xBcE8…, coordinator wallet 0xc865…`
>
> A line starting `[CHAIN] cannot start yet (retrying)` means it will keep trying — see
> Part 11.

Leave this open in its own window for the rest of the guide:

```bash
tail -f /tmp/vyomanaut-demo/logs/microservice.log | grep CHAIN
```

---

## Part 5 — Tell the coordinator which wallet is whose

The coordinator needs to know which wallet belongs to which owner and provider. For now this
is two small database commands. (A friendlier command is planned; nothing here is hard.)

### 5.1 The owner

Register the owner as in Part 6 of the normal guide. Then:

```bash
db -c "SELECT owner_id, phone_number FROM owners;"
```

Copy the `owner_id`, then:

```bash
db -c "INSERT INTO owner_wallets (owner_id, wallet) VALUES ('<owner_id>', decode(substr('$OWNER_WALLET', 3), 'hex'));"
```

> `substr(…, 3)` chops off the `0x`; `decode` turns the rest into raw bytes. You should see
> `INSERT 0 1`.

### 5.2 The providers

Wait until providers show `ACTIVE` on the console. Then, one command binds **all** of them:

```bash
db -At -c "SELECT provider_id FROM providers WHERE status = 'ACTIVE';" | while read PID; do
  ADDR="$(cast wallet new | awk '/^Address/ {print $2}')"
  db -c "INSERT INTO provider_payout_addresses (provider_id, wallet) VALUES ('$PID', decode(substr('$ADDR', 3), 'hex')) ON CONFLICT (provider_id) DO NOTHING;"
done
```

> What this does: for every active provider it makes a **brand-new demo wallet address** and
> records it as that provider's payout address. We throw the secret key away on purpose. We
> only need the address, so we can watch money arrive on the explorer.
>
> **Be honest about this on stage:** in the demo, these are addresses the operator picked
> for the providers. In a real network each provider would bring their own wallet.
>
> Safe to run again after more machines join — providers already bound are skipped.

See who got what:

```bash
db -c "SELECT p.phone_number, '0x'||encode(a.wallet,'hex') AS wallet FROM provider_payout_addresses a JOIN providers p USING (provider_id) ORDER BY 1;"
```

> A provider with no wallet is not blocked. Its earnings simply wait until you bind one.

---

## Part 6 — The owner puts USDT in

Set the owner's side first (`MSURL`, `OWNER_DIR`, as in the normal guide). Then deposit 1 USDT
with the two `cast send` commands from Part 3.

Each command prints a `transactionHash`. Keep both. Open:

```
https://explorer.testnet.whitechain.io/tx/<transactionHash>
```

> **This is a qualifying link** for the Whitechain prize: a USDT payment made on Whitechain
> in the main flow. Save the link for the README.

Within about 30 seconds the coordinator window shows:

```
[CHAIN] credited 100000 paise to owner <id> for an on-chain deposit of 1000000 micro-units
```

Check from the owner's side:

```bash
"$BIN_DIR/client" balance --mode=demo --microservice-url="$MSURL" --data-dir="$OWNER_DIR"
```

> Expect ₹1,000 (`100000` paise). The wait is two blocks of safety plus the ten-second check
> cycle. Nothing is stuck.

---

## Part 7 — Upload and watch the money move

Upload a plain text file as in Part 6.4 of the normal guide. Then just watch the `[CHAIN]`
window. The journey takes roughly **eight to ten minutes** in the demo, because of the
two-minute charge and release timers and the one-minute hold:

| Step | You will see | What it means |
| --- | --- | --- |
| 1 | `reserved period 1: … releases, … payees` | The coordinator wrote the payout list down, in its own database. |
| 2 | `period 1 posted, claimable at <time>` | The list is now **on the blockchain**. The 120-second window has started. |
| 3 | `period 1 claimed: N payees paid` | USDT was sent to each provider wallet. |

Check the periods:

```bash
db -c "SELECT period, status, total_micro, '0x'||encode(post_tx,'hex') AS post_tx, '0x'||encode(claim_tx,'hex') AS claim_tx FROM chain_periods ORDER BY period;"
```

> `RESERVED → POSTED → CLAIMED`. Paste `post_tx` or `claim_tx` after `…/tx/` in the explorer
> link to see the transaction. On a provider's wallet page, open the **Token transfers** tab
> to see the USDT arrive.

See any provider's USDT balance:

```bash
cast call "$USDT" "balanceOf(address)(uint256)" <provider_wallet> --rpc-url "$RPC"
```

> The provider laptop shows nothing different. Its owner will not notice a thing, which is
> the point.

---

## Part 8 — The big moment: the coordinator disappears

This is the demonstration that matters. It shows: **once a payout is posted, it will be paid
even if we are gone.**

You need a fresh period, so upload another small file and wait for step 2 of Part 7.

**1. The moment you see `period N posted`, stop the coordinator.** You have about two
minutes, because the coordinator would otherwise claim by itself.

```bash
kill "$(head -n1 /tmp/vyomanaut-demo/pids)"
```

> The first line of that file is the coordinator's own process. This stops **only** the
> coordinator, not Postgres and not the providers. Do not use `down.sh` here.

Prove it is gone:

```bash
curl -s -o /dev/null -w "%{http_code}\n" "http://127.0.0.1:8080/api/v1/admin/readiness"
```

> Expect `000` (no answer).

**2. Look up who is owed.** A stranger would read this from the contract's `Allocated`
events on the explorer. For speed, read it from our database:

```bash
export PERIOD=<N>
db -At -c "SELECT '0x'||encode(wallet,'hex'), amount_micro FROM chain_period_payouts WHERE period = $PERIOD;"
```

Confirm the contract itself agrees:

```bash
cast call "$ESCROW" "allocationOf(uint64,address)(uint256)" "$PERIOD" <provider_wallet> --rpc-url "$RPC"
```

**3. Wait for the window to end** (the time printed in the `posted` line). Trying early is
harmless; it just fails with `WindowStillOpen`.

**4. A bystander claims.** Any wallet with a little WBT works. We use the owner's, because to
the provider it is a stranger:

```bash
cast send "$ESCROW" "claim(uint64,address)" "$PERIOD" <provider_wallet> --rpc-url "$RPC" --private-key "$(cat "$OWNER_KEY_FILE")"
```

Now show the proof:

```bash
cast call "$USDT" "balanceOf(address)(uint256)" <provider_wallet> --rpc-url "$RPC"
```

> The provider got its USDT. On the explorer, the `Claimed` event lists **two** addresses:
> the *account* (provider, who got the money) and the *caller* (the bystander, who paid the
> fee). They are different. That is the whole story in one screen.

**Be straight about the limit:** only money that was **already posted** is protected. Earnings
the coordinator had not posted yet when it died are not. Say this before anyone asks.

---

## Part 9 — The owner gets unspent money back

The owner can also leave without us. If the coordinator posts nothing for **900 seconds**
(fifteen minutes), anyone can send an owner's unspent USDT back to that owner.

```bash
cast call "$ESCROW" "refundableAt()(uint256)" --rpc-url "$RPC"
date -r <that number>
cast call "$ESCROW" "credit(address)(uint256)" "$OWNER_WALLET" --rpc-url "$RPC"
```

> The first line is the unix time the refund opens, the second turns it into a clock time,
> the third shows how much is waiting. Every time the coordinator posts a period, the 900
> seconds start again. If the owner spent nothing, deposit again so there is something left
> to return.

When the time has passed:

```bash
cast send "$ESCROW" "refund(address)" "$OWNER_WALLET" --rpc-url "$RPC" --private-key "$(cat "$OWNER_KEY_FILE")"
```

> Too early gives `CoordinatorStillActive`. That is the contract working, not a bug. USDT
> arrives back in the owner's wallet. Only the unspent part comes back; what was already
> posted for providers stays theirs.

---

## Part 10 — Providers: what changes for them

| Question | Answer |
| --- | --- |
| New commands for providers? | **None.** |
| Do they need a wallet, WBT or USDT? | **No.** The coordinator pays the gas fees. |
| New question when they join? | **No.** |
| Do they run anything different from the provider guide? | **No.** Same `join.sh`, same `provider inspect`, same `provider depart`. |
| How would a provider see money? | Only through the operator, who can show their wallet page on the explorer. |
| What does the operator do per provider? | Runs the one bind command in 5.2 once the provider is `ACTIVE`. |

> Providers' wallets are chosen by the operator in this demo. A provider does not even know
> it exists unless you show them.

---

## Part 11 — Running it again, and when something goes wrong

### Running again

`up.sh` wipes the database every time. The blockchain remembers everything.

- Always set `VYOMANAUT_CHAIN_START_BLOCK` fresh (Part 4), so old deposits are not counted
  again.
- Period numbers keep going up across runs (period 7, 8…). The coordinator handles this on
  its own.
- Wallets must be bound again after every `up.sh` (Part 5). The database was wiped.
- Unspent USDT from an earlier run stays inside the contract. Take it back with Part 9.
- There is **no limit** on how many times you can run this. The real limits are fuel and
  coins: WBT for fees (about a thousandth of a WBT per transaction, 2 WBT a day from the
  faucet) and USDT (110 in the owner wallet; a demo run uses very little).

### When something goes wrong

| Symptom | Cause | Fix |
| --- | --- | --- |
| No `[CHAIN] settlement running` line | The `export` lines were set in a different terminal from `up.sh` | `down.sh`, export again in the same terminal, `up.sh` |
| `[CHAIN] cannot start yet (retrying)` | Node unreachable, or the key file is readable by others | Check internet; `chmod 600 ~/vyoma-keys/deployer.key` |
| `WARNING: this key … is not the escrow's coordinator` | Wrong key file | Use `deployer.key` |
| `… is required when VYOMANAUT_CHAIN_ENABLED=1` | A variable is missing | Re-run the exports in Part 4 |
| Deposit made but no `credited` line | Owner wallet not bound (5.1), or the deposit was made before `START_BLOCK` was set | Bind it; the credit appears by itself. If it was too early, deposit again |
| `releases are waiting but owners' charges or on-chain credit cannot fund them yet` | Owner has no USDT inside the contract | Deposit (Part 6) |
| Payout never reserved | The provider has no wallet bound | Part 5.2 |
| Period `RESERVED` but never `posted` | Coordinator wallet out of WBT | Check balance (2.3), use the faucet |
| `cast send` says `connection` or empty URL | `$RPC` is not set in this terminal | Set it again (2.3) |
| `WindowStillOpen` | 120 seconds have not passed | Wait for the time in the `posted` line |
| `CoordinatorStillActive` | 900 seconds of silence have not passed | Wait; check `refundableAt` |
| `NothingToClaim` | Already claimed, or wrong wallet | Check `allocationOf` |
| Coordinator claimed before you could stop it | You were slower than two minutes | Upload again and be quicker; pre-type the `kill` line |
| Every number on the console looks wrong | `--mode=demo` missing | Add it |

> If you edit anything and are unsure, ask before running a command that has a private key
> in it. A key pasted into a chat or a public file cannot be taken back.

---

## Part 12 — What is not built yet

Be honest with anyone who asks:

- Binding wallets is a database command, not a button. A `client` command is planned.
- The data owner deposits with `cast`, not with the `client` program.
- Audit history is not yet published on the chain. The contract for it is deployed and
  verified; the coordinator does not use it yet.
- A provider's payout address is chosen by the operator. Providers do not bring a wallet.
- Everything runs on a **test network** with coins that have no value, and the contracts
  are unaudited.
