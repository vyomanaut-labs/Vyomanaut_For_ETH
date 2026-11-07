# Deploy the coordinator on a real server

For when tunnels are not allowed. One cloud VM runs the whole network at a fixed public address:
the coordinator, the relay, Postgres and 7 providers. Phones and laptops join over the internet
through the relay, exactly as with tunnels. Nothing here uses ZeroTier, Cloudflare or bore.

## What you need (about 10 minutes)

1. **A VM** with a public IPv4 address: Ubuntu 22.04/24.04, 2 vCPU, 4 GB RAM, 20 GB disk. Any
   cloud works (Hetzner, DigitalOcean, AWS, GCP, Oracle free tier...). Create it yourself; keep
   its SSH key.
2. **Open these ports** in the cloud's own firewall (security group): `22`, `80`, `443`, `30300`,
   `8090`. (Without a name, open `8080` instead of 80 and 443.)
3. **Optional but recommended: a name** for https. Either your own domain pointing at the VM, or
   the free `<ip-with-dashes>.sslip.io` (the setup script can use it with `--sslip`). The Android
   app accepts plain http too, but a browser page on https cannot embed an http QR.
4. **The chain keys** (only for the on-chain tabs): copy `~/vyoma-keys/` to the VM yourself.
   Never paste a key into a chat or commit it.
   ```bash
   scp -r ~/vyoma-keys USER@VM_IP:~ && ssh USER@VM_IP 'chmod 600 ~/vyoma-keys/*'
   ```

## Steps (on the VM, over SSH)

```bash
sudo apt-get update && sudo apt-get install -y git
git clone https://github.com/vyomanaut-labs/Vyomanaut_For_ETH.git && cd Vyomanaut_For_ETH
sudo bash scripts/deploy/server_setup.sh --ip VM_IP --sslip
```

Log out and back in once. Copy the app the QR page hands out (from your laptop):

```bash
scp -r dist USER@VM_IP:~/Vyomanaut_For_ETH/
```

Then start the network (from the repo, on the VM). Skip the two `eval` lines for a no-chain run:

```bash
eval "$(scripts/demo/chain_env.sh)"
eval "$(scripts/demo/arkiv_env.sh)"
setsid nohup scripts/demo/host.sh --name "Vyomanaut" --server VM_IP --domain VM-IP-WITH-DASHES.sslip.io >~/host.log 2>&1 </dev/null &
tail -f ~/host.log
```

When the log prints "is open", visit `https://VM-IP-WITH-DASHES.sslip.io/join`. The join desk is at
`/join/desk`; the admin key is printed at the end of `~/host.log` (read it there, do not share it).
Give the key only to people who should run the desk.

The processes survive you logging out. They do **not** survive a reboot: run the `setsid nohup`
line again. Stop with `scripts/demo/host.sh --stop`.

## Point the public QR page at it

The address is now permanent, so set it once and the GitHub Pages page (`site/`) always works:

```bash
scripts/demo/site_pointer.sh https://VM-IP-WITH-DASHES.sslip.io
git add site/coordinator.json && git commit -m "site: point at the server" && git push
```

## Check it, from another machine and a phone on mobile data

| Check | Expected |
| --- | --- |
| `curl https://NAME/api/v1/network/info` | JSON with `coordinator_url` = your https name and a relay `IP:30300` |
| Scan the QR on a phone | join page loads, app downloads, Open in the app fills everything in |
| Phone registers, you read the code off the desk | phone shows ONLINE; desk shows "joined" |
| Desk, Store a file | upload and download work |

## Things to know

- **Free sslip.io names share Let's Encrypt's rate limit** with everyone using sslip.io. If Caddy
  cannot get a certificate (`docker logs vyomanaut-caddy`), use your own domain, or fall back to
  plain `http://IP:8080` by leaving out `--domain`.
- **This is a demo server.** The coordinator runs in demo mode, the database uses the dev
  password but listens on the VM only, and the contracts and chains are testnets. Do not put
  real data or real money on it.
- **Untested so far:** these scripts were written and syntax-checked but have not run on a real
  VM yet. Do one full dry run (setup, start, join from a phone) well before the presentation.
