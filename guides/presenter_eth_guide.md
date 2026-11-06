# Presenter Guide — Vyomanaut at ETHGlobal Mumbai

Read this the night before. Everything is in plain words. If you can say the one-minute story
below without looking, you are ready.

We enter **three partner prizes** (the most ETHGlobal allows), all on the **Continuity track**
because Vyomanaut existed before the hackathon:

| Partner | Prize we enter | Money | One question the judges ask |
| --- | --- | --- | --- |
| **Whitechain** | Continuity · Bring Your Product to Whitechain | $3,000 | Did you make real progress this weekend, and is a real USDT payment on Whitechain at the heart of it? |
| **Ethereum Foundation** | Best CROPS Upgrade to an Existing Project | $1,500 | Is the new property *structural* (it works without you), and do the other three still hold? |
| **Arkiv** | Build on Arkiv (existing projects) | $4,000 | Why Arkiv and not our database, and does something a user sees read from Arkiv? |

> Rules worth knowing: the EF allows one prize per project. Arkiv's tracks count as one partner
> prize. Judges score only the **new work** done this weekend, so always show *before* and
> *after*. We are **not** entering the Arkiv + Swarm bonus.

> **Update the status box before every presentation.** Anything not ticked must not be shown as
> working.

## Status box

Tick a row only when you have seen its log line or explorer link yourself. Rows ticked here were
seen in the run of **Fri 6 Nov 2026** (coordinator log and screenshots).

### Whitechain (money and fingerprints)

| Piece | Tick when seen live |
| --- | --- |
| Escrow and anchor contracts deployed and verified on Whitechain Sepolia | [ ] open both explorer pages once |
| Providers get a payout address automatically (custodial in the demo) | [x] `[CHAIN] assigned demo payout wallets to 7 provider(s)` |
| Audit fingerprint anchored every 2 minutes | [x] `[ANCHOR] epoch N anchored`, epochs 114 to 139 |
| `verifyreceipt` MATCH | [x] epoch 139, 804 receipts |
| `chain_tamper.sh` → MISMATCH | [x] epochs 146 and 148: MATCH, then MISMATCH naming the receipt, then restored to MATCH |
| Providers' earnings are released every tick (patch 0017) | [x] 112 of 112 audit periods released, 9 `RELEASE` rows (before 0017 this was always 0) |
| Owner binds a wallet and deposits real test USDT → `credited` | [x] wallet `0x7CBE…FEb1` bound; 1 USDT deposited (approve and deposit explorer links). Still grep the `credited` log line |
| Payout `reserved` → `posted` → `claimed` | [x] `vyomachain period 1`: fully claimed, 0.10946 USDT; a provider wallet now holds 0.01703 USDT. Owner's unspent credit 0.89054 = 1 − 0.10946 |
| "Coordinator disappears, a stranger pays the provider" (`chain_escape.sh`) | [ ] first try was too late: the coordinator had already claimed |
| Owner gets unspent USDT back (`vyomachain refund`, patch 0018) | [x] "too early, refunds open at 11:51:48 UTC", then "returned the unspent credit" with an explorer link. Still grep `[CHAIN] debited … for an on-chain refund` |
| Grafana chain dashboard | [ ] |

### Arkiv (readable audit history)

| Piece | Tick when seen live |
| --- | --- |
| Mirror runs inside the same coordinator as the chain features | [x] `[ARKIV] mirror running … wallet 0x1044…27B9` |
| Audit batches published, one per provider per 2-minute period | [x] many `audit_batch … published: entity 0x…`; explorer shows 199 transactions from our wallet |
| Arkiv trouble does not disturb anything else | [x] 14:40 to 14:54 the key was wrong (401) while anchors carried on |
| File manifest published after an upload | [x] `file_manifest 01a1b07e… published: entity 0x0135c4aa…` |
| File tombstone published after a delete | [ ] |
| `arkivcheck audits` green, then `arkiv_tamper.sh` red | [ ] green seen (5 verified); red not yet: the script tampered the wrong slice (fixed by patch 0021) |
| `arkivcheck public` (no database, anyone can run it) | [x] 20 verified, 0 failed |
| Arkiv dashboard in Grafana | [ ] |

---

## 1. The one-minute story

Say this in your own words:

