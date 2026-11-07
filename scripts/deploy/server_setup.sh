#!/usr/bin/env bash
# scripts/deploy/server_setup.sh: get a fresh Ubuntu/Debian VM ready to host a Vyomanaut network.
#
#   sudo bash scripts/deploy/server_setup.sh --ip 203.0.113.7 --sslip     # https://203-0-113-7.sslip.io
#   sudo bash scripts/deploy/server_setup.sh --ip 203.0.113.7 --domain demo.example.com
#   sudo bash scripts/deploy/server_setup.sh --ip 203.0.113.7             # plain http://IP:8080
#
# It installs Docker, Go (the version go.mod asks for), psql and git, puts a Caddy proxy in front of
# the coordinator when you give a name (a free Let's Encrypt certificate, no account), and opens
# only the ports the network needs. Run it from a clone of the repo. Then follow guides/deploy_server.md.
#
#   --ip IP          this machine's PUBLIC IPv4 address (required)
#   --domain NAME    https name that already points at --ip
#   --sslip          use <ip-with-dashes>.sslip.io as the name (free, no DNS to set up)
#   --no-firewall    do not touch ufw (use it if your cloud's own firewall already does this job)
set -euo pipefail

IP=""; DOMAIN=""; SSLIP=0; FIREWALL=1
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ip) IP="$2"; shift 2 ;;
    --domain) DOMAIN="$2"; shift 2 ;;
    --sslip) SSLIP=1; shift ;;
    --no-firewall) FIREWALL=0; shift ;;
    -h | --help) sed -n 2,16p "$0"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done

say() { printf '\033[1;36m[setup]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[setup]\033[0m %s\n' "$*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || fail "Run with sudo."
[[ "$IP" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "--ip must be this machine's public IPv4 address."
[[ $SSLIP -eq 1 && -z "$DOMAIN" ]] && DOMAIN="${IP//./-}.sslip.io"
[[ -z "$DOMAIN" || "$DOMAIN" =~ ^[A-Za-z0-9.-]+$ ]] || fail "--domain is not a plain host name."
command -v apt-get >/dev/null || fail "This script supports Ubuntu and Debian (apt)."

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
GO_VERSION="$(awk '/^go /{print $2; exit}' "$REPO_ROOT/go.mod")"
[[ -n "$GO_VERSION" ]] || fail "Could not read the Go version from go.mod."
case "$(uname -m)" in x86_64) ARCH=amd64 ;; aarch64 | arm64) ARCH=arm64 ;; *) fail "Unsupported CPU $(uname -m)." ;; esac

say "Installing Docker, git, psql and a C toolchain"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq ca-certificates curl git build-essential postgresql-client docker.io ufw >/dev/null
apt-get install -y -qq docker-compose-v2 >/dev/null 2>&1 || apt-get install -y -qq docker-compose-plugin >/dev/null 2>&1 \
  || fail "Could not install the Docker Compose plugin. Install it from https://docs.docker.com/engine/install/ and re-run."
systemctl enable --now docker >/dev/null 2>&1 || true

if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "go$GO_VERSION "; then
  say "Installing Go $GO_VERSION (official tarball from go.dev)"
  tmp="$(mktemp -d)"
  curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o "$tmp/go.tgz" || fail "Could not download Go $GO_VERSION."
  rm -rf /usr/local/go && tar -C /usr/local -xzf "$tmp/go.tgz" && rm -rf "$tmp"
fi
printf 'export PATH=$PATH:/usr/local/go/bin\n' >/etc/profile.d/go.sh

if [[ -n "$DOMAIN" ]]; then
  say "Starting the Caddy proxy: https://$DOMAIN -> the coordinator on port 8080"
  docker rm -f vyomanaut-caddy >/dev/null 2>&1 || true
  docker run -d --name vyomanaut-caddy --restart unless-stopped --network host \
    -v vyomanaut-caddy-data:/data caddy:2 caddy reverse-proxy --from "$DOMAIN" --to localhost:8080 >/dev/null
fi

if [[ $FIREWALL -eq 1 ]]; then
  say "Opening only the needed ports (ssh first, so you cannot lock yourself out)"
  ufw allow OpenSSH >/dev/null
  ufw allow 30300/tcp >/dev/null        # the relay phones and laptops connect through
  ufw allow 8090/tcp >/dev/null         # the chain API (the app's Payments tab); harmless without chains
  if [[ -n "$DOMAIN" ]]; then ufw allow 80/tcp >/dev/null; ufw allow 443/tcp >/dev/null; else ufw allow 8080/tcp >/dev/null; fi
  ufw --force enable >/dev/null
fi

if [[ -n "${SUDO_USER:-}" ]]; then usermod -aG docker "$SUDO_USER"; fi

URL="http://$IP:8080"; WEBPORTS="8080"
if [[ -n "$DOMAIN" ]]; then URL="https://$DOMAIN"; WEBPORTS="80 and 443"; fi
cat <<DONE

[setup] Done. Log out and back in once (so the docker group applies), then, from the repo:

  eval "\$(scripts/demo/chain_env.sh)"     # only if the chain keys are in ~/vyoma-keys
  eval "\$(scripts/demo/arkiv_env.sh)"
  setsid nohup scripts/demo/host.sh --name "Vyomanaut" --server $IP ${DOMAIN:+--domain $DOMAIN }>~/host.log 2>&1 </dev/null &
  tail -f ~/host.log

Guests will join at: $URL/join
Also open these on your cloud's own firewall (security group): 22, 30300, 8090 and $WEBPORTS.
DONE
