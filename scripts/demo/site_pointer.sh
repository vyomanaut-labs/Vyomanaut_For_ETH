#!/usr/bin/env bash
# scripts/demo/site_pointer.sh
#
# Points the public QR page (site/, on GitHub Pages) at the network that is
# running now. The free tunnel address changes every time host.sh starts, so
# run this after each start, then commit and push site/coordinator.json: the
# page redeploys in about a minute and shows the new QR.
#
#   scripts/demo/site_pointer.sh                 # read the address from the local coordinator
#   scripts/demo/site_pointer.sh https://x.trycloudflare.com
#   scripts/demo/site_pointer.sh --clear         # no live network (page says offline)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FILE="$REPO_ROOT/site/coordinator.json"
PORT="${PORT:-8080}"

case "${1:-}" in
  --clear) URL="" ;;
  "")
    INFO="$(curl -fsS -m 5 "http://localhost:$PORT/api/v1/network/info")" \
      || { echo "No coordinator answering on localhost:$PORT. Start host.sh first, or pass the address." >&2; exit 1; }
    URL="$(printf '%s' "$INFO" | sed -n 's/.*"coordinator_url":"\([^"]*\)".*/\1/p')"
    ;;
  *) URL="${1%/}" ;;
esac

if [ -n "$URL" ] && ! printf '%s' "$URL" | grep -Eq '^https?://[A-Za-z0-9.-]+(:[0-9]{1,5})?$'; then
  echo "Not a plain address: $URL" >&2; exit 1
fi
case "$URL" in
  http://localhost*|http://127.*|"") ;;
  http://*) echo "Warning: $URL is plain http; a secure page cannot open it. Start host.sh without --local." >&2 ;;
esac

cat > "$FILE" <<JSON
{
  "coordinator_url": "$URL",
  "note": "The host's public address. scripts/demo/site_pointer.sh fills this in from the running network; commit and push it to update the live page."
}
JSON

echo "site/coordinator.json now points at: ${URL:-(nothing: page will say offline)}"
echo
echo "Publish it:"
echo "  git add site/coordinator.json && git commit -m 'site: point at the live network' && git push"
echo "Live page: https://vyomanaut-labs.github.io/Vyomanaut_For_ETH/"
echo "Instant preview without pushing: https://vyomanaut-labs.github.io/Vyomanaut_For_ETH/?c=${URL:-<tunnel address>}"
