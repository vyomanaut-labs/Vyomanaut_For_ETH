#!/usr/bin/env bash
# scripts/demo/arkiv_env.sh
#
# Prints the export lines that turn the Arkiv mirror on for the coordinator.
# Run it in the terminal you will start scripts/demo/up.sh from, wrapped in
# eval so the variables land in YOUR shell (works in zsh and bash, including
# macOS's bash 3.2):
#
#   eval "$(scripts/demo/arkiv_env.sh)"
#   scripts/demo/up.sh --providers 7 --advertise-host "$MY_IP"
#
# The coordinator inherits the exported environment, so up.sh needs no edit.
# It can be combined with scripts/demo/chain_env.sh: each feature has its own
# switch and its own key.
#
# Override any value by exporting it first:
#   VYOMANAUT_ARKIV_RPC_URL           (default: the public Rabdi testnet RPC)
#   VYOMANAUT_ARKIV_PRIVATE_KEY_FILE  (default: ~/vyoma-keys/arkiv.key)
#   VYOMANAUT_ARKIV_ACCESS_KEY_FILE   (default: ~/vyoma-keys/arkiv_access.key, if it exists)
#   VYOMANAUT_ARKIV_PERIOD_SECONDS    (default: empty = the profile's audit period)
#   VYOMANAUT_ARKIV_MANIFESTS         (default 1)
#   VYOMANAUT_ARKIV_APP_TAG           (default vyomanaut-demo)
#
# The wallet key and the access key are never read or printed here: only their
# paths. Notes go to stderr, so eval only ever sees export lines.
set -euo pipefail

note() { echo "# [arkiv_env] $*" >&2; }

RPC="${VYOMANAUT_ARKIV_RPC_URL:-https://rpc.rabdi.db-chain.testnet.arkiv.network}"
KEY_FILE="${VYOMANAUT_ARKIV_PRIVATE_KEY_FILE:-$HOME/vyoma-keys/arkiv.key}"
ACCESS_FILE="${VYOMANAUT_ARKIV_ACCESS_KEY_FILE:-$HOME/vyoma-keys/arkiv_access.key}"

if [[ ! -f "$KEY_FILE" ]]; then
  note "warning: $KEY_FILE does not exist yet; the coordinator will skip the Arkiv mirror without it"
  note "  create a 32-byte hex key there (chmod 600) and fund its address with tGLM at hub.arkiv.network"
fi

emit() { printf 'export %s=%q\n' "$1" "$2"; }
emit VYOMANAUT_ARKIV_ENABLED 1
emit VYOMANAUT_ARKIV_RPC_URL "$RPC"
emit VYOMANAUT_ARKIV_PRIVATE_KEY_FILE "$KEY_FILE"
if [[ -f "$ACCESS_FILE" ]]; then
  emit VYOMANAUT_ARKIV_ACCESS_KEY_FILE "$ACCESS_FILE"
else
  note "no access key file at $ACCESS_FILE; using the public RPC without one"
fi
emit VYOMANAUT_ARKIV_MANIFESTS "${VYOMANAUT_ARKIV_MANIFESTS:-1}"
emit VYOMANAUT_ARKIV_APP_TAG "${VYOMANAUT_ARKIV_APP_TAG:-vyomanaut-demo}"
if [[ -n "${VYOMANAUT_ARKIV_PERIOD_SECONDS:-}" ]]; then
  emit VYOMANAUT_ARKIV_PERIOD_SECONDS "$VYOMANAUT_ARKIV_PERIOD_SECONDS"
fi

note "Arkiv mirror on: endpoint ${RPC%%\?*}, wallet key $KEY_FILE"
note "now start the network: scripts/demo/up.sh --providers 7 --advertise-host \"\$MY_IP\""
note "then watch: grep ARKIV /tmp/vyomanaut-demo/logs/microservice.log"
