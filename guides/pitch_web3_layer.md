# The Trust Layer — what we say after the file comes back

A short spoken script for the part of the pitch **after the file is retrieved**. The main
presentation (providers join, upload, a machine dies, the file comes back) is unchanged and is not
in here.

Facts, numbers, limits and judges' questions live in `presenter_eth_guide.md`. Exact commands live
in `operator_eth_guide.md`. This file decides **what we say, in what order, and with which picture**.
Learn the **bold** lines word for word. Everything else is guidance.

---

## The idea in one breath

> **Everything you just saw works, as long as you trust us. Now we take that trust away.**

The audience has just watched the storage work. The trust layer answers three questions anyone in
the room would ask a storage company. Each question has one partner as its answer:

| The question a provider would ask | Who answers it | The picture |
| --- | --- | --- |
| "Can you quietly change my exam results?" | **Arkiv** (public records) + Whitechain (fingerprint) | Marks pinned on the college notice board |
| "Will I actually get paid?" | **Whitechain** (USDT escrow) | A vending machine for salaries |
| "What if your company disappears tomorrow?" | **Ethereum Foundation, CROPS** (it works without us) | The shop is shut, but the vending machine still pays |

That is the whole layer: **three questions, three proofs, one ending.**

---

## Spread it out: three small hints during the main demo

Do not save every chain moment for the end. Drop **one sentence and one screen** at three moments
in the main demo, then pay all three off after retrieval. Each hint is under ten seconds. Do not
explain it yet.

### Hint 1: at the deposit (Whitechain)

When the owner tops up their account, also put **real test USDT** into the escrow at the same
moment. Do not wait for the finale.

```bash
"$BIN_DIR/vyomachain" wallet bind --api "http://$MY_IP:8090" --data-dir "$OWNER_DIR" --key-file ~/vyoma-keys/owner.key
"$BIN_DIR/vyomachain" deposit --amount 1 --key-file ~/vyoma-keys/owner.key
```

Open the `deposited` explorer link for two seconds, then move on.

> **"This money didn't go to us. It went into a locked box on Whitechain. Remember this box. It
> comes back at the end."**

Why here: the providers start earning **from this minute**. By the time the file is retrieved, the
coordinator has a payout ready to post, so the finale does not wait. (The guide's rule "leave ten
minutes between the upload and Act 3" happens by itself.)

### Hint 2: at the upload (Arkiv)

The moment the upload finishes, show the new `file_manifest` entity on the Arkiv explorer.

> **"Every file gets a birth certificate on a public board. It holds a fingerprint and a count.
> No name, no owner, no size."**

### Hint 3: while the audits run (Arkiv)

While the dashboard shows audit passes climbing (the phones going from VETTING to ACTIVE is a good
moment), point at the Arkiv explorer page of our writer wallet. New transactions appear every two
minutes.

> **"Every exam this phone passes is pinned up in public. Hold that thought."**

Three seeds planted. Nobody is confused, because each one is a single sentence.

---

## After retrieval: the trust layer (about 4 minutes)

Start the moment the file is back and the audience has seen it open.

> **"That's storage that works. But every single step you just watched, you had to trust us. Let's
> take that trust away, one piece at a time."**

### Beat 1: "Can we cheat on the records?" (≈70 s, Arkiv, then Whitechain)

**The picture.** College exam results go on the notice board outside the office. If the clerk
later changes your marks in the register, anyone holding the photo of the board can prove it.

**Do and say:**

1. Run `arkivcheck public` (no database, no password). It goes green.
   > **"This is a stranger's laptop. No password, no access to our database. It reads every audit
   > result from Arkiv and checks the signatures. All green."**
2. Run `scripts/demo/arkiv_tamper.sh`: green, then **red**, and it names the receipt.
   > **"Now I play the dishonest operator. I change one exam result in our own database. Watch."**
   > *(red)* **"Caught. It even tells you which result I touched."**
3. Run `verifyreceipt epoch latest` (MATCH), then `scripts/demo/chain_tamper.sh` (MISMATCH).
   > **"And a second witness. Every two minutes Whitechain keeps one fingerprint of all the results.
   > Change one, and the fingerprint stops matching."**
4. **Restore both** (`--restore`), off-screen or while you talk.

**The one line:** **"Arkiv keeps the records anyone can read. Whitechain keeps the fingerprint that
proves nobody edited them."**

### Beat 2: "Will the phone actually get paid?" (≈60 s, Whitechain)

**The picture.** A vending machine for salaries. The owner filled it at the start (Hint 1). We
press a button saying "this phone gets 2 coins". Two minutes later the machine drops the coins
straight into the phone's pocket.

**Do and say:**

1. Point at the coordinator log: `posted`, then about 2 minutes later `claimed`. (Have a recent pair
   on screen already.)
   > **"Remember the box from the deposit? We just told it who earned what. For two minutes a
   > separate guardian can cancel a wrong list. Then it's final, and it pays."**
2. Open **the phone's** payout wallet on the explorer: USDT arrived. (Find it with the
   `provider_payout_addresses` query in `operator_eth_guide.md`, Scene 2.2, before the show.)
   If asked: **"In the demo we created that wallet for the phone. A real provider brings their
   own."**
