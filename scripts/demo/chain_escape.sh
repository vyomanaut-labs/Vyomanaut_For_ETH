#!/usr/bin/env bash
# scripts/demo/chain_escape.sh
#
# The escape-hatch demo: the coordinator disappears right after a payout period
# was posted on chain, and a stranger still gets the provider paid.
#
#   scripts/demo/chain_escape.sh          stop the coordinator, wait out the window, claim
#   scripts/demo/chain_escape.sh --dry    only show what it would do
#
# Run it right after the coordinator log shows "[CHAIN] period N posted". You
# have about two minutes: the coordinator claims by itself when the 120-second
# challenge window ends, and then there is nothing left to demonstrate.
#
# It stops ONLY the coordinator (the first process in the demo's pid file), not
# Postgres and not the providers, then claims with vyomachain from a wallet that
# has nothing to do with the coordinator. Needs, in the environment:
#   VYOMANAUT_CHAIN_RPC_URL, VYOMANAUT_CHAIN_ESCROW_ADDRESS
# and a key file for the stranger (any wallet with a little WBT):
#   ESCAPE_KEY_FILE   (default: ~/vyoma-keys/owner.key)
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." >/dev/null 2>&1 && pwd)"
cd -- "$REPO_ROOT"

STATE_DIR="${VYOMANAUT_DEMO_STATE_DIR:-/tmp/vyomanaut-demo}"
BIN_DIR="$STATE_DIR/bin"
LOG="$STATE_DIR/logs/microservice.log"
PID_FILE="$STATE_DIR/pids"
KEY_FILE="${ESCAPE_KEY_FILE:-$HOME/vyoma-keys/owner.key}"

PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGDATABASE="${PGDATABASE:-vyomanaut_demo}"
PGMIGRATORUSER="${PGMIGRATORUSER:-vyomanaut_migrator}"
PGMIGRATORPASSWORD="${PGMIGRATORPASSWORD:-devpass}"

DRY=0
[[ "${1:-}" == "--dry" ]] && DRY=1

log() { echo "[escape] $*"; }
die() { echo "[escape] $*" >&2; exit 1; }

command -v psql >/dev/null 2>&1 || die "psql is not on PATH"
command -v go >/dev/null 2>&1 || die "go is not on PATH"
[[ -n "${VYOMANAUT_CHAIN_RPC_URL:-}" ]] || die "VYOMANAUT_CHAIN_RPC_URL is not set"
[[ -n "${VYOMANAUT_CHAIN_ESCROW_ADDRESS:-}" ]] || die "VYOMANAUT_CHAIN_ESCROW_ADDRESS is not set"
[[ -f "$LOG" ]] || die "no coordinator log at $LOG: is the demo network up?"
[[ -f "$KEY_FILE" ]] || die "no key file at $KEY_FILE (set ESCAPE_KEY_FILE to a wallet with a little WBT)"

mkdir -p "$BIN_DIR"
go build -o "$BIN_DIR/vyomachain" ./cmd/vyomachain
chain() { "$BIN_DIR/vyomachain" "$@"; }
admin() {
  PGPASSWORD="$PGMIGRATORPASSWORD" psql -v ON_ERROR_STOP=1 -At \
    -h "$PGHOST" -p "$PGPORT" -U "$PGMIGRATORUSER" -d "$PGDATABASE" "$@"
}

# The newest period the coordinator posted, and when it becomes claimable.
POSTED="$(grep -E '\[CHAIN\] period [0-9]+ posted' "$LOG" | tail -n1 || true)"
[[ -n "$POSTED" ]] || die "the coordinator has not posted a period yet: wait for a '[CHAIN] period N posted' line"
PERIOD="$(sed -n 's/.*period \([0-9][0-9]*\) posted.*/\1/p' <<<"$POSTED")"
if grep -qE "\[CHAIN\] period $PERIOD claimed" "$LOG"; then
  die "the coordinator already claimed period $PERIOD itself; wait for the next 'posted' line and be quicker"
fi

# Ask the chain, not just the log: the coordinator may have been stopped earlier,
# or the period may already be paid.
VIEW="$(chain period "$PERIOD" 2>&1 || true)"
if grep -q 'fully claimed' <<<"$VIEW"; then
  die "period $PERIOD is already fully claimed on chain. Upload another file and wait for the next 'posted' line."
fi
if grep -q 'never posted' <<<"$VIEW"; then
  die "the chain has no period $PERIOD: check VYOMANAUT_CHAIN_ESCROW_ADDRESS and VYOMANAUT_CHAIN_RPC_URL match the running coordinator"
fi
if grep -q 'cancelled' <<<"$VIEW"; then
  die "period $PERIOD was cancelled by the guardian; there is nothing to claim"
fi
log "period $PERIOD is posted and not yet paid"

if (( DRY )); then
  log "dry run: would stop the coordinator, wait for the window, and claim period $PERIOD for:"
  admin -F ' ' -c "SELECT '0x'||encode(wallet,'hex'), amount_micro FROM chain_period_payouts WHERE period = $PERIOD ORDER BY 1;"
  exit 0
fi

PID="$(head -n1 "$PID_FILE" 2>/dev/null || true)"
[[ -n "$PID" ]] || die "no coordinator process id in $PID_FILE"
if kill -0 "$PID" 2>/dev/null; then
  log "1/4  stopping the coordinator (process $PID); providers and Postgres keep running"
  kill "$PID" || die "could not stop process $PID"
  sleep 1
  if kill -0 "$PID" 2>/dev/null; then die "the coordinator is still running"; fi
  log "     the coordinator is gone"
else
  log "1/4  the coordinator is already stopped"
fi

log "2/4  waiting for the challenge window to end (anyone can claim after it)"
for _ in $(seq 1 90); do
  VIEW="$(chain period "$PERIOD" 2>&1 || true)"
  if grep -q 'claimable now' <<<"$VIEW"; then break; fi
  sleep 3
done
echo "$VIEW"
grep -q 'claimable now' <<<"$VIEW" || die "the period did not become claimable in time"

log "3/4  a stranger claims for each provider; the money goes to the provider, not to the stranger"
admin -F ' ' -c "SELECT '0x'||encode(wallet,'hex') FROM chain_period_payouts WHERE period = $PERIOD ORDER BY 1;" |
while read -r WALLET; do
  [[ -n "$WALLET" ]] || continue
  chain claim --period "$PERIOD" --account "$WALLET" --key-file "$KEY_FILE"
done

log "4/4  the providers now hold their USDT:"
admin -F ' ' -c "SELECT '0x'||encode(wallet,'hex') FROM chain_period_payouts WHERE period = $PERIOD ORDER BY 1;" |
while read -r WALLET; do
  [[ -n "$WALLET" ]] || continue
  chain balance --address "$WALLET" | sed -n '1p;3p'
done
log "done. Restart the demo with scripts/demo/down.sh and up.sh when you are finished."
