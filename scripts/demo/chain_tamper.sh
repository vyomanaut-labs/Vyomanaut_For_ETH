#!/usr/bin/env bash
# scripts/demo/chain_tamper.sh
#
# The tamper demo for audit anchoring: change one stored audit receipt the way
# an operator with database rights could, then show that verifyreceipt catches
# it, because the root on the chain no longer matches the database.
#
#   scripts/demo/chain_tamper.sh            show the honest check, tamper, check again
#   scripts/demo/chain_tamper.sh --restore  put the receipt back (repeat the demo)
#
# Needs the network started by scripts/demo/up.sh with anchoring on, at least
# one anchored epoch (a "[ANCHOR] epoch N anchored" line in the coordinator log),
# and these in the environment (the same ones the coordinator was started with):
#   VYOMANAUT_CHAIN_RPC_URL, VYOMANAUT_CHAIN_ANCHOR_ADDRESS
#
# The tamper flips one boolean (jit_flag) on the last receipt of the newest
# anchored epoch. That field is part of the receipt's fingerprint, the row stays
# valid for every database constraint, and flipping it again restores it.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." >/dev/null 2>&1 && pwd)"
cd -- "$REPO_ROOT"

STATE_DIR="${VYOMANAUT_DEMO_STATE_DIR:-/tmp/vyomanaut-demo}"
BIN_DIR="$STATE_DIR/bin"
STATE_FILE="$STATE_DIR/tamper.state"

PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGDATABASE="${PGDATABASE:-vyomanaut_demo}"
PGMIGRATORUSER="${PGMIGRATORUSER:-vyomanaut_migrator}"
PGMIGRATORPASSWORD="${PGMIGRATORPASSWORD:-devpass}"
PGAPPPASSWORD="${PGAPPPASSWORD:-devpass}"

log() { echo "[tamper] $*"; }
die() { echo "[tamper] $*" >&2; exit 1; }

command -v psql >/dev/null 2>&1 || die "psql is not on PATH"
command -v go >/dev/null 2>&1 || die "go is not on PATH"
[[ -n "${VYOMANAUT_CHAIN_RPC_URL:-}" ]] || die "VYOMANAUT_CHAIN_RPC_URL is not set"
[[ -n "${VYOMANAUT_CHAIN_ANCHOR_ADDRESS:-}" ]] || die "VYOMANAUT_CHAIN_ANCHOR_ADDRESS is not set"

# As the database owner: the superuser sees past the append-only policies, which
# is exactly the access an operator-level attacker has.
admin() {
  PGPASSWORD="$PGMIGRATORPASSWORD" psql -v ON_ERROR_STOP=1 -At \
    -h "$PGHOST" -p "$PGPORT" -U "$PGMIGRATORUSER" -d "$PGDATABASE" "$@"
}

mkdir -p "$BIN_DIR"
log "building verifyreceipt"
go build -o "$BIN_DIR/verifyreceipt" ./cmd/verifyreceipt

verify() {
  PGUSER=vyomanaut_app PGPASSWORD="$PGAPPPASSWORD" PGHOST="$PGHOST" PGPORT="$PGPORT" PGDATABASE="$PGDATABASE" \
    "$BIN_DIR/verifyreceipt" "$@"
}

flip() { # flip <receipt_id>
  admin -c "UPDATE audit_receipts SET jit_flag = NOT jit_flag WHERE receipt_id = '$1'::uuid;" >/dev/null
}

if [[ "${1:-}" == "--restore" ]]; then
  [[ -f "$STATE_FILE" ]] || die "nothing to restore: no tamper recorded in $STATE_FILE"
  RECEIPT="$(cat "$STATE_FILE")"
  flip "$RECEIPT"
  rm -f "$STATE_FILE"
  log "restored receipt $RECEIPT"
  verify epoch latest || true
  exit 0
fi

[[ ! -f "$STATE_FILE" ]] || die "a tamper is already in place ($(cat "$STATE_FILE")); run with --restore first"

RECEIPT="$(admin -c "SELECT last_receipt_id FROM chain_anchors WHERE status = 'ANCHORED' ORDER BY epoch DESC LIMIT 1;")"
[[ -n "$RECEIPT" ]] || die "no anchored epoch yet: wait for an '[ANCHOR] epoch N anchored' line in the coordinator log"

echo
log "1/3  the honest history, checked against the chain"
verify epoch latest || die "the history did not match before any tampering; fix that first"

echo
log "2/3  tampering: flipping one flag on receipt $RECEIPT, as an operator with database rights could"
flip "$RECEIPT"
echo "$RECEIPT" > "$STATE_FILE"

echo
log "3/3  the same check again"
if verify epoch latest; then
  die "the check still passed: the tampering was not detected"
fi
echo
verify receipt "$RECEIPT" || true
echo
log "caught. Put it back with: scripts/demo/chain_tamper.sh --restore"
