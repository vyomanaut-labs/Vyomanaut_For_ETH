#!/usr/bin/env bash
# scripts/demo/arkiv_tamper.sh
#
# The tamper demo for the Arkiv mirror: check the mirrored audit batches
# against PostgreSQL (green), change one stored receipt the way an operator
# with database rights could, and check again (red, naming the receipt and the
# field that changed).
#
#   scripts/demo/arkiv_tamper.sh            honest check, tamper, check again
#   scripts/demo/arkiv_tamper.sh --restore  put the receipt back (repeat the demo)
#
# Needs the network started by scripts/demo/up.sh with the Arkiv mirror on and
# at least one published batch (an "[ARKIV] audit_batch ... published" line in
# the coordinator log). Uses the same VYOMANAUT_ARKIV_* environment as the
# coordinator (eval "$(scripts/demo/arkiv_env.sh)").
#
# The tamper flips one boolean (jit_flag) on a receipt of the newest published
# batch. That field is part of the receipt's leaf, the row stays valid for
# every database constraint, and flipping it again restores it.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." >/dev/null 2>&1 && pwd)"
cd -- "$REPO_ROOT"

STATE_DIR="${VYOMANAUT_DEMO_STATE_DIR:-/tmp/vyomanaut-demo}"
BIN_DIR="$STATE_DIR/bin"
STATE_FILE="$STATE_DIR/arkiv_tamper.state"

PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGDATABASE="${PGDATABASE:-vyomanaut_demo}"
PGMIGRATORUSER="${PGMIGRATORUSER:-vyomanaut_migrator}"
PGMIGRATORPASSWORD="${PGMIGRATORPASSWORD:-devpass}"
PGAPPPASSWORD="${PGAPPPASSWORD:-devpass}"

log() { echo "[arkiv-tamper] $*"; }
die() { echo "[arkiv-tamper] $*" >&2; exit 1; }

command -v psql >/dev/null 2>&1 || die "psql is not on PATH"
command -v go >/dev/null 2>&1 || die "go is not on PATH"

# As the database owner: the superuser sees past the append-only policies, which
# is exactly the access an operator-level attacker has.
admin() {
  PGPASSWORD="$PGMIGRATORPASSWORD" psql -v ON_ERROR_STOP=1 -At \
    -h "$PGHOST" -p "$PGPORT" -U "$PGMIGRATORUSER" -d "$PGDATABASE" "$@"
}

mkdir -p "$BIN_DIR"
log "building arkivcheck"
go build -o "$BIN_DIR/arkivcheck" ./cmd/arkivcheck

check() {
  PGUSER=vyomanaut_app PGPASSWORD="$PGAPPPASSWORD" PGHOST="$PGHOST" PGPORT="$PGPORT" PGDATABASE="$PGDATABASE" \
    "$BIN_DIR/arkivcheck" --limit 5 audits
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
  check || true
  exit 0
fi

[[ ! -f "$STATE_FILE" ]] || die "a tamper is already in place ($(cat "$STATE_FILE")); run with --restore first"

# The newest confirmed batch: its reference is <provider_id>:<period_start>:<seq>.
RECEIPT="$(admin -c "
SELECT r.receipt_id FROM arkiv_publications p
JOIN audit_receipts r
  ON r.provider_id = split_part(p.reference, ':', 1)::uuid
 AND r.service_countersign_ts >= p.range_start AND r.service_countersign_ts < p.range_end
 AND r.service_sig IS NOT NULL AND r.audit_result IS NOT NULL
WHERE p.kind = 'audit_batch' AND p.status = 'CONFIRMED'
ORDER BY p.created_at DESC, r.service_countersign_ts, r.receipt_id
LIMIT 1;")"
[[ -n "$RECEIPT" ]] || die "no published batch yet: wait for an '[ARKIV] audit_batch ... published' line in the coordinator log"

echo
log "1/3  the honest history, checked against Arkiv"
check || die "the history did not match before any tampering; fix that first"

echo
log "2/3  tampering: flipping one flag on receipt $RECEIPT, as an operator with database rights could"
flip "$RECEIPT"
echo "$RECEIPT" > "$STATE_FILE"

echo
log "3/3  the same check again"
if check; then
  die "the check still passed: the tampering was not detected"
fi
echo
log "caught. Put it back with: scripts/demo/arkiv_tamper.sh --restore"
