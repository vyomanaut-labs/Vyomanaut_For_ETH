#!/usr/bin/env bash
# scripts/demo/host.sh — host a Vyomanaut network from this Mac or Linux machine with one command.
#
#   scripts/demo/host.sh --name "ETHGlobal demo"
#
# Starts Postgres (Docker), the relay, two free public tunnels (bore.pub for
# the relay, a Cloudflare quick tunnel for the coordinator), the coordinator
# with local providers (up.sh), then opens the QR screen and your join desk.
#
#   --local       no tunnels: only devices on this Wi-Fi can join
#   --chain-api   also give the chain API (port 8090) a public address
#   --providers N local providers to start (default 7)
#   --stop        stop everything
#
# Needs Go, Docker and psql (README 4.1), plus cloudflared and bore:
#   macOS: brew install cloudflared bore-cli
#   Linux: see https://github.com/cloudflare/cloudflared and https://github.com/ekzhang/bore
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_ROOT"
HOST_DIR="${TMPDIR:-/tmp}/vyomanaut-host"
LOG_DIR="$HOST_DIR/logs"
PID_FILE="$HOST_DIR/pids"

NAME="Vyomanaut network"; PROVIDERS=7; LOCAL=0; CHAIN=0; STOP=0; PORT=8080; RELAY_PORT=30300
while [[ $# -gt 0 ]]; do
  case "$1" in
    --name) NAME="$2"; shift 2 ;;
    --providers) PROVIDERS="$2"; shift 2 ;;
    --local) LOCAL=1; shift ;;
    --chain-api) CHAIN=1; shift ;;
    --stop) STOP=1; shift ;;
    -h | --help) sed -n 2,20p "$0"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done

say() { printf '\033[1;36m[host]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[host]\033[0m %s\n' "$*" >&2; exit 1; }

stop_hosted() {
  if [[ -f "$PID_FILE" ]]; then
    while read -r p; do [[ -n "$p" ]] && kill "$p" 2>/dev/null || true; done <"$PID_FILE"
    rm -f "$PID_FILE"
  fi
  "$SCRIPT_DIR/down.sh" || true
}
if [[ $STOP -eq 1 ]]; then stop_hosted; say "Stopped."; exit 0; fi

mkdir -p "$LOG_DIR"
[[ -f "$PID_FILE" ]] && { say "Stopping the previous run first."; stop_hosted; }
: >"$PID_FILE"

start_bg() { # log-name, command...
  local log="$LOG_DIR/$1"; shift
  "$@" >"$log" 2>&1 &
  echo $! >>"$PID_FILE"
  echo "$log"
}
wait_for() { # log, pattern, what
  for _ in $(seq 1 90); do
    if m="$(grep -Eo "$2" "$1" | head -n1)" && [[ -n "$m" ]]; then echo "$m"; return 0; fi
    sleep 0.5
  done
  fail "$3 did not start. Log: $1"
}
need() { command -v "$1" >/dev/null 2>&1 || fail "$1 is not installed. $2"; }

need go "See README 4.1."
need docker "See README 4.1."
need psql "See README 4.1."
docker info >/dev/null 2>&1 || fail "Docker is not running."

say "Starting Postgres in Docker"
docker compose -f deployments/dev/docker-compose.yml up -d --wait postgres

say "Starting the relay on port $RELAY_PORT"
eval "$(scripts/release/purego_env.sh)"
go build -o "$HOST_DIR/relay" ./cmd/relay
start_bg relay.log "$HOST_DIR/relay" --listen ":$RELAY_PORT" >/dev/null

if command -v ip >/dev/null 2>&1; then
  LAN_IP="$(ip route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="src"){print $(i+1); exit}}')"
else
  LAN_IP="$(ipconfig getifaddr en0 2>/dev/null || true)"
fi
LAN_IP="${LAN_IP:-127.0.0.1}"
unset VYOMANAUT_PUBLIC_URL VYOMANAUT_PUBLIC_RELAY VYOMANAUT_PUBLIC_CHAIN_API

if [[ $LOCAL -eq 1 ]]; then
  PUBLIC_URL="http://$LAN_IP:$PORT"
  PUBLIC_RELAY="$LAN_IP:$RELAY_PORT"
else
  need cloudflared "macOS: brew install cloudflared"
  need bore "macOS: brew install bore-cli"
  say "Opening a public address for the relay (bore.pub)"
  log="$(start_bg bore.log bore local "$RELAY_PORT" --to bore.pub)"
  PUBLIC_RELAY="$(wait_for "$log" 'bore\.pub:[0-9]+' 'The bore tunnel')"
  say "Opening a public HTTPS address for the coordinator (Cloudflare)"
  log="$(start_bg cloudflared.log cloudflared tunnel --no-autoupdate --url "http://localhost:$PORT")"
  PUBLIC_URL="$(wait_for "$log" 'https://[a-z0-9-]+\.trycloudflare\.com' 'The Cloudflare tunnel')"
  export VYOMANAUT_PUBLIC_URL="$PUBLIC_URL" VYOMANAUT_PUBLIC_RELAY="$PUBLIC_RELAY"
  # up.sh's own providers stay reachable through the same relay.
  export VYOMANAUT_RELAY_ADDRS="$PUBLIC_RELAY"
  if [[ $CHAIN -eq 1 ]]; then
    log="$(start_bg cloudflared-chain.log cloudflared tunnel --no-autoupdate --url http://localhost:8090)"
    export VYOMANAUT_PUBLIC_CHAIN_API="$(wait_for "$log" 'https://[a-z0-9-]+\.trycloudflare\.com' 'The chain API tunnel')"
  fi
fi

export VYOMANAUT_NETWORK_NAME="$NAME" VYOMANAUT_DOWNLOADS_DIR="$REPO_ROOT/dist"
[[ -f dist/vyomanaut-provider-android.apk ]] || say "Note: dist/ has no app yet; downloads will come from GitHub releases."
say "Starting the coordinator and $PROVIDERS local providers (up.sh). This takes a few minutes."
scripts/demo/up.sh --providers "$PROVIDERS" --advertise-host "$LAN_IP"

# shellcheck disable=SC1091
source "${VYOMANAUT_DEMO_STATE_DIR:-/tmp/vyomanaut-demo}/env"
open_url() { (command -v open >/dev/null && open "$1") || (command -v xdg-open >/dev/null && xdg-open "$1") || true; }
open_url "$PUBLIC_URL/join/screen"
open_url "http://localhost:$PORT/join/desk#key=$ADMIN_API_KEY"

cat <<EOF

  $NAME is open.

  Join page (QR):  $PUBLIC_URL/join
  Big screen:      $PUBLIC_URL/join/screen
  Your join desk:  http://localhost:$PORT/join/desk   (admin key below; already opened for you)
  Relay:           $PUBLIC_RELAY
${VYOMANAUT_PUBLIC_CHAIN_API:+  Chain API:       $VYOMANAUT_PUBLIC_CHAIN_API
}
  Phones:   scan the QR, tap Download, then Open in the app.
  Laptops:  open $PUBLIC_URL/join and paste the one line it shows.
  Codes:    appear on your join desk; read them out.

  Admin key: $ADMIN_API_KEY
  Stop everything: scripts/demo/host.sh --stop
EOF