3. **Hold up the phone.** Open the app's **Payments** tab: the escrow and coordinator wallet,
   read live from the chain.
   > **"This phone never installed a wallet, never bought gas, never saw a seed phrase. It just
   > got paid in a dollar stablecoin, and it can read the box itself."**

**The one line:** **"A log can prove what you're owed. Only a chain can pay you when we won't."**

### Beat 3: the capstone, "What if we vanish?" (≈80 s, the magic moment)

**The picture.** It's Diwali night. The shop owner pulls the shutter down and goes home. The
vending machine outside still drops your chocolate. Nobody has to be there.

**Do and say.** Wait for the next `posted` line (it comes every release tick while the deposit
lasts), then **immediately**:

1. `scripts/demo/chain_escape.sh`
   > **"Now we disappear."** *(The coordinator stops. Say it slowly.)*
   > **"Our server is off. The company that runs this network is gone."**
2. The script waits out the window, then a **stranger's wallet** claims.
   > **"Someone who has nothing to do with us presses the button..."**
3. Show the claim on the explorer: **caller is not the account**, and USDT lands in the provider's
   wallet.
   > **"...and the money goes to the provider, never to the person who pressed it. We're not
   > here. It still paid."**

**The one line:** **"That's not a promise. It's code we can't switch off. It works without us."**

### Close (≈15 s)

> **"Storage stays off-chain, because that's fast and cheap. The chains do only the two jobs we
> shouldn't be trusted with: remembering, and paying. No token. No wallet for providers. Nothing
> new for them to learn."**

Optional, if time allows: delete the file and show its **tombstone** on Arkiv.
> **"And when a file is deleted, it gets a death certificate too."**

---

## The same layer, cut for each partner

When pitching to one partner, keep all three beats but **change the order and the weight**. Open
with their beat and end on their line.

| Partner | Open with | Spend most time on | End with |
| --- | --- | --- | --- |
| **Whitechain** | Hint 1 replayed: "the money went into a box on Whitechain" | Beat 2 (deposit → `posted` → `claimed`, explorer links, test USDT `…2f55a7`), then Beat 3 | **"A real USDT payment reaches a provider even after the operator is gone."** |
| **Ethereum Foundation** | "Before this weekend, if we stopped paying, the story ended." | Beat 3, slow. Then name CROPS: C (it pays without us), O (open, verified contracts), P (no owner data on chain), S (bounded key, veto window) | **"Censorship resistance that's structural. It works without us."** |
| **Arkiv** | Hint 3 replayed: "every exam result is pinned in public" | Beat 1, and show the **read side**: `arkivcheck public`, the query `app = str('vyomanaut-demo') AND kind = str('audit_batch')`, tamper turns red | **"Our audit history lives where we can't quietly edit it, and anyone can query it."** |

Each cut fits in **3 minutes**.

---

## Words: say these, never those

| Say | Never say |
| --- | --- |
| "test network", "test USDT" | "real money", "mainnet" |
| "optional payment rail", "optional public mirror" | "we moved Vyomanaut on-chain" |
| "only our wallet can change it, and it shows" (Arkiv) | "immutable", "permanent" about Arkiv |
| "works without us" (posted payouts, refunds) | "fully decentralised" |
| "no token, and there won't be one" | anything about a token sale or price |
| "getting your file back still needs us" (if asked) | "trustless storage" |

If someone asks something not built: **"We haven't built that yet."** Never guess.

---

## Timing rules that make it feel like magic

- **Deposit early (Hint 1).** Then the payout is already waiting when you reach Beat 2.
- **Beat 3 needs a fresh `posted` line.** Keep the coordinator log visible. The moment you see
  `period N posted`, run `chain_escape.sh`. You have about two minutes before the coordinator
  claims by itself.
- **Restore after every tamper**, or the next check fails for the wrong reason.
- **Keep three browser tabs ready:** the Arkiv writer wallet, the escrow contract, one provider's
  payout wallet. Never type a URL on stage.
- **Practise the three bold lines of each beat out loud** until you can say them without the screen.
- If a chain is slow: switch to a prepared tab from an earlier run and **say so**. Never present a
  recording as live.

---

## The grandmother version (if someone asks "explain it simply")

> **"You lend your phone's spare space and get paid. Before, the company could skip paying you or
> fudge your records. Now your marks are pinned on a public board, and your pay sits in a vending
> machine that pays you even if the company shuts down."**
