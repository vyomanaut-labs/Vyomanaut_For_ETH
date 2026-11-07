#!/bin/sh
# Join [[.NetworkName]] from this computer (macOS or Linux).
#
#   curl -fsSL [[.CoordinatorURL]]/join/install.sh | sh
#
# Downloads the Vyomanaut provider, registers this computer once (you need a
# phone number and the 6-digit code from the network's host), then starts
# lending storage. Run the same line again to start it again later.
# Everything lives in ~/.vyomanaut-join/[[.Slug]]; delete that folder to leave.
set -eu

COORD=[[sh .CoordinatorURL]]
RELAY=[[sh .Relay]]
MODE=[[sh .Mode]]
DIR="${VYOMANAUT_DIR:-$HOME/.vyomanaut-join/[[.Slug]]}"

say() { printf '\n\033[1;36m[vyomanaut]\033[0m %s\n' "$*"; }

case "$(uname -s)" in
  Darwin) os=macos ;;
  Linux) os=linux ;;
  *) echo "On Windows, run this in PowerShell instead:  irm $COORD/join/install.ps1 | iex" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "This computer's processor ($(uname -m)) is not supported yet." >&2; exit 1 ;;
esac

mkdir -p "$DIR"
BIN="$DIR/provider"
if [ ! -x "$BIN" ]; then
  say "Downloading the provider for $os-$arch ..."
  curl -fL --progress-bar -o "$BIN.part" "$COORD/join/download/provider-$os-$arch"
  mv "$BIN.part" "$BIN"
  chmod +x "$BIN"
  if [ "$os" = macos ]; then xattr -d com.apple.quarantine "$BIN" 2>/dev/null || true; fi
fi

if [ ! -f "$DIR/registration.json" ]; then
  say "Joining [[.NetworkName]]"
  while :; do
    printf 'Your phone number with country code (e.g. +919876500001): '
    read -r PHONE </dev/tty
    case "$PHONE" in
      +[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]*) break ;;
      *) echo "  Start with + and your country code, then the number." ;;
    esac
  done
  printf 'How many GB of this disk to lend (at least 1) [10]: '
  read -r GB </dev/tty
  case "$GB" in '' | *[!0-9]*) GB=10 ;; esac
  [ "$GB" -ge 1 ] || { echo "  The network needs at least 1 GB; using 1."; GB=1; }
  say "Now ask the host for your 6-digit code. It shows on their join desk."
  "$BIN" onboard --microservice-url="$COORD" --phone="$PHONE" --storage-gb="$GB" --data-dir="$DIR" </dev/tty
fi

GB="$(grep -o '"declared_storage_gb":[0-9]*' "$DIR/registration.json" 2>/dev/null | grep -o '[0-9]*$' || true)"
[ -n "$GB" ] || GB=10

say "You're in. Lending $GB GB to [[.NetworkName]]. Keep this window open; Ctrl-C stops it."
exec "$BIN" run --mode="$MODE" --microservice-url="$COORD" --data-dir="$DIR" \
  --declared-storage-gb="$GB" --relay-addrs="$RELAY" </dev/null
