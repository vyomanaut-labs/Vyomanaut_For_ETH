#!/usr/bin/env bash
# deployments/railway/start.sh: what the Railway container runs. It does what
# scripts/demo/host.sh does on a laptop (relay, coordinator, local providers), but the
# addresses come from Railway: a public domain for the coordinator and a TCP proxy for the relay.
#
# Settings (Railway variables). Required:
#   PGHOST PGPORT PGUSER PGPASSWORD  reference the Postgres service: ${{Postgres.PGHOST}} etc.
#   PORT=8080                        the coordinator's port; the domain must point at it
# Recommended:
#   VYOMANAUT_DEMO_ADMIN_KEY         64 hex characters (openssl rand -hex 32): the join desk key
#   VYOMANAUT_NETWORK_NAME           shown on every page
#   VYOMANAUT_RELEASE_BASE           where the app/laptop downloads come from (a GitHub release)
# Optional:
#   VYOMANAUT_PROVIDERS              local providers (default 5, the demo network's minimum)
#   CHAIN_PRIVATE_KEY, ARKIV_PRIVATE_KEY, ARKIV_ACCESS_KEY   hex keys; turn the chains on
set -euo pipefail
cd /app

log() { printf '[railway] %s\n' "$*"; }
die() { printf '[railway] ERROR: %s\n' "$*" >&2; exit 1; }

PORT="${PORT:-8080}"
PROVIDERS="${VYOMANAUT_PROVIDERS:-5}"
RELAY_PORT=30300
STATE_DIR="${VYOMANAUT_DEMO_STATE_DIR:-/var/vyomanaut}"
BIN_DIR="$STATE_DIR/bin"

: "${PGHOST:?PGHOST is not set: see guides/deploy_railway.md}"
: "${PGPASSWORD:?PGPASSWORD is not set: see guides/deploy_railway.md}"
PGADMINUSER="${PGUSER:-postgres}"; PGADMINPASSWORD="$PGPASSWORD"; PGPORT="${PGPORT:-5432}"

[[ -n "${RAILWAY_PUBLIC_DOMAIN:-}" ]] \
  || die "No public domain yet. In this service: Settings > Networking > Generate Domain, target port $PORT. Then redeploy."
[[ -n "${RAILWAY_TCP_PROXY_DOMAIN:-}" && -n "${RAILWAY_TCP_PROXY_PORT:-}" ]] \
  || die "No TCP proxy yet. In this service: Settings > Networking > TCP Proxy, port $RELAY_PORT. Then redeploy."
PUBLIC_URL="https://$RAILWAY_PUBLIC_DOMAIN"
PUBLIC_RELAY="$RAILWAY_TCP_PROXY_DOMAIN:$RAILWAY_TCP_PROXY_PORT"

log "waiting for Postgres at $PGHOST:$PGPORT"
for _ in $(seq 1 60); do
  PGPASSWORD="$PGADMINPASSWORD" psql -h "$PGHOST" -p "$PGPORT" -U "$PGADMINUSER" -d postgres -Atc "select 1" >/dev/null 2>&1 && break
  sleep 2
done
PGPASSWORD="$PGADMINPASSWORD" psql -h "$PGHOST" -p "$PGPORT" -U "$PGADMINUSER" -d postgres -Atc "select 1" >/dev/null \
  || die "Cannot reach Postgres. Check the PG* variables."

# The schema must be owned by a role called vyomanaut_migrator (up.sh and the migrations expect it).
# Make it from the admin user with fresh random passwords on every start; nothing is stored.
MIGRATOR_PW="$(openssl rand -hex 16)"; APP_PW="$(openssl rand -hex 16)"
PGPASSWORD="$PGADMINPASSWORD" psql -v ON_ERROR_STOP=1 -q -h "$PGHOST" -p "$PGPORT" -U "$PGADMINUSER" -d postgres <<SQL
DO \$\$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'vyomanaut_migrator') THEN
    CREATE ROLE vyomanaut_migrator LOGIN SUPERUSER;
  END IF;
