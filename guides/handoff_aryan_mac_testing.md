# Handoff: test the open network on Aryan's Mac

7 Nov 2026 · from Amar

## What this is

The branch `vyomanaut_open_network` (commit `24f98e1`, pushed to GitHub) lets anyone host a
Vyomanaut network from one laptop. Guests join by scanning a QR code. ZeroTier is no longer
needed; it stays as a fallback.

- One command (`scripts/demo/host.sh`) starts Postgres, the relay, the coordinator, 5 providers
  and two free public tunnels.
- A **QR screen** goes on the projector. Guests scan it, download the app, tap "Open in the app"
  and type only their phone number.
- A **join desk** page on the Mac shows each guest's join code. Read it out; they type it in.
- Laptops that cannot scan run one line from the join page instead.

Already verified on the Windows laptop (7 Nov, about 01:00 IST):

- Galaxy M34 on 4G joined through a public Cloudflare tunnel. The app opened from the QR page with
  everything filled in, took the code from the desk, and showed **ONLINE**.
- A laptop joined with the install script; the desk marked it "joined".
- Two Android bugs fixed: the app could not look up web names or check HTTPS certificates. The
  fixed app is in `dist/`.

**Never run on a Mac yet:** `host.sh` and the bore relay tunnel. Testing these is the point.

## One-time setup on the Mac

1. Get the branch:
   ```bash
   git fetch && git switch vyomanaut_open_network && git pull
   ```
2. Install the two free tunnel tools (no account needed):
   ```bash
   brew install cloudflared bore-cli
   ```
3. Unzip `dist.zip` from Amar in the repo root. `ls dist` must show
   `vyomanaut-provider-android.apk` directly, not a second `dist` folder. Without it the join page
   hands out the old app from GitHub, which cannot open from the QR or reach a tunnel.
4. Check the tools `host.sh` needs (Docker Desktop must be running):
   ```bash
   for t in docker go psql cloudflared bore; do command -v $t >/dev/null && echo "ok  $t" || echo "MISSING  $t"; done
   ```

## Start the network

Terminal 1 replaces the old `MY_IP` / `down.sh` / `up.sh --advertise-host` steps with one
`host.sh` line. Everything else is as before.

**Terminal 1**

```bash
cd Vyomanaut_For_ETH

# RocksDB, same as always (optional: without these the pure-Go engine is used)
export CGO_CFLAGS="-I$HOME/rocksdb/include"
export CGO_LDFLAGS="-L$HOME/rocksdb/lib -L$(brew --prefix)/lib -L$(brew --prefix snappy)/lib \
  -Wl,-rpath,$HOME/rocksdb/lib -Wl,-rpath,$(brew --prefix)/lib -Wl,-rpath,$(brew --prefix snappy)/lib \
  -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd"
export DYLD_LIBRARY_PATH="$HOME/rocksdb/lib:$(brew --prefix)/lib:$(brew --prefix snappy)/lib:$DYLD_LIBRARY_PATH"

# Fresh database (optional)
docker compose -f deployments/dev/docker-compose.yml down -v

# Chains, same as before
eval "$(scripts/demo/chain_env.sh)"
eval "$(scripts/demo/arkiv_env.sh)"

# Postgres, relay, tunnels, coordinator, 5 providers, QR screen, join desk
scripts/demo/host.sh --name "Vyomanaut" --providers 5
```

When it finishes it prints the public join URL and relay address, and opens two browser tabs:
the **QR screen** (projector) and the **join desk** (keep on the Mac). Do not restart `host.sh`
mid-demo: the tunnel addresses change and the QR stops working.

**Terminal 2** (unchanged)

```bash
source /tmp/vyomanaut-demo/env
"$BIN_DIR/operator" watch --mode=demo --microservice-url="$MICROSERVICE_URL" --admin-api-key="$ADMIN_API_KEY"
```

Backup to the desk: `"$BIN_DIR/operator" otp --mode=demo --otp-delivery-log="$OTP_LOG" "+91XXXXXXXXXX"`.

## Test checklist

Use at least one phone on mobile data (not the hall Wi-Fi), so the test really goes through the
tunnels. Write the result in the last column.

| # | Test | How | Expected | Result |
| --- | --- | --- | --- | --- |
| 1 | Host starts | `host.sh` as above | Summary with public join URL (`https://…trycloudflare.com/join`) and relay at `bore.pub:<port>`; two tabs open | |
| 2 | Local providers | Terminal 2 `operator watch` | 5 providers online | |
| 3 | Public info | Open `https://<tunnel>.trycloudflare.com/api/v1/network/info` on the phone | JSON with the trycloudflare coordinator and the `bore.pub` relay | |
| 4 | App download | Scan the QR, tap Download | APK (about 22 MB) downloads and installs | |
| 5 | Open in app | Tap "Open in the app" | "Join Vyomanaut" dialog with coordinator and relay filled in | |
| 6 | Phone join | Enter phone, Join, then Register | Number appears on the desk with a code, state "waiting" | |
| 7 | Code | Type the desk code in the app | App ONLINE; desk "joined"; `operator watch` lists the phone | |
| 8 | Relay over bore | Provider log in the app | Relay slot reserved via `bore.pub` (first test of this) | |
| 9 | Laptop join | On another laptop, run the line from the join page (Windows `irm … \| iex`, Mac/Linux `curl … \| sh`) | Asks phone + GB (10 or more), takes the desk code, provider runs | |
| 10 | Payments tab | Open Payments in the app (only if chains are on) | Loads from the tunnelled chain API | |
| 11 | Stop | `scripts/demo/host.sh --stop` | All processes and tunnels gone | |

## Troubleshooting

| Symptom | Likely cause | What to do |
| --- | --- | --- |
| `host.sh` stops early | A tool missing or Docker not running | Run the tool check above; start Docker Desktop |
| No `bore.pub` address | bore.pub down or blocked | Retry once; else the ZeroTier fallback below |
| Join page won't load on 4G | Cloudflare tunnel failed or changed | Take the new URL from the `host.sh` summary; reopen the QR screen |
| "Open in the app" does nothing | Old app installed | Uninstall, install the APK from the join page |
| App: name lookup or certificate error | Old app without the fix | Same as above |
| Laptop join refused | Storage under 10 GB | Enter 10 or more |
| Code rejected | Older than 10 minutes, or already used | Press Register again; read the new code |
| Desk asks for a password | Opened without the key link | Use the tab `host.sh` opened, or `ADMIN_API_KEY` from `/tmp/vyomanaut-demo/env` |

**Fallback if the Wi-Fi blocks tunnels** (old ZeroTier way, same pages and desk):

```bash
scripts/demo/host.sh --name "Vyomanaut" --providers 5 --local --advertise-host "$MY_IP"
```

`MY_IP` from `zerotier-cli` as before. Guests on ZeroTier or the same Wi-Fi open
`http://$MY_IP:8080/join`.

## Reference

| Page | For | Notes |
| --- | --- | --- |
| `/join` | Guests | Download app, open in app, laptop one-liners |
| `/join/screen` | Projector | Big QR + URL |
| `/join/desk` | Host only | Needs the admin key; masked phone, code, waiting / joined / expired |
| `/join/install.sh`, `/join/install.ps1` | Laptops | One-line join |
| `/api/v1/network/info` | App | Coordinator, relay, chain API, name as JSON |

- `scripts/demo/host.sh --help` lists the options.
- `guides/host_demo_day.md` is the short version; `README.md` section 4 has the full options.
- Stop everything: `scripts/demo/host.sh --stop`.
