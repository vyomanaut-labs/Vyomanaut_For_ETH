# Vyomanaut

**Private cloud storage on the spare space of everyday phones and PCs. Payments and audit records
on public chains, so nobody has to trust the operator.**

Your files are encrypted on your device, split into pieces and spread across many independent
providers. No provider can read your data or holds a whole file, and even if a third of them
disappear overnight, the maths still brings your file back.

Built by [Vyomanaut Labs](https://www.linkedin.com/company/vyomanaut-labs) and extended at
**ETHGlobal Mumbai** for three partners:

| Partner | What we added | See it yourself |
| --- | --- | --- |
| **Whitechain** | Owners pay in test USDT into an escrow contract; providers are paid from it, even if we disappear | [Escrow contract](https://explorer.testnet.whitechain.io/address/0xBcE841C78c6c40C551fc223Ba9415D4751Af5958) |
| **Ethereum Foundation (CROPS)** | Payouts and refunds work without us: censorship resistance built into the contract | [How it works](#the-on-chain-layer) |
| **Arkiv** | Every audit result is published where anyone can read and check it | [Our Arkiv writer wallet](https://rabdi.explorer.arkiv.network/address/0x1044E36804C0cED0243529dc6Dc865a3E0b727B9) |

> **Anyone can run a Vyomanaut network.** Bring your own machine, keys, wallets and contracts.
> Section 4 shows how, on free hosting.

---

## Contents

1. [Try it in five minutes](#1-try-it-in-five-minutes)
2. [Become a provider (phone or PC)](#2-become-a-provider)
3. [Store a file (data owner)](#3-store-a-file)
4. [Run your own network (coordinator)](#4-run-your-own-network)
5. [The on-chain layer](#the-on-chain-layer)
6. [How it works](#6-how-it-works)
7. [Limits, said plainly](#7-limits-said-plainly)
8. [Repository, docs and team](#8-repository-docs-and-team)

---

## 1. Try it in five minutes

| You want to | Do this |
| --- | --- |
| **See it** | Watch the demo video: *link added on submission* |
| **Lend your phone's space** | Install the app, join our network: [section 2.1](#21-android-phone) |
| **Store a file** | Download the client and upload: [section 3](#3-store-a-file) |
| **Check our records without trusting us** | Run `arkivcheck public` or open the explorer links above: [section 5](#the-on-chain-layer) |

**Our public network:** `http://<COORDINATOR-IP>:8080` *(the address is shown at our booth and in
the submission; any network you run yourself works the same way)*.

### Downloads

All files are on the [latest release](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest).
Direct links:

| What | Android | Windows | macOS (Apple silicon / Intel) | Linux (x86-64 / ARM) |
| --- | --- | --- | --- | --- |
| **Provider** (lend space) | [APK](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/vyomanaut-provider-android.apk) | [exe](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/provider-windows-amd64.exe) | [arm64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/provider-macos-arm64) / [amd64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/provider-macos-amd64) | [amd64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/provider-linux-amd64) / [arm64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/provider-linux-arm64) |
| **Client** (store files) | | [exe](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/client-windows-amd64.exe) | [arm64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/client-macos-arm64) / [amd64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/client-macos-amd64) | [amd64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/client-linux-amd64) / [arm64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/client-linux-arm64) |
| **vyomachain** (wallet, USDT) | | [exe](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/vyomachain-windows-amd64.exe) | [arm64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/vyomachain-macos-arm64) / [amd64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/vyomachain-macos-amd64) | [amd64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/vyomachain-linux-amd64) / [arm64](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/vyomachain-linux-arm64) |

Checksums are in `SHA256SUMS.txt` on the release. To build everything yourself:
`.\scripts\release\build_release.ps1` (Windows, with Go and Android Studio).

> **macOS:** the binaries are not notarised. After downloading, run
> `chmod +x <file> && xattr -d com.apple.quarantine <file>`.
> **Linux:** `chmod +x <file>`.

---

## 2. Become a provider

A provider lends disk space and is paid for keeping its pieces safe. You need:
- the coordinator's address (for our network, see section 1);
- a phone number. Each number can register once on a network.
- the 6-digit join code. The network's operator reads it to you (see 4.6).

You need **no wallet, no coins and no VPN**. Your device opens connections out to the network's
relay, so it works behind a home router or on mobile data.

### 2.1 Android phone

Android 8 or newer, 64-bit (almost every phone since 2019).

1. **Install.** Open the [APK link](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/vyomanaut-provider-android.apk)
   on the phone and allow your browser to install apps when asked.
2. **Network tab.** Enter the coordinator address (`http://<COORDINATOR-IP>:8080`) and your phone
   number, leave **Relay** empty, and tap **Save and test connection**. You should see
   *Coordinator reachable*, and under *This phone's address*, *Audits and file uploads reach this
   phone through the relay*.
3. **Storage tab.** Choose how much space to lend (at least 1 GB, at most 10% of your free
   space) and save.
4. **Home tab: "Before you start".** Tap **Allow background running** and accept. If your phone
   is a Xiaomi, OPPO, vivo, realme, Samsung, OnePlus or Huawei, follow the extra steps shown there:
   these makers stop background apps more aggressively. (The ZeroTier step appears only if you use
   ZeroTier.)
5. **Register.** Tap **Register this phone**, ask the operator for your 6-digit code, enter it.
   The provider **starts by itself** and the status turns **online**.
6. **Leave it running.** A notification shows it is working. Plugging in helps but is not
   required: the app keeps the phone's network awake.
7. **Payments tab.** See your unpaid balance, recent audits, and the coordinator wallet and
   escrow, read straight from Whitechain.

Something wrong? The **Home** tab shows the provider's own output, kept across restarts. If the
operator restarted their network, tap **Register again**.

### 2.2 Windows PC

1. Download [`provider-windows-amd64.exe`](https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/provider-windows-amd64.exe)
   into a folder, open **PowerShell** there.
2. Register once (it asks for the join code):
   ```powershell
   .\provider-windows-amd64.exe onboard --microservice-url=http://<COORDINATOR-IP>:8080 --phone=+91XXXXXXXXXX --storage-gb=10 --data-dir="$env:USERPROFILE\.vyomanaut"
   ```
3. Start providing (keep the window open):
   ```powershell
   .\provider-windows-amd64.exe run --mode=demo --microservice-url=http://<COORDINATOR-IP>:8080 --data-dir="$env:USERPROFILE\.vyomanaut" --declared-storage-gb=10 --relay-addrs=<COORDINATOR-IP>:30300
   ```
   Expect `daemon ready`, then `reserved a slot on <COORDINATOR-IP>:30300`.

From source instead (needs Go): `.\scripts\demo\join.ps1 http://<COORDINATOR-IP>:8080 -RelayAddrs <COORDINATOR-IP>:30300`.
Full guide: [`guides/provider_windows_guide.md`](guides/provider_windows_guide.md).

### 2.3 macOS or Linux PC

1. Download the provider for your machine (table in section 1), then
   `chmod +x provider-*` (and on macOS `xattr -d com.apple.quarantine provider-*`).
2. Register once:
   ```bash
   ./provider-macos-arm64 onboard --microservice-url=http://<COORDINATOR-IP>:8080 --phone=+91XXXXXXXXXX --storage-gb=10 --data-dir="$HOME/.vyomanaut"
   ```
3. Start providing:
   ```bash
   ./provider-macos-arm64 run --mode=demo --microservice-url=http://<COORDINATOR-IP>:8080 --data-dir="$HOME/.vyomanaut" --declared-storage-gb=10 --relay-addrs=<COORDINATOR-IP>:30300
   ```

From source instead (needs Go, no RocksDB):
```bash
eval "$(scripts/release/purego_env.sh)"
scripts/demo/join.sh http://<COORDINATOR-IP>:8080 --relay-addrs <COORDINATOR-IP>:30300
```
Full guide: [`guides/provider_unix_guide.md`](guides/provider_unix_guide.md).

> **On the same Wi-Fi or ZeroTier network as the coordinator?** You can leave out the relay;
> providers then accept connections directly. A phone always needs the relay on ZeroTier: Android
> does not deliver incoming VPN connections to apps.

---

## 3. Store a file

The **client** encrypts on your machine, so the network only ever holds noise. Download it from
section 1 (or `go build ./cmd/client`). Below, `client` means the file you downloaded.

```bash
URL=http://<COORDINATOR-IP>:8080
DIR="$HOME/.vyomanaut-owner"

client register --mode=demo --microservice-url=$URL --data-dir=$DIR      # asks for your phone number, the join code and a passphrase
client deposit  --mode=demo --microservice-url=$URL --data-dir=$DIR --amount-paise=1000000   # ledger credit (₹10,000 in the demo)
client upload   --mode=demo --microservice-url=$URL --data-dir=$DIR ./my-photo.jpg
client ls       --mode=demo --microservice-url=$URL --data-dir=$DIR       # shows the file id
client retrieve --mode=demo --microservice-url=$URL --data-dir=$DIR -o ./back.jpg <file_id>
```

Keep the **recovery phrase** `register` prints: it is the only way to recover your files on a new
device. Nobody, including us, can reset it.

### Pay in real test USDT (Whitechain)

The ledger deposit above is enough to try storage. To fund the escrow on chain instead, use
`vyomachain` (section 1). Your wallet needs test USDT
(`0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7`) and a little WBT for gas from the
[Whitechain faucet](https://faucet.testnet.whitechain.io).

```bash
export VYOMANAUT_CHAIN_RPC_URL=https://rpc.testnet.whitechain.io
export VYOMANAUT_CHAIN_API_URL=http://<COORDINATOR-IP>:8090        # tells vyomachain which contracts

vyomachain wallet new  --out ~/vyoma-keys/me.key                    # or use your own key file (32 hex bytes)
vyomachain wallet bind --api $VYOMANAUT_CHAIN_API_URL --data-dir $DIR --key-file ~/vyoma-keys/me.key
vyomachain deposit --amount 1 --key-file ~/vyoma-keys/me.key        # prints the explorer link
vyomachain balance --address <your wallet>
```

---

## 4. Run your own network

Anyone can be a Vyomanaut **coordinator**: the machine that places pieces, runs audits, keeps the
ledger, and writes to the chains. Your network uses **your** keys, **your** wallets and **your**
contracts. Nothing of ours is needed.

### 4.1 What you need

- A machine people can reach: a cloud VM with a public IP (free options in 4.7), or a laptop on a
  shared Wi-Fi or ZeroTier network.
- Linux or macOS with **Go 1.26+**, **Docker** and the **psql** client. Windows works too with
  `up.ps1` (see `guides/operator_windows_guide.md`).
- **No RocksDB:** `eval "$(scripts/release/purego_env.sh)"` makes every build use the pure-Go
  storage engine.

```bash
git clone https://github.com/vyomanaut-labs/Vyomanaut_For_ETH.git
cd Vyomanaut_For_ETH
eval "$(scripts/release/purego_env.sh)"
go build ./...
```

### 4.2 Your wallets

Make three wallets (key files are 32 hex bytes; keep them in `~/vyoma-keys`, `chmod 600`,
**never** commit or share them):

```bash
go run ./cmd/vyomachain wallet new --out ~/vyoma-keys/deployer.key   # coordinator: posts payouts, pays gas
go run ./cmd/vyomachain wallet new --out ~/vyoma-keys/guardian.key   # guardian: can veto a wrong payout list
go run ./cmd/vyomachain wallet new --out ~/vyoma-keys/arkiv.key      # Arkiv publisher (a separate wallet)
```

Fund them (free test tokens):
- **Coordinator and guardian:** WBT from the [Whitechain faucet](https://faucet.testnet.whitechain.io) (2 WBT a day per GitHub account).
- **Arkiv publisher:** tGLM from the faucet at [hub.arkiv.network](https://hub.arkiv.network).

### 4.3 Your contracts (Whitechain)

The escrow fixes its coordinator and guardian at deployment, so your network deploys its own. With
[Foundry](https://getfoundry.sh):

```bash
cd contracts
export WHITECHAIN_SEPOLIA_RPC_URL=https://rpc.testnet.whitechain.io
export COORDINATOR_ADDR=$(go run ../cmd/vyomachain wallet show --key-file ~/vyoma-keys/deployer.key)
export GUARDIAN_ADDR=$(go run ../cmd/vyomachain wallet show --key-file ~/vyoma-keys/guardian.key)
forge script script/Deploy.s.sol:Deploy --rpc-url whitechain_sepolia \
  --private-key "$(cat ~/vyoma-keys/deployer.key)" --broadcast
cd ..
```

Copy `deployments/whitechain/sepolia.json` to, say, `deployments/whitechain/mynet.json`, put your
`vyomaEscrow`, `auditAnchor`, `coordinator`, `guardian` and `deploymentBlock` in it, and verify
the contracts (commands in [`contracts/README.md`](contracts/README.md)).

*Just trying it out?* Skip this and leave the chain off. Storage, audits and the ledger run the
same without it.

### 4.4 Start the network

```bash
# Postgres (once)
docker compose -f deployments/dev/docker-compose.yml up -d postgres

# This machine's address that providers will dial (public IP, LAN IP or ZeroTier IP)
export MY_IP=<your address>

# Chains (each is optional)
export VYOMANAUT_CHAIN_DEPLOYMENT_FILE=deployments/whitechain/mynet.json
export VYOMANAUT_CHAIN_PRIVATE_KEY_FILE=~/vyoma-keys/deployer.key
eval "$(scripts/demo/chain_env.sh)"
export VYOMANAUT_ARKIV_APP_TAG=mynet                 # your network's own label on Arkiv
eval "$(scripts/demo/arkiv_env.sh)"                   # uses ~/vyoma-keys/arkiv.key

# Coordinator, plus 7 providers on this machine so the network is ready at once
eval "$(scripts/release/purego_env.sh)"
scripts/demo/up.sh --providers 7 --advertise-host "$MY_IP" --advertise-addr "$MY_IP"
```

In a second terminal, start the **relay** that phones and home PCs connect through:

```bash
go run ./cmd/relay --listen :30300
```

`up.sh` prints `MICROSERVICE_URL` (share it) and the values for later terminals
(`source /tmp/vyomanaut-demo/env`). After about ten minutes the network is **READY**. Watch it live:

```bash
source /tmp/vyomanaut-demo/env
$BIN_DIR/operator watch --mode=demo --microservice-url=$MICROSERVICE_URL --admin-api-key=$ADMIN_API_KEY
```

Check the chains woke up:
```bash
grep -E "EXTENSION|CHAIN|ARKIV" /tmp/vyomanaut-demo/logs/microservice.log
```

### 4.5 Ports to open

| Port | For |
| --- | --- |
| 8080 | the coordinator API (providers, owners, the app) |
| 8081–8087 | the 7 providers `up.sh` starts on this machine |
| 8090 | the chain API (owners binding wallets, the app's Payments tab) |
| 30300 | the relay (phones and PCs behind routers) |

### 4.6 Let people join

Share `http://<MY_IP>:8080`. When someone registers, read them their code:

```bash
$BIN_DIR/operator otp --mode=demo --otp-delivery-log=$OTP_LOG <their phone number>
```

### 4.7 Host it for free

Everything on chain is free: both chains are test networks and their faucets give tokens. The only
thing to host is the coordinator, which needs a reachable address.

| Option | Cost | Good for |
| --- | --- | --- |
| **Oracle Cloud "Always Free"** Ampere VM (up to 4 cores, 24 GB, public IP) | free (card check at sign-up) | a public network anyone can join from anywhere |
| **Google Cloud e2-micro** (1 GB) | free tier | the coordinator alone; put providers elsewhere |
| **A laptop + ZeroTier** (free plan) | free | a room or a campus; phones still use the relay |

On a cloud VM (Ubuntu):

1. Open the ports in section 4.5 in the cloud firewall, **and** in the VM:
   `sudo iptables -I INPUT -p tcp -m multiport --dports 8080:8090,30300 -j ACCEPT`.
2. Let the VM reach its own public address (cloud NAT often cannot loop back):
   `sudo ip addr add <PUBLIC-IP>/32 dev lo`.
3. Install Go, Docker and psql, then follow 4.1 to 4.4 with `MY_IP=<PUBLIC-IP>`. Run `up.sh` and the
   relay inside `tmux` so they survive logging out.

> The demo network speaks plain HTTP. Fine for test networks; put TLS in front before real data.

---

## The on-chain layer

Storage stays off-chain because that is fast and cheap. The chains do the two jobs an operator
should not be trusted with: **paying** and **remembering**.

| Trust question | Answer | Where |
| --- | --- | --- |
| Will providers get paid? | Owners deposit USDT into `VyomaEscrow`. The coordinator posts each period's payouts. After a 120-second window in which a guardian can veto, **anyone** can claim, and the money always goes to the provider | Whitechain Sepolia |
| What if the operator vanishes? | Posted payouts stay claimable by anyone; after 15 minutes of silence owners can take back unspent credit | Whitechain Sepolia |
| Can the operator rewrite audit history? | Every 2 minutes a Merkle root of new audit receipts goes to `AuditAnchor` | Whitechain Sepolia |
| Can anyone read and check the history? | Each provider's receipts per period, signed, as queryable entities; file manifests (hashes only) | Arkiv (Rabdi) |

**Check it yourself, without our database:**

```bash
go run ./cmd/arkivcheck public                                  # reads Arkiv, verifies every entity

export VYOMANAUT_CHAIN_RPC_URL=https://rpc.testnet.whitechain.io
export VYOMANAUT_CHAIN_ESCROW_ADDRESS=0xBcE841C78c6c40C551fc223Ba9415D4751Af5958
go run ./cmd/vyomachain period 1 --account <provider wallet>    # what a payout period holds
go run ./cmd/vyomachain claim --period 1 --account <provider wallet> --key-file <any wallet>   # pays the provider, not you
```

| Item | Value |
| --- | --- |
| Whitechain Sepolia | chain 1874, [explorer](https://explorer.testnet.whitechain.io) |
| Escrow (verified) | [`0xBcE841C7…1Af5958`](https://explorer.testnet.whitechain.io/address/0xBcE841C78c6c40C551fc223Ba9415D4751Af5958) |
| Audit anchor (verified) | [`0x1b2b623e…34FDB3d`](https://explorer.testnet.whitechain.io/address/0x1b2b623ecA0A7899C6a24eaf401a99d2434FDB3d) |
| Test USDT | `0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7` |
| Arkiv | Rabdi testnet, chain 7794095, [explorer](https://rabdi.explorer.arkiv.network) |
| Arkiv query | `app = str('vyomanaut-demo') AND kind = str('audit_batch')` |

**CROPS.**
- **C, censorship resistance (new this weekend):** posted payouts and refunds need no permission from us.
- **O, open:** free licence, verified contracts, and any operator can run a network (section 4).
- **P, private:** files are encrypted on the owner's device, and no owner, file name, size or chunk list goes on chain.
- **S, secure:** the coordinator key is bounded by owners' deposits, a veto window and a separate guardian. The contracts are tested, but unaudited.

Details: [`docs/WHITECHAIN.md`](docs/WHITECHAIN.md), [`docs/ARKIV.md`](docs/ARKIV.md),
[`contracts/README.md`](contracts/README.md).

---

## 6. How it works

```
 owner's device                       coordinator (anyone can run one)            providers
 ──────────────                       ───────────────────────────────            ─────────
 encrypt + split (RS 3-of-5) ──────►  places pieces for diversity     ─────────► phones, PCs
 recovery phrase stays here           audits every 2 min, repairs     ◄───────── signed answers
                                      keeps the ledger (Postgres)
                                          │                │
                       Whitechain: escrow, payouts,   Arkiv: readable,
                       audit fingerprints             signed audit history
```

- **Encryption:** files are encrypted before they leave your device; providers hold only ciphertext.
- **Erasure coding:** any 3 of a file's 5 pieces rebuild it.
- **Audits:** the coordinator challenges every provider about every two minutes; each answer is signed.
- **Repair:** when a provider leaves, its pieces are rebuilt elsewhere.
- **Reaching phones:** Android blocks incoming VPN connections to apps, and mobile data sits behind
  carrier NAT. So providers keep an outgoing connection to a **relay** (`cmd/relay`), and the
  coordinator reaches them through it, with TLS end to end.

---

## 7. Limits, said plainly

- **Test networks only.** The coins have no value; the contracts are unaudited.
- **Getting a file back still needs a coordinator.** The chains protect money and history, not
  retrieval.
- **Demo payout wallets are custodial:** the coordinator derives them. A real provider would bring
  their own.
- **Arkiv proves history did not change after publishing,** not that it was true when published.
  PASS and FAIL receipts carry the provider's own signature.
- **Join codes are read out by the operator,** which does not scale past a demo.
- **Plain HTTP** on the demo network.
- **No token, and there will not be one.**

---

## 8. Repository, docs and team

```
android/      the Android provider app (Kotlin, Jetpack Compose)
cmd/          coordinator (microservice), provider, client, relay, vyomachain, arkivcheck, verifyreceipt, operator
contracts/    VyomaEscrow and AuditAnchor (Solidity, Foundry)
internal/     the core: crypto, erasure coding, audit, payment, p2p and relay, chain, arkiv
migrations/   database schema
scripts/      demo (up/join/down), release builds, CI checks
guides/       step-by-step operator, provider and presenter guides
docs/         Whitechain and Arkiv design notes
```

Guides: [operator (macOS/Linux)](guides/operator_unix_guide.md) ·
[operator (Windows)](guides/operator_windows_guide.md) ·
[on-chain finale](guides/operator_eth_guide.md) · [provider (macOS/Linux)](guides/provider_unix_guide.md) ·
[provider (Windows)](guides/provider_windows_guide.md) · [Android app](android/README.md)

Before ETHGlobal Mumbai the project was the V2 demo (commit `086be48`, tag `pre-ethglobal-mumbai`):
storage, audits, repair and the ledger, tested on 10 machines with more than 73,000 audits and no
integrity failures ([demo report](https://drive.google.com/drive/folders/1tCk9fueHt9Np4GcJYck9dJC2D98Lb1Y6?usp=drive_link)).
The escrow, the audit anchor and mirror, the verifiers, the Android app and the relay were added
during the hackathon.

**Team:** Aryan Singh ([@masamasaowl](https://github.com/masamasaowl)) and Amaresh Singh
([@amar641](https://github.com/amar641)).
[YouTube](https://www.youtube.com/@vyomanaut-labs) ·
[LinkedIn](https://www.linkedin.com/company/vyomanaut-labs) ·
[Vyomanaut V1](https://github.com/vyomanaut-labs/Vyomanaut)

**How we used AI:** Claude helped write code, tests and guides; we reviewed, ran and decided.
