#!/usr/bin/env bash
# scripts/release/purego_env.sh
#
# Prints the export lines that make every `go build` in this terminal use the
# pure-Go Badger storage engine instead of RocksDB, so a coordinator or a
# provider on macOS or Linux needs no RocksDB and no C toolchain. Windows and
# Android already use Badger.
#
#   eval "$(scripts/release/purego_env.sh)"
#   scripts/demo/up.sh ...        # or scripts/demo/join.sh ...
#
# It works through a Go overlay that swaps the build tags of three storage
# files (the same idea as android/scripts/build_android_provider.ps1); no Go
# file in the repository is changed. Data written by a RocksDB build is not
# readable by a Badger build, so start a provider with a fresh --data-dir.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." >/dev/null 2>&1 && pwd)"
STORAGE="$REPO_ROOT/internal/storage"
OUT="${XDG_CACHE_HOME:-$HOME/.cache}/vyomanaut-purego"
mkdir -p "$OUT"

retag() { # file, new first line
  local src="$STORAGE/$1"
  head -n1 "$src" | grep -q '^//go:build' || { echo "# [purego_env] unexpected first line in $src" >&2; echo "false"; exit 1; }
  { printf '%s\n' "$2"; tail -n +2 "$src"; } > "$OUT/$1"
}
retag engine_rocksdb.go '//go:build (linux || darwin) && !vyopurego'
retag vlog.go           '//go:build (linux || darwin) && !vyopurego'
retag engine_badger.go  '//go:build windows || vyopurego'

# Go wants native paths; under Git Bash on Windows turn /c/... into C:/...
native() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi; }
S="$(native "$STORAGE")"
O="$(native "$OUT")"
cat > "$OUT/overlay.json" <<JSON
{"Replace": {
  "$S/engine_rocksdb.go": "$O/engine_rocksdb.go",
  "$S/vlog.go": "$O/vlog.go",
  "$S/engine_badger.go": "$O/engine_badger.go"
}}
JSON

case "$OUT" in *" "*) echo "# [purego_env] the cache path contains a space; set XDG_CACHE_HOME to a path without one" >&2; echo "false"; exit 1 ;; esac
printf 'export CGO_ENABLED=0\n'
printf 'export GOFLAGS=%q\n' "-overlay=$O/overlay.json -tags=vyopurego"
echo "# [purego_env] go builds in this terminal now use the pure-Go storage engine (no RocksDB)" >&2
