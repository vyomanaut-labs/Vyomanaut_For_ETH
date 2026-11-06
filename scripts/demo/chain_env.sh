#!/usr/bin/env bash
# scripts/demo/chain_env.sh
#
# Prints the export lines that turn Whitechain settlement, audit anchoring and
# wallet binding on for the coordinator. Run it in the terminal you will start
# scripts/demo/up.sh from, wrapped in eval so the variables land in YOUR shell
# (works in zsh and bash):
#
#   eval "$(scripts/demo/chain_env.sh)"
#   scripts/demo/up.sh --providers 7 --advertise-host "$MY_IP"
#
# The coordinator inherits the exported environment, so up.sh needs no edit.
#
# Values come from deployments/whitechain/sepolia.json. Override any of them by
# exporting it first:
#   VYOMANAUT_CHAIN_RPC_URL           (default: the public Whitechain Sepolia RPC)
#   VYOMANAUT_CHAIN_PRIVATE_KEY_FILE  (default: ~/vyoma-keys/deployer.key)
#   VYOMANAUT_CHAIN_ANCHOR_INTERVAL_SECONDS (default 120 here; the code default is 600)
#   VYOMANAUT_CHAIN_API_ADDR          (default :8090)
#   VYOMANAUT_CHAIN_DEPLOYMENT_FILE   (default: deployments/whitechain/sepolia.json, our contracts)
#
# START_BLOCK is the chain's current block, so deposits and anchors from earlier
# runs are not replayed into this run's fresh database. Make deposits AFTER
# running this. The private key itself is never read or printed: only its path.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." >/dev/null 2>&1 && pwd)"
# Your own network: deploy your own contracts (contracts/README.md), write a file
# shaped like deployments/whitechain/sepolia.json, and point this at it.
DEPLOY_JSON="${VYOMANAUT_CHAIN_DEPLOYMENT_FILE:-$REPO_ROOT/deployments/whitechain/sepolia.json}"

note() { echo "# [chain_env] $*" >&2; }
die() { echo "# [chain_env] $*" >&2; echo "false"; exit 1; }

[[ -f "$DEPLOY_JSON" ]] || die "missing $DEPLOY_JSON"
field() { sed -n "s/.*\"$1\": *\"\{0,1\}\([^\",]*\)\"\{0,1\}.*/\1/p" "$DEPLOY_JSON" | head -n1; }

CHAIN_ID="$(field chainId)"
ESCROW="$(field vyomaEscrow)"
ANCHOR="$(field auditAnchor)"
EXPLORER="$(field explorer)"
DEPLOY_BLOCK="$(field deploymentBlock)"
[[ -n "$CHAIN_ID" && -n "$ESCROW" && -n "$ANCHOR" ]] || die "could not read the contract addresses from $DEPLOY_JSON"

RPC="${VYOMANAUT_CHAIN_RPC_URL:-https://rpc.testnet.whitechain.io}"
KEY_FILE="${VYOMANAUT_CHAIN_PRIVATE_KEY_FILE:-$HOME/vyoma-keys/deployer.key}"
[[ -f "$KEY_FILE" ]] || note "warning: $KEY_FILE does not exist yet; the coordinator will not start settlement without it"

# The chain's current block, without needing Foundry: a plain JSON-RPC call.
START=""
if command -v curl >/dev/null 2>&1; then
  HEX="$(curl -s --max-time 10 -X POST -H 'content-type: application/json' \
    --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' "$RPC" \
    | sed -n 's/.*"result": *"0x\([0-9a-fA-F]*\)".*/\1/p' || true)"
  [[ -n "$HEX" ]] && START="$((16#$HEX))"
fi
if [[ -z "$START" ]]; then
  START="${DEPLOY_BLOCK:-0}"
  note "warning: could not read the current block from $RPC; using the deployment block $START. Deposits from earlier runs will be seen again."
fi

emit() { printf 'export %s=%q\n' "$1" "$2"; }
emit VYOMANAUT_CHAIN_ENABLED 1
emit VYOMANAUT_CHAIN_RPC_URL "$RPC"
emit VYOMANAUT_CHAIN_PRIVATE_KEY_FILE "$KEY_FILE"
emit VYOMANAUT_CHAIN_ESCROW_ADDRESS "$ESCROW"
emit VYOMANAUT_CHAIN_ID "$CHAIN_ID"
emit VYOMANAUT_CHAIN_START_BLOCK "$START"
emit VYOMANAUT_CHAIN_ANCHOR_ADDRESS "$ANCHOR"
emit VYOMANAUT_CHAIN_ANCHOR_INTERVAL_SECONDS "${VYOMANAUT_CHAIN_ANCHOR_INTERVAL_SECONDS:-120}"
emit VYOMANAUT_CHAIN_API_ADDR "${VYOMANAUT_CHAIN_API_ADDR:-:8090}"
emit VYOMANAUT_CHAIN_AUTO_PAYOUT_WALLETS 1
emit VYOMANAUT_CHAIN_EXPLORER_URL "${VYOMANAUT_CHAIN_EXPLORER_URL:-$EXPLORER}"

note "chain $CHAIN_ID, escrow $ESCROW, anchor $ANCHOR, start block $START"
note "now start the network: scripts/demo/up.sh --providers 7 --advertise-host \"\$MY_IP\""