> Vyomanaut is cloud storage on ordinary computers instead of a big data centre. People lend
> spare disk space. Files are locked with the owner's key before they leave, so the computers
> only hold noise.
>
> There were two honest weaknesses. **Providers have to trust us to pay them**, and **they have
> to trust our audit records**, because we keep the only copy. If we vanish, their earnings
> vanish. If we edit a record, nobody can tell.
>
> At this hackathon we fixed both. Owners put test USDT into a contract on Whitechain. When we
> say "pay these providers", the contract pays, and once we have said it we cannot take it back
> or stop it by disappearing. And every audit result is now also written to Arkiv, a public
> database we do not control, where anyone can read it and check it against our records.
>
> Storage itself still runs off-chain, because that is fast and cheap. The chains do only what
> they are better at: paying people when the operator will not, and remembering what we cannot
> quietly change.

**The line to remember:**

> A log can prove what you are owed. Only a chain can pay you when we won't.

**One line per partner** (use the one that matches whoever is listening):

- **Whitechain:** "A real USDT payment reaches a provider even after the operator has gone."
- **Ethereum Foundation:** "Censorship resistance that is structural: it works without us."
- **Arkiv:** "Our audit history lives where we cannot edit it, and anyone can query it."

---

## 2. What we added

**Money (Whitechain)**

1. **A locked box (the escrow contract).** Owners put USDT in. Only its fixed rules can take it
   out. We cannot withdraw it and nobody can change the rules.
2. **A bridge.** The coordinator watches the box. When an owner deposits, it credits their normal
   account. When providers earn, it tells the box who to pay.
3. **A safety window.** A separate "guardian" key can cancel a payout list within two minutes of
   its being posted. After that it is final.
4. **An exit door.** Posted payouts can be claimed by *anyone*, and the money always goes to the
   provider. If we are silent for fifteen minutes, owners can take back what they have not spent.
5. **A fingerprint of the audit history.** Every two minutes we write one short fingerprint of
   all new audit records to Whitechain. Change an old record and the fingerprint no longer
   matches.

**History (Arkiv)**

6. **A readable copy of the audit history.** Each provider's audit receipts for each 2-minute
   period are written to Arkiv as one signed entity. It is public, queryable by attribute, and
   anyone can recompute the check without our database.
7. **A public record of each file.** When a file is registered, a small manifest (hashes and
   counts only) is written to Arkiv. Deleting a file writes a separate "tombstone".
8. **A verifier, `arkivcheck`.** It reads Arkiv directly, checks every entity on its own, and
   compares with our database. Edit a database row and its batch turns red.

What we did **not** change: how files are stored, encrypted, checked, repaired or priced.
Providers' computers run the same program as before. **No wallet, no coins, nothing new for
them.** Neither chain is needed for the network to run, and each feature is off unless switched on.

### Pictures to use

> **Whitechain:** a vending machine for salaries. The owner fills it with coins. We press a
> button that says "Provider A gets 3, Provider B gets 2". After two minutes the machine hands
> out exactly that, even if the shop is closed.
>
> **Arkiv:** a glass notice board outside the shop. Every audit result is pinned up the moment
> it is final. We can read it too, but anyone walking past can check that our private ledger
> still says the same thing.

---

## 3. Before and during (needed for the README and the forms)

| | Before the hackathon (V2, up to `086be48`) | Built during ETHGlobal |
| --- | --- | --- |
| Money | Ledger in Postgres, pretend payments | Real test-USDT escrow on Whitechain Sepolia |
| If the operator vanishes | Providers lose unpaid earnings | Posted payouts stay claimable; owners can reclaim unspent deposits |
| Who can move the money | The operator | Fixed contract rules, plus a veto key for two minutes |
| Audit history | Only in our database; we could rewrite it silently | Fingerprint on Whitechain every 2 minutes, plus signed copies on Arkiv |
| Who can check the history | Only us | Anyone, with `verifyreceipt` or `arkivcheck`, without our database |
| Files | Registered privately | A public, hash-only manifest on Arkiv (no owner, no name, no chunk list) |
| Providers | Join, store, get paid by trust | **Identical.** Nothing new for them |
| Everything else | Storage, audits, repair, ZeroTier network | Unchanged |

Starting point proof: commit `086be48` (29 Sep 2026), tag `pre-ethglobal-mumbai`.

---

## 4. What each partner must see

### Whitechain: show the money