END \$\$;
ALTER ROLE vyomanaut_migrator LOGIN SUPERUSER PASSWORD '$MIGRATOR_PW';
SQL

# Keep the chain keys (optional) in private files, never on a command line.
if [[ -n "${CHAIN_PRIVATE_KEY:-}" || -n "${ARKIV_PRIVATE_KEY:-}" ]]; then
  umask 077; mkdir -p /run/vkeys
  if [[ -n "${CHAIN_PRIVATE_KEY:-}" ]]; then
    printf '%s\n' "$CHAIN_PRIVATE_KEY" >/run/vkeys/deployer.key
    export VYOMANAUT_CHAIN_PRIVATE_KEY_FILE=/run/vkeys/deployer.key
    eval "$(bash scripts/demo/chain_env.sh)"
    log "Whitechain settlement and anchoring are on"
  fi
  if [[ -n "${ARKIV_PRIVATE_KEY:-}" ]]; then
    printf '%s\n' "$ARKIV_PRIVATE_KEY" >/run/vkeys/arkiv.key
    export VYOMANAUT_ARKIV_PRIVATE_KEY_FILE=/run/vkeys/arkiv.key
    if [[ -n "${ARKIV_ACCESS_KEY:-}" ]]; then
      printf '%s\n' "$ARKIV_ACCESS_KEY" >/run/vkeys/arkiv_access.key
      export VYOMANAUT_ARKIV_ACCESS_KEY_FILE=/run/vkeys/arkiv_access.key
    fi
    eval "$(bash scripts/demo/arkiv_env.sh)"
    log "the Arkiv mirror is on"
  fi
  umask 022
fi

log "starting the relay on port $RELAY_PORT"
"$BIN_DIR/relay" --listen ":$RELAY_PORT" >"$STATE_DIR/relay.log" 2>&1 &
echo $! >"$STATE_DIR/relay.pid"

export VYOMANAUT_NETWORK_NAME="${VYOMANAUT_NETWORK_NAME:-Vyomanaut}"
export VYOMANAUT_PUBLIC_URL="$PUBLIC_URL" VYOMANAUT_PUBLIC_RELAY="$PUBLIC_RELAY"
# The local providers sit beside the coordinator, so they stay direct.
unset VYOMANAUT_RELAY_ADDRS VYOMANAUT_PUBLIC_CHAIN_API
export PGMIGRATORUSER=vyomanaut_migrator PGMIGRATORPASSWORD="$MIGRATOR_PW" PGAPPPASSWORD="$APP_PW"
export PGHOST PGPORT PGDATABASE=vyomanaut_demo

log "starting the coordinator and $PROVIDERS local providers (about 1 to 2 minutes)"
# up.sh prints the admin key; keep it out of Railway's log view.
bash scripts/demo/up.sh --providers "$PROVIDERS" --port "$PORT" --advertise-host 127.0.0.1 \
  | sed -E 's/(ADMIN_API_KEY +=) +[0-9a-f]+/\1 (hidden)/'

# shellcheck disable=SC1091
source "$STATE_DIR/env"
cat <<DONE

[railway] $VYOMANAUT_NETWORK_NAME is up.
  Join page:   $PUBLIC_URL/join
  Big screen:  $PUBLIC_URL/join/screen
  Join desk:   $PUBLIC_URL/join/desk
  Relay:       $PUBLIC_RELAY
DONE
if [[ -z "${VYOMANAUT_DEMO_ADMIN_KEY:-}" ]]; then
  log "VYOMANAUT_DEMO_ADMIN_KEY is not set, so this start made a one-off admin key (it changes at every restart):"
  log "  $ADMIN_API_KEY"
fi

# Show the coordinator's log in Railway's log view, and exit (so Railway restarts us) if it dies.
tail -n 0 -F "$STATE_DIR/logs/microservice.log" &
trap 'kill $(jobs -p) 2>/dev/null; pkill -f "$BIN_DIR/" 2>/dev/null; exit 0' TERM INT
MS_PID="$(head -n1 "$STATE_DIR/pids")"
while kill -0 "$MS_PID" 2>/dev/null; do sleep 5; done
die "the coordinator stopped"
