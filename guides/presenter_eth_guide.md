# Presenter Guide — what we added at ETHGlobal Mumbai

Read this the night before. Everything is in plain words. If you can say the one-minute story
below without looking, you are ready.

> **Update the status box before every presentation.** Anything not marked LIVE must not be
> shown as working.

## Status box

Everything below was built and tested on a local chain and a real database. **Nothing counts as
LIVE until the smoke run on Whitechain Sepolia has passed** — tick each row yourself after you have
seen its explorer link or its log line.

| Piece | Built | Tick when seen live |
| --- | --- | --- |
| Escrow contract on Whitechain Sepolia (USDT in, payouts out, refund) | Deployed and verified | [ ] |
| Owner deposits USDT with `vyomachain` → ledger credit | Built | [ ] `[CHAIN] credited …` |
| Payout reserved → posted → claimed on chain | Built | [ ] `[CHAIN] period N claimed` |
| Providers get a payout address automatically (custodial in the demo) | Built | [ ] |
| "Coordinator disappears, a stranger still pays the provider" (`chain_escape.sh`) | Built, rehearsed on a local chain | [ ] |
| Owner gets unspent USDT back (`vyomachain refund`) | Built, tested on a local chain | [ ] |
| Audit fingerprint anchored on chain | Built | [ ] `[ANCHOR] epoch N anchored` |
| `verifyreceipt` + tamper demo (`chain_tamper.sh`) | Built, rehearsed on a local chain | [ ] MATCH then MISMATCH |
| Grafana chain dashboard | Built | [ ] |

---

## 1. The one-minute story

Say this in your own words:

> Vyomanaut is cloud storage on ordinary computers instead of a big data centre. People lend
> spare disk space. Files are locked with the owner's key before they leave, so the computers
> only hold noise.
>
> There was one honest weakness. **Providers have to trust us to pay them.** If we vanish,
> their earnings vanish.
>
> At this hackathon we fixed that part. Owners now put test USDT into a contract on
> Whitechain. When we say "pay these providers", the contract pays — **and once we have said
> it, we cannot take it back, and we cannot stop it by disappearing.**
>
> Storage itself still runs off-chain, because that is where it is fast and cheap. The
> blockchain does only the job it is better at: paying people when the operator will not.

**The one line to remember:**

> A log can prove what you are owed. Only a chain can pay you when we won't.

---

## 2. What we added, in five points

1. **A locked box (the escrow contract).** Owners put USDT in. Only its fixed rules can take
   it out. We cannot withdraw it, and nobody can change the rules.
2. **A bridge.** The coordinator watches the box. When an owner deposits, it credits their
   normal account. When providers earn, it tells the box who to pay.
3. **A safety window.** A separate "guardian" key can cancel a payout list within two
   minutes of it being posted. After that it is final.
4. **An exit door.** Posted payouts can be claimed by *anyone*, and the money always goes to
   the provider. If we are silent for fifteen minutes, owners can take back what they have not
   spent.
5. **A fingerprint of the audit history.** Every couple of minutes we write one short
   fingerprint of all new audit records to the blockchain. If we ever change an old record, the
   fingerprint no longer matches, and anyone with the database can show it.

What we did **not** change: how files are stored, encrypted, checked, repaired, or priced.
Providers' computers run the same program as before. **No wallet, no coins, nothing new for
them.**

### A picture to use

> Think of a vending machine for salaries. The owner fills it with coins. We press a button
> that says "Provider A gets 3, Provider B gets 2". After two minutes the machine will hand
> out exactly that, to exactly those people, even if the shop is closed and we have left the
> country.

---

## 3. Before and during (needed for the README and the form)

| | Before the hackathon (V2, up to `086be48`) | Built during ETHGlobal |
| --- | --- | --- |
| Money | Ledger in Postgres, pretend payments | Real test-USDT escrow on Whitechain Sepolia |
| If the operator vanishes | Providers lose unpaid earnings | Posted payouts stay claimable; owners can reclaim unspent deposits |
| Who can move the money | The operator | Fixed contract rules, plus a veto key for two minutes |
| Providers | Join, store, get paid by trust | **Identical.** Nothing new for them |
| Everything else | Storage, audits, repair, ZeroTier network | Unchanged |

Starting point proof: commit `086be48` (29 Sep 2026), tag `pre-ethglobal-mumbai`.

---

## 4. The live demo: the original demo, then the blockchain finale

The blockchain is switched on from the first minute but stays invisible. **Act 1 is the original
Vyomanaut demo, run exactly as the operator guide describes.** Act 2 is a four-scene finale on the same
running network, about four minutes, and each scene answers one trust question. Details and exact
commands: `operator_eth_guide.md`, Parts 4 and 5.

**Act 1 (as before):** providers join, the owner uploads, stored pieces look like noise, a machine dies and
the file still comes back, payments move in the ledger. Say nothing about the blockchain yet.

