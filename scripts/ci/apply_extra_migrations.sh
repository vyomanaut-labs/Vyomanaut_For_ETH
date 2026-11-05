#!/usr/bin/env bash
# scripts/ci/apply_extra_migrations.sh
#
# Applies every numbered migration AFTER 001 to the database named by the
# standard libpq variables (PGHOST, PGPORT, PGUSER, PGDATABASE, PGPASSWORD),
# in numeric order.
#
# 001 is generated per profile (migrations/generator.go) and applied by its
# caller. Every later migration is profile-invariant hand-written SQL, so one
# file serves demo and prod. Run this script AFTER the 001 schema is applied
# and BEFORE any test or daemon starts; the three callers are:
#   - .github/workflows/ci.yml (check-07)
#   - scripts/demo/up.sh
#   - scripts/demo/up.ps1 (native PowerShell loop, same rules)
#
# Number ownership for the hackathon build (see migrations/README.md):
#   002  chain / payments feature
#   003  Arkiv mirror feature
#
# Two files carrying the same number is a hard error. It is exactly what two
# feature branches produce when both claim the next free number, and failing
# here makes that clash loud instead of letting one file silently apply first.
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
shopt -s nullglob

# Pass 1: validate before touching the database. 001 is exempt from the
# uniqueness rule because it legitimately has two files (prod and demo).
declare -A seen=()
extra=()
for f in "$REPO_ROOT"/migrations/[0-9][0-9][0-9]_*.sql; do
  base="$(basename "$f")"
  num="${base%%_*}"
  [[ "$num" == "001" ]] && continue
  if [[ -n "${seen[$num]:-}" ]]; then
    echo "FAIL: migration number $num is used by both ${seen[$num]} and $base" >&2
    exit 1
  fi
  seen[$num]="$base"
  extra+=("$f")
done

# Pass 2: apply in numeric order (the glob already sorts lexicographically,
# which is numeric order for zero-padded names).
for f in "${extra[@]}"; do
  echo "[migrate] applying $(basename "$f")"
  psql -v ON_ERROR_STOP=1 -f "$f"
done
echo "[migrate] ${#extra[@]} extra migration(s) applied"
