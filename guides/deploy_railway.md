# Deploy the coordinator on Railway (no card, no tunnels)

The whole network runs as **one Railway service** (coordinator, relay and 5 local providers in one
container) plus **Railway's Postgres**. Guests join over the internet through a permanent
`https://….up.railway.app` address and a TCP proxy for the relay. No ZeroTier, Cloudflare or bore.

Railway's free trial needs no card: a one-time $5 credit for up to 30 days, 1 GB of RAM per service,
5 services per project (we use 2). Check the live limits at <https://docs.railway.com/pricing/free-trial>.

## 0. Before you start

1. **Push this repo's `main` to GitHub** (Railway builds from it).
2. **Make a GitHub release that carries the downloads.** `dist/` is not in git, so the join page
   would otherwise hand out the *old* app. On GitHub: Releases > Draft a new release > tag
   `demo-finals` > attach **`dist/vyomanaut-provider-android.apk`** and the
   `provider-linux-*`, `provider-macos-*`, `provider-windows-amd64.exe` files from `dist/` > Publish.
   The page then downloads from
   `https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/download/demo-finals`
   (you set that as `VYOMANAUT_RELEASE_BASE` below).
3. **Make an admin key** and keep it: `openssl rand -hex 32` (64 hex characters). It opens the join desk.

## 1. Create the project

1. <https://railway.com> > sign in with GitHub > **New Project** > **Deploy PostgreSQL**.
2. In the same project: **New** > **GitHub Repo** > pick `Vyomanaut_For_ETH`, branch `main`.
   Railway finds `railway.json` and builds the `Dockerfile` in the repo root (a few minutes).
   The first deploy will stop with a message about a missing domain. That is expected.

## 2. Variables of the app service (Variables tab)

| Variable | Value |
| --- | --- |
| `PGHOST` | `${{Postgres.PGHOST}}` |
| `PGPORT` | `${{Postgres.PGPORT}}` |
| `PGUSER` | `${{Postgres.PGUSER}}` |
| `PGPASSWORD` | `${{Postgres.PGPASSWORD}}` |
| `PORT` | `8080` |
| `VYOMANAUT_DEMO_ADMIN_KEY` | the 64-hex key from step 0 |
| `VYOMANAUT_NETWORK_NAME` | `Vyomanaut` |
| `VYOMANAUT_RELEASE_BASE` | the release URL from step 0 |

(Type `${{Postgres.PGHOST}}` exactly; Railway turns it into the database's private address.)

## 3. Open the two public doors (Settings > Networking of the app service)

1. **Generate Domain**, target port **8080**. This is the address guests use.
2. **TCP Proxy**, port **30300**. This is the relay phones and laptops connect through.

Then **Redeploy**. The log should end with `Vyomanaut is up` and the join page address.
The 5 providers then need about **10 to 12 minutes** to finish vetting (the desk's counters show
them turn active). Uploads work once all 5 are active, so start well before the presentation.

## 4. Check it (from a phone on mobile data)

| Check | Expected |
| --- | --- |
| `https://YOUR-DOMAIN/api/v1/network/info` | JSON; `relay_addrs` is the TCP proxy address |
| `https://YOUR-DOMAIN/join` on a phone, scan the QR on `/join/screen` | page loads, the app downloads, "Open in the app" fills in everything |
| Register in the app; the code shows on `https://YOUR-DOMAIN/join/desk` (paste the admin key) | phone shows ONLINE, desk shows "joined" |
| Desk > Store a file | upload and download work |

## 5. Point the public QR page at it (optional)

The address is permanent, so set it once:

```bash
scripts/demo/site_pointer.sh https://YOUR-DOMAIN
git add site/coordinator.json && git commit -m "site: point at Railway" && git push
```

## Chains (optional)

Add these variables to turn the on-chain features on. They are secrets: keep them only in Railway.

| Variable | Value |
| --- | --- |
| `CHAIN_PRIVATE_KEY` | the coordinator wallet's hex key (`deployer.key` contents) |
| `ARKIV_PRIVATE_KEY` | the Arkiv writer wallet's hex key |
| `ARKIV_ACCESS_KEY` | only if you use an Arkiv RPC access key |

The desk's **On chain** tab then shows live transactions. Limits on Railway: only one web port is
public, so the chain API (port 8090, used by `vyomachain wallet bind` and the app's Payments tab)
is not reachable from outside. The `verifyreceipt`, `arkivcheck` and `chain_escape.sh` commands also
need the database and the coordinator process, so run those from a laptop only on a laptop-hosted
network.

## If a deploy fails

| The log says | What to do |
| --- | --- |
| `No public domain yet` or `No TCP proxy yet` | Do step 3 (Settings > Networking), then **Redeploy**. Expected on the first try. |
| `Cannot reach Postgres` | Re-check the four `PG...` variables in step 2. The database service must be named exactly `Postgres`. |
| The build cannot find a Dockerfile | In **Settings > Build**, set the Dockerfile path to `Dockerfile`. |
| `Create container` fails with "The executable `npm` could not be found" | A wrong start command is saved in the dashboard. In **Settings > Deploy**, clear **Custom Start Command** (`railway.json` already sets the right one: `bash deployments/railway/start.sh`), then redeploy. |
| `microservice never became reachable` | The lines just below it show the coordinator's own error. If it says the admin key is not valid hex, fix `VYOMANAUT_DEMO_ADMIN_KEY`: exactly 64 characters, only 0-9 and a-f, no spaces or quotes (or delete the variable). |
| `WARNING: VYOMANAUT_DEMO_ADMIN_KEY is not 64 hex characters` | Your key was ignored and a temporary one is printed at the end of the log. Fix the variable and redeploy to get a stable key. |
| Not sure which version is running | The first log line is `[railway] build: commit ...`. Compare it with the newest commit on GitHub. If it is older, use **Ctrl+K > Deploy Latest Commit** (Redeploy rebuilds the old commit). |
| `Permission denied` on a `.sh` file during the build | A script lost its runnable flag in git (it happens when it is committed from Windows). Run `git update-index --chmod=+x <file>` and commit. |

## Things to know

- **Every restart resets the network.** The container's disk is wiped and `up.sh` recreates the
  database, so guests and files are gone after a redeploy or crash. Restart only before the show.
- **Memory is fine.** In a local test of the same image, the coordinator and 5 providers together
  used about 35 MB at start (the Railway trial gives 1 GB per service), and a 900 KB upload and
  download through the desk worked.
- **The $5 credit runs out.** Watch Usage; stop the service when you are not presenting.
- **It is a demo server:** demo mode, testnets, throwaway data.
- **Tested locally, not on Railway itself.** The image was built and run here against a plain
  Postgres with fake Railway address variables. The Railway-only parts (the domain, the TCP proxy,
  the `${{Postgres.…}}` variables, the free-trial rules) follow Railway's documentation but have not
  been run, so do a full dry run (deploy, then join from a phone) before the presentation.