**Act 2: the finale.** Practise it three times. Pre-type every command in a text file so you only paste.

| Scene | Who | What you do | What you say |
| --- | --- | --- | --- |
| 2.1 Audit anchoring | Operator | `verifyreceipt epoch latest` → MATCH. Then `scripts/demo/chain_tamper.sh` → MATCH, then MISMATCH | "Everything you just watched was fingerprinted onto a public chain every two minutes. Now I secretly change one record. The chain still remembers the old fingerprint, so the check fails. We cannot quietly rewrite history." |
| 2.2 Payout address | Operator | List the providers' payout wallets; open one on the explorer; the owner runs `wallet bind` | "Each provider has an address on a public chain, and nobody installed a wallet. The owner proves they hold theirs by signing a sentence with it." |
| 2.3 Escrow | Owner | `vyomachain deposit --amount 1 …`; open the `deposited` link. Watch `credited`, `posted`, `claimed` | "The owner puts real test USDT into a contract. It pays what the providers earned during the demo. We posted a list we cannot take back, then sent each provider its money." |
| 2.4 Capstone | Operator | On the next `posted` line, run `scripts/demo/chain_escape.sh` | "Now we disappear." (the script stops only the coordinator) |
| | Bystander | The script waits out the window, then claims from a different wallet | "A stranger presses the button. We are not involved." |
| | Anyone | Show the provider's wallet page: USDT arrived. Point at *caller ≠ account* | "The provider is paid. The money went to the provider, not to the stranger." |
| | Owner | (Only if 15 minutes have passed) `vyomachain refund` | "And the owner can leave with what is left." |

> **Leave ten minutes between the upload and Act 2**, so the providers have earned something to pay. Skip
> the "delete a file" step of Act 1 until the end: deleting stops the charges that fund the finale.
>
> Amounts are tiny (fractions of a USDT) because the demo file is small. Say so before anyone asks; the
> flow is the point.
>
> **Do the first part before the audience arrives** and keep a finished example open in a browser tab: the
> 120-second window and the 15-minute silence rule are long. Run the real thing for the video; show the
> prepared tab in the booth.
>
> If the network or testnet fails in the room: show the explorer links from an earlier run and the
> recorded video, and say so. Never pretend a recording is live.

---

## 5. Say these limits first (before they ask)

They make the rest believable.