They want a **real USDT payment on Whitechain in the main flow**, an explorer link in the README,
and a demo that shows **each party** (owner, provider, operator, and the stranger).

| Show | Proof |
| --- | --- |
| Owner deposits test USDT (`0xcedf…55a7`) | the `deposited` explorer link |
| Coordinator posts a payout list | `[CHAIN] period N posted` |
| A provider is paid | the `Claimed` event: **caller ≠ account** |
| We disappear and a stranger still pays the provider | `chain_escape.sh` |
| Why a chain at all | "A bank rail cannot promise that a posted payout survives the operator." |

Paste at least one **USDT deposit link and one claim link** into the README. Without a USDT payment
the Whitechain entry does not qualify.

### Ethereum Foundation: show the moment it matters

The judges' words: *depth of the new property: structural (works without you) beats cosmetic.*
Our lead property is **censorship resistance**: **when the coordinator is gone, the money still
moves.**

| Show | Why it matters |
| --- | --- |
| **Before:** in V2 an operator who stops paying ends the story | the "before" half of before/after |
| **The moment:** stop the coordinator, a stranger claims, the provider is paid | the 3-minute video must include this |
| Open (**O**): free licence, contracts verified, runs from the repo | CROPS section in the README |
| Private (**P**): what each third party learns | see the table below |
| Secure (**S**): bounded coordinator key, veto window, guardian | trade-offs stated openly |