- **It is a test network.** The coins have no value. The contracts are unaudited.
- **Only posted payouts are protected.** Earnings not yet posted when we vanish are not.
- **Owners need a little WBT** (Whitechain's fuel) to deposit. Providers need none; we pay
  their claim fees.
- **In the demo, we chose the providers' wallet addresses.** A real provider would bring
  their own.
- **Getting your file back still needs us.** Downloads are signed by the coordinator. The
  chain protects money, not retrieval.
- **USDT can be frozen by its issuer.** A stablecoin brings that risk.
- **Whitechain has one sequencer.** If it censors, there is a slower route through Ethereum.
- **Cost grows with providers.** The payout list is written on chain, so gas rises with the
  number of providers in a period. Good for tens to low hundreds.
- **In India**, crypto gains and transfers carry tax rules (VDA). UPI remains the better
  everyday rail for providers there. We offer this as an optional rail, not a replacement.

**Words to use:** "optional settlement rail", "hackathon build", "test network".
**Words to avoid:** "Vyomanaut is moving on-chain", "decentralised" without saying what
exactly, any claim about being faster, greener or cheaper than data centres (say "designed
to be"). **We have no token and will not have one.**

---

## 6. Questions you will get

### From the Whitechain judges

**Why does this need a blockchain? A normal database or UPI would do it.**
A bank rail cannot promise that a payout survives the operator. Here, a posted payout is
claimable by the provider even if we are gone. The chain is used for exactly that one job.

**Why Whitechain?**
It is cheap and fast enough that we can post payouts and let anyone claim for a fraction of a
cent. Blocks take about one second. Their test USDT let us run the real payment flow. Be
ready to name one real thing from `WHITECHAIN_FEEDBACK.md`.

**Show me the USDT payment.**
Open the deposit link and the claim link. Say the token address ends in `…2f55a7`: it is the
required test USDT.

**What is on chain, and what is not?**
On chain: deposits, the payout list per period, claims, refunds. Off chain: files, audit
records, placement, repair, pricing.

**Who can steal the money?**
Nobody can withdraw it; there is no admin withdraw and no upgrade key. A stolen coordinator
key can post a bad list, but it cannot exceed what owners deposited, and the guardian can
cancel it in two minutes. Worst case is one period.

**What if the coordinator key is stolen *and* the guardian is asleep?**
Then one period of bad payouts can go through. We say so openly. A production version would
make the guardian a multi-signature wallet.

**What if Whitechain is down?**
Uploads and audits carry on, because the ledger is separate. Payouts wait and catch up when it
returns. Posted payouts stay claimable.

**Why is the audit history anchored every two minutes and not every record?**
One short fingerprint per batch, however many records, so the cost does not grow with the
network. The records themselves stay in our database; the chain only remembers what they must
add up to.

**Why not a token?**
Because providers should be paid in something stable. A token adds speculation, and the
problem we are solving is trust, not fundraising.

**Is the gas sponsored?**
Yes. The coordinator pays the claim fees, so providers never touch a wallet or WBT. Anyone
can also claim for them.

**How many times can you run this?**
As many as there is fuel and coins. Each payout period can be claimed once by design, and
period numbers only go up. The practical limits are the test WBT and USDT balances.

### From the Ethereum Foundation (CROPS) judges

CROPS means **C**ensorship-resistant, **O**pen, **P**rivate, **S**ecure. Answer in that order.

**Censorship-resistance: what can we not stop?**
Posted payouts and unspent deposits. If we refuse, fail or disappear, providers can claim and
owners can reclaim. If Whitechain's sequencer censors, there is a slower route through
Ethereum.

**What still depends on you, then?**
Placement, repair and **retrieval**. The coordinator signs download permission. We say this
openly; the chain protects the money first.

**Open: can I run it myself?**
Yes. The code is public under a free licence, the contracts are verified, and the demo runs
from the repo.

**Private: what does the chain learn?**
Fingerprints and payout addresses, never file contents. Files are locked on the owner's
device. Weak spot: payout addresses and amounts are public, and we know phone numbers.

**Secure: what if a key leaks?**
The coordinator key is bounded: a per-period cap, a two-minute veto window, a separate
guardian key. The contracts have a test suite with attack attempts (double claim, wrong
caller, oversized post), but they are **unaudited**.

**What did you build in these two days, and what was already there?**
Show the before/after table in section 3. Everything storage-related was there before; the
escrow, the bridge, the exit door and the guides are new.

### From finalists' panel and general judges

**Explain it to my grandmother.**
"You rent out spare disk space and get paid every month. Before, the company could just not
pay you. Now the money sits in a locked box that pays you even if the company vanishes."

**Who is it for, and who pays?**
Households and small labs lending storage; people and businesses who want private, cheaper
storage. India first. Owners pay a fixed price like a utility bill.

**Why not Filecoin, Swarm or Storj?**
Storj is the closest cousin. Ours has no token, no stake to lock up, always-on client-side
encryption, and a coordinator that picks placement for diversity. Filecoin and Swarm use
tokens and market prices.

**Can the coordinator cheat on audits?**
Two protections. Every pass carries the provider's own signature, so a faked result can be shown
to be forged. And every couple of minutes we write a fingerprint of the audit history to the
blockchain, so rewriting an old record is caught: the check compares the database with the
fingerprint read from the chain, not from us.

**But you could simply not record a failure.**
The fingerprint proves old records were not changed. It does not prove a record was written; the
provider keeps the countersigned receipt, so a missing record is evidence against us.

**What happens to the data if you go bankrupt?**
Honest answer: the money is protected, the *service* is not automatic. Pieces sit on
providers' computers, and getting files back needs a coordinator. A future version could
let anyone run one. That is the next step, not something we have today.

**Is this a real startup or a hackathon toy?**
It is a startup that already existed. Before the hackathon the V2 lab demo had thousands of
audits, no integrity failures, and an exact ledger. The hackathon added the trust piece
that was missing.

**How did you use AI?**
Claude helped write code, tests and guides; we reviewed, ran and decided. Say this plainly,
because the form asks.

**What is the business model?**
A small fee on storage priced like a utility. The chain adds no fee of its own; the
coordinator pays small gas costs.

---

## 7. Numbers to have in your pocket

| Item | Value |
| --- | --- |
| Network | Whitechain Sepolia, chain ID **1874**, one-second blocks |
| Test USDT | `0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7` (6 decimals) |
| Escrow contract | `0xBcE841C78c6c40C551fc223Ba9415D4751Af5958` (verified) |
| Audit anchor contract | `0x1b2b623ecA0A7899C6a24eaf401a99d2434FDB3d` (verified) |
| Veto window | **120 seconds** after a payout is posted |
| Silence before refund | **900 seconds** without a post |
| Demo scale | ₹1,000 = 1 USDT |
| Explorer | `https://explorer.testnet.whitechain.io` |
| Contract tests | 46 tests, including attack attempts |
| Prize deadline | Sat 7 Nov, 09:00 IST (aim for 06:00) |

---

## 8. If it goes wrong on stage

| What happens | What to say and do |
| --- | --- |
| The chain is slow | "Test networks have off days." Switch to the prepared explorer tab |
| The coordinator claims before you stop it | Show it as the normal path: "That is the happy path. Now the unhappy one" and replay the recording |
| A provider drops | Perfect. Show repair, then carry on |
| You forget a number | Section 7 is open in a tab. Read it |
| A question you cannot answer | "We have not built that yet" is a good answer. Never guess |