**What each outside party learns** (use this for the README's "P" line):

| Party | What it sees |
| --- | --- |
| Whitechain RPC and chain | deposits, payout addresses and amounts, anchors (fingerprints only) |
| Arkiv RPC and chain | audit receipts per provider (a random provider ID, challenge hashes, results), file manifests (a random file ID and hashes). **Not** owner IDs, names, sizes, chunk lists or pointer files |
| Us | phone numbers, which provider holds what |
| Nobody | file contents (locked on the owner's device) |

The README must also have a section titled **CROPS** with one or two lines per letter, say what the
project looked like before, and say which property is new. There must be **no third-party trackers
or analytics** in the client. We have none; say so.

### Arkiv: show that something reads it

They score four things, 25 points each, and each has a **file in the repo** (see section 10):

| Criterion | Our answer in one breath |
| --- | --- |
| Arkiv feedback | Real, reproducible friction we met. Be specific |
| Reach | Who uses it, and will it ship? Be honest: lab and field runs, no paying customers yet |
| Why Arkiv | If the audit history stayed only in our database, **we** could rewrite it and nobody could check. On Arkiv a provider, a judge or a stranger can read and verify it without asking us |
| How you use Arkiv | Each feature changes something a user sees: filter by attribute, wallet ownership, 30-day expiry, `arkivcheck public` |

Also say what we **kept off** Arkiv and why: owner IDs, file sizes and names, the encrypted pointer
file and its nonce, chunk lists, key material. They would reveal who stores what, or allow
offline passphrase guessing on a public chain.

**Arkiv is not counted if it is only a log nothing reads.** So in the demo, always show the read
side: `arkivcheck public` and the tamper turning red. Do not stop at "look, entities appeared".

---

## 5. The live demo: one network, three acts

The two chains are switched on from the first minute. **Act 1 is the original Vyomanaut demo,
run exactly as the operator guide describes.** Acts 2 and 3 use the same running network.
Details and exact commands: `operator_eth_guide.md`. Practise three times. Pre-type every command
in a text file so you only paste.

**Act 1 (as before):** providers join, the owner uploads, stored pieces look like noise, a machine
dies and the file still comes back. Say nothing about chains yet.

**Act 2: Arkiv, the history.** About two minutes. Run it before the escape, because the escape
stops the coordinator.

| Scene | Who | What you do | What you say |
| --- | --- | --- | --- |
| 2.1 It appears | Operator | Open `rabdi.explorer.arkiv.network/address/0x1044E36804C0cED0243529dc6Dc865a3E0b727B9`. Refresh after an audit period | "Every audit result is written here the moment it is final. We own the key that writes, but the board is public." |
| 2.2 Anyone can check | Anyone | `arkivcheck public` | "No password, no database. A stranger reads the records and recomputes the check." |
| 2.3 Tamper | Operator | `scripts/demo/arkiv_tamper.sh`: green, then red, then `--restore` | "Now I secretly change one record in our own database. The public copy no longer matches, and the check names the receipt." |
| 2.4 A file's birth | Owner | Upload a file; open its manifest in the explorer | "A hash and a count. No owner, no name, no list of who holds what." |

**Act 3: Whitechain, the money.** About four minutes.

| Scene | Who | What you do | What you say |
| --- | --- | --- | --- |
| 3.1 Fingerprint | Operator | `verifyreceipt epoch latest` → MATCH. Then `scripts/demo/chain_tamper.sh` → MATCH, then MISMATCH, then `--restore` | "A second, independent witness. Whitechain holds one fingerprint per two minutes. Change history and it fails." |
| 3.2 Payout address | Operator | List the providers' wallets; open one on the explorer; owner runs `wallet bind` | "Each provider has an address on a public chain, and nobody installed a wallet." |
| 3.3 Escrow | Owner | `vyomachain deposit --amount 1 …`; open the `deposited` link. Watch `credited`, `posted`, `claimed` | "The owner puts real test USDT into a contract. It pays what the providers earned." |
| 3.4 Capstone | Operator | On the next `posted` line, run `scripts/demo/chain_escape.sh` | "Now we disappear." |
| | Bystander | The script waits out the window, then claims from a different wallet | "A stranger presses the button. We are not involved." |
| | Anyone | Show the provider's wallet page: USDT arrived; point at *caller ≠ account* | "The provider is paid." |
| | Owner | (Only if 15 minutes have passed) `vyomachain refund` | "And the owner can leave with what is left." |

> **Restore after every tamper.** Both tamper scripts flip a flag on a stored receipt. Run
> `--restore` before the next scene or the next check will fail for the wrong reason.
>
> **Leave ten minutes between the upload and Act 3**, so providers have earned something to pay.
> Skip "delete a file" until the end: deleting stops the charges that fund the finale (and writes
> the tombstone, which is a good last 20 seconds).
>
> **Do the slow parts before the audience arrives** and keep a finished example open in a browser
> tab: the 120-second window and the 15-minute silence rule are long. Run the real thing for the
> video, and show the prepared tab at the booth.
>
> If a network fails in the room: show the explorer links from an earlier run and the recorded
> video, and say so. Never pretend a recording is live.

---

## 6. The one video

ETHGlobal takes one demo video. The EF allows **3 minutes at most**; Whitechain wants **2 to 4
minutes** showing each party. So make **one 3-minute video**, with the times in the README.

| Time | Shows | For |
| --- | --- | --- |
| 0:00 to 0:20 | **Before:** V2, the operator holds the money and the only copy of the records | EF, all |
| 0:20 to 0:45 | Providers, upload, audits flowing (a montage, no talking over the terminal) | all |
| 0:45 to 1:20 | Arkiv: entities in the explorer, `arkivcheck public`, tamper → red | Arkiv |
| 1:20 to 1:40 | Whitechain fingerprint: MATCH, tamper, MISMATCH | Whitechain |
| 1:40 to 2:35 | Owner deposits USDT; coordinator stopped; stranger claims; provider paid | Whitechain, EF |
| 2:35 to 3:00 | Limits said out loud, the CROPS summary, where the README is | all |

Show **each party**: owner, provider, operator, stranger. Say the limits in the video itself.

For the 4-minute live finalist slot (if you are chosen; then 3 more minutes of questions), use the
same cut and add the file manifest scene.

---

## 7. Say these limits first (before they ask)

They make the rest believable.

**Both chains**

- **These are test networks.** The coins have no value. The contracts are unaudited.
- **Getting your file back still needs us.** Downloads are signed by the coordinator. The chains
  protect money and history, not retrieval.
- **One coordinator writes to both chains.** If it is gone, the history stops being added to.
- **Cost grows with providers.** See section 9.

**Whitechain**

- **Only posted payouts are protected.** Earnings not yet posted when we vanish are not.
- **In the demo, we chose the providers' wallet addresses.** A real provider would bring their own.
- **USDT can be frozen by its issuer.** **Whitechain has one sequencer.** If it censors, there is a
  slower route through Ethereum.
- **In India**, crypto gains and transfers carry tax rules (VDA). UPI remains the better everyday
  rail for providers there. We offer this as an optional rail, not a replacement.

**Arkiv**

- **It proves the history did not change after we published it. It does not prove it was true
  when we published it.** A coordinator could still publish a false receipt. PASS and FAIL receipts
  carry the provider's own signature, so a faked one can be shown to be forged. TIMEOUT receipts
  are signed only by us.
- **Each batch is written once, but the chain does not lock it.** Only our Arkiv wallet can change
  or delete an entity, and any change is a visible transaction. We have **not** turned on Arkiv's
  read-only flag, so do not say the chain makes the batches impossible to change. Say "only our
  wallet can, and it shows".
- **Entities expire.** We set 30 days (the limit is about 182). It is a window, not permanent
  storage.
- **It is a public testnet, and Arkiv's own paper says testnet data may be purged** before its
  mainnet, planned for 2027.
- **It is public.** Anyone can read provider IDs and audit results. Owner and file details stay off.
- **`arkivcheck public` reads the first 100 entities.** Fine for a demo, not a full audit.

**Words to use:** "optional settlement rail", "optional public mirror", "hackathon build", "test
network". **Words to avoid:** "Vyomanaut is moving on-chain", "decentralised" without saying what
exactly, "immutable" or "permanent" about Arkiv, any claim about being faster, greener or cheaper
than data centres (say "designed to be"). **We have no token and will not have one.**

---

## 8. Questions you will get

### From the Whitechain judges

**Why does this need a blockchain? A normal database or UPI would do it.**
A bank rail cannot promise that a payout survives the operator. Here, a posted payout is
claimable by the provider even if we are gone. The chain is used for exactly that one job.

**Why Whitechain?**
It is cheap and fast enough that we can post payouts and let anyone claim for a fraction of a
cent. Its test USDT let us run the real payment flow. Be ready to name one real thing from
`WHITECHAIN_FEEDBACK.md`.

**Show me the USDT payment.**
Open the deposit link and the claim link. The token address ends in `…2f55a7`: it is the required
test USDT.

**What is on chain, and what is not?**
On chain: deposits, the payout list per period, claims, refunds, audit fingerprints. Off chain:
files, audit records, placement, repair, pricing.

**Who can steal the money?**
Nobody can withdraw it; there is no admin withdraw and no upgrade key. A stolen coordinator key
can post a bad list, but it cannot exceed what owners deposited, and the guardian can cancel it in
two minutes. Worst case is one period.

**What if the coordinator key is stolen *and* the guardian is asleep?**
Then one period of bad payouts can go through. We say so openly. A production version would make
the guardian a multi-signature wallet.

**What if Whitechain is down?**
Uploads and audits carry on, because the ledger is separate. Payouts wait and catch up. We saw a
dropped connection to the chain in our own run: the log said "will retry" and the anchors
continued.

**Do you have real users?**
Be honest: a lab run and a field run with thousands of audits and volunteer providers, no paying
customers yet. The hackathon added the payment rail that real customers would need.

**Is the gas sponsored?**
Yes. The coordinator pays the claim fees, so providers never touch a wallet or WBT. Anyone can
also claim for them.

**Why not a token?**
Because providers should be paid in something stable. A token adds speculation, and the problem
we are solving is trust, not fundraising.

**How many providers can it pay?**
At most 50 per posted period today, because the payout list is written on chain. That suits tens
of providers at demo speed and low thousands on a monthly payout. The next step is posting one
Merkle root per period and letting providers claim with proofs, so posting costs the same for any
number. That is designed, not built.

### From the Ethereum Foundation (CROPS) judges

CROPS means **C**ensorship-resistant, **O**pen, **P**rivate, **S**ecure. Answer in that order.
Lead with the property we added.

**Censorship resistance: what can we not stop?**
Posted payouts and unspent deposits. If we refuse, fail or disappear, providers can claim and
owners can reclaim. If Whitechain's sequencer censors, there is a slower route through Ethereum.
That is structural: it works without us. A setting we could switch off would not count.

**What still depends on you?**
Placement, repair and **retrieval**. The coordinator signs download permission. We say so openly;
the chains protect the money and the history first.

**Open: can I run it myself?**
Yes. The code is public under a free licence, the contracts are verified, and the demo runs from
the repo.

**Private: what does it learn?**
See the table in section 4. Weak spot: payout addresses and amounts are public on Whitechain,
audit receipts per provider are public on Arkiv, and we know phone numbers. We kept owners, file
names, sizes and chunk lists off both chains.

**Secure: what if a key leaks?**
The coordinator key is bounded: a per-period cap, a two-minute veto window, a separate guardian
key. Our Arkiv wallet key can write and delete only our own entities. The contracts have a test
suite with attack attempts, but they are **unaudited**.

**What did you build in these days, and what was already there?**
Show the table in section 3. Storage, audits and repair were there before. The escrow, the bridge,
the exit door, the audit mirror and the verifiers are new.

### From the Arkiv judges

**Why Arkiv and not Postgres, IPFS or a subgraph?**
Postgres is ours: the operator can edit it. A subgraph or IPFS pointer still has us as the author.
On Arkiv each entity has a wallet owner, a lifetime and queryable attributes, and anyone can run
the check without our database.

**How is this different from anchoring on Whitechain?**
Whitechain keeps one tiny fingerprint, which says "the history adds up to this". Arkiv keeps the
records themselves, readable and filterable, so a provider can check *their own* results. The two
are independent witnesses over the same receipts, and neither is needed for the other to work.

**What did you keep off Arkiv?**
Owner IDs, file names and sizes, the encrypted pointer file, chunk lists, keys. Putting them on a
public chain would show who stores what, or allow offline guessing of passphrases.

**Does anything a user sees read from Arkiv?**
Yes: `arkivcheck public` reads entities by attribute from Arkiv with no database. A provider can
verify its own receipts, and the tamper check turns red from the Arkiv copy, not from us.

**What does it cost?**
Measured on Rabdi: a one-receipt batch is about 0.36 million gas, a full 100-receipt batch about
4.7 million gas, which is **0.0047 tGLM** at 1 gwei. Our wallet went from 10 to about 9.5 tGLM
during this run (199 writes, including an upload of a video file). Testnet tokens, so no money.

**How many providers can the mirror serve?**
One write takes about 4 seconds (measured: batches land 3 to 4 seconds apart), so about 0.25
writes per second. Providers served ≈ writes per second × period length ÷ entities per provider
per period. At the 2-minute demo period that is about 30 providers; at a 1-hour period about 900.

**What if Arkiv is down or out of tokens?**
Uploads and audits carry on, because Postgres is the source of truth. Rows stay unpublished and
retry; the log says so once a minute. We saw this live when our access key was wrong.

**Who uses it, and will it ship after Mumbai?**
Be honest: no paying users yet. A provider or an operator can use the check today; shipping it to
volunteers on a real Arkiv network depends on Arkiv's mainnet.

**What friction did you hit?**
Say two real things (your `friction.md`). Never invent one.

### From finalists' panel and general judges

**Explain it to my grandmother.**
"You rent out spare disk space and get paid every month. Before, the company could just not pay
you, or change the books. Now the money sits in a locked box that pays you even if the company
vanishes, and the books are pinned up in public."

**Who is it for, and who pays?**
Households and small labs lending storage; people and businesses who want private, cheaper
storage. India first. Owners pay a fixed price like a utility bill.

**Why not Filecoin, Swarm or Storj?**
Storj is the closest cousin. Ours has no token, no stake to lock up, always-on client-side
encryption, and a coordinator that picks placement for diversity. Filecoin and Swarm use tokens
and market prices.

**Can the coordinator cheat on audits?**
Two protections. Every pass carries the provider's own signature, so a faked result can be shown
to be forged. And the history is fingerprinted on Whitechain and copied to Arkiv, so rewriting an
old record is caught from the outside.

**But you could simply not record a failure.**
The fingerprint and the copy prove old records were not changed. They do not prove a record was
written; the provider keeps the countersigned receipt, so a missing record is evidence against us.

**What happens to the data if you go bankrupt?**
Honest answer: the money is protected, the *service* is not automatic. Pieces sit on providers'
computers, and getting files back needs a coordinator. A future version could let anyone run one.

**Is this a real startup or a hackathon toy?**
It is a startup that already existed. Before the hackathon the V2 lab demo had thousands of
audits, no integrity failures, and an exact ledger. The hackathon added the trust pieces that
were missing.

**How did you use AI?**
Claude helped write code, tests and guides; we reviewed, ran and decided. Say this plainly,
because the forms ask. The Arkiv form asks too.

**What is the business model?**
A small fee on storage priced like a utility. The chains add no fee of ours; the coordinator pays
small gas costs.

---

## 9. Numbers to have in your pocket

| Item | Value |
| --- | --- |
| Whitechain network | Sepolia, chain ID **1874** |
| Test USDT | `0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7` (6 decimals) |
| Escrow contract | `0xBcE841C78c6c40C551fc223Ba9415D4751Af5958` (verified) |
| Audit anchor contract | `0x1b2b623ecA0A7899C6a24eaf401a99d2434FDB3d` (verified) |
| Veto window | **120 seconds** after a payout is posted |
| Silence before refund | **900 seconds** without a post |
| Payees per posted period | **50** |
| Demo scale | ₹1,000 = 1 USDT |
| Whitechain explorer | `https://explorer.testnet.whitechain.io` |
| Contract tests | 46 tests, including attack attempts |
| Arkiv network | Rabdi testnet, chain ID **7794095**, 2-second blocks, token tGLM |
| Arkiv explorer | `https://rabdi.explorer.arkiv.network` |
| Our Arkiv writer wallet | `0x1044E36804C0cED0243529dc6Dc865a3E0b727B9` |
| A query anyone can run | `app = str('vyomanaut-demo') AND kind = str('audit_batch')` (or `arkivcheck public`) |
| Receipts per Arkiv entity | up to **100** (about 80 KB) |
| Write speed, measured | about **4 seconds** per entity |
| Cost, measured | about 0.36 M gas (1 receipt) to 4.7 M gas (100 receipts); 0.0047 tGLM for a full batch |
| Arkiv entity lifetime | **30 days** (limit about 182) |
| Anchor interval, audit period | **2 minutes** in the demo |
| Submission deadline | **Sat 7 Nov, 09:00 IST** is our target. Confirm on ETHGlobal's page: one tracker lists Sun 8 Nov, 09:00 IST |
| Finalist slot | 4 minutes demo, 3 minutes questions |

---

## 10. Before you submit: what must exist in the repo

Tick each line yourself.

**All three**

- [ ] Public repo, free licence (we have `LICENSE`), steady commit history (do **not** squash)
- [ ] README explains setup, architecture and limits
- [ ] README "before and during" part, with the starting-point commit `086be48` / tag
- [ ] One demo video of at most 3 minutes, with the times in the README
- [ ] "How we used AI" stated

**Whitechain**

- [ ] Contracts verified on the Whitechain explorer
- [ ] **A USDT payment explorer link in the README** (deposit and claim)
- [ ] One line in the README: why this flow needs on-chain stablecoin settlement
- [ ] `WHITECHAIN_FEEDBACK.md` (a few honest lines: what worked, where you got stuck)

**Ethereum Foundation**

- [ ] README section titled **CROPS**: C, O, P, S in one or two lines each
- [ ] README says what the project looked like before and which property is new
- [ ] No third-party trackers or analytics in the client

**Arkiv** (all five files must be in an `arkiv/` folder at the repo root. **None exist yet**)

- [ ] README: the wallet address that wrote our entities (`0x1044…27B9`) and one query to run
- [ ] `arkiv/schema.md`: our entities, the fields we query, who owns them, when they expire
- [ ] `arkiv/friction.md`: real, reproducible feedback
- [ ] `arkiv/reach.md`
- [ ] `arkiv/why-arkiv.md`
- [ ] `arkiv/how-you-use-arkiv.md`
- [ ] README says what existed before and what we added to Arkiv
- [ ] No real personal data in Arkiv entities (the demo uses made-up phone numbers and IDs)

---

## 11. If it goes wrong on stage

| What happens | What to say and do |
| --- | --- |
| A chain is slow | "Test networks have off days." Switch to the prepared explorer tab |
| The chain connection drops | The log says "will retry". Point at it: "That is the system carrying on" |
| Arkiv shows 401 or no entities | Check the access key file; if needed run without it. Say: "Arkiv trouble must not stop storage, and it does not" |
| The coordinator claims before you stop it | "That is the happy path. Now the unhappy one" and replay the recording. To retry, deposit again (a refund empties the owner's credit, and nothing is posted without credit), wait for `posted`, then run `chain_escape.sh` at once |
| `arkivcheck` cannot reach the database | `export PGSSLMODE=disable` and run it again |
| A provider drops | Perfect. Show repair, then carry on |
| You forget a number | Section 9 is open in a tab. Read it |
| A question you cannot answer | "We have not built that yet" is a good answer. Never guess |
