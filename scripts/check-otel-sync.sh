#!/usr/bin/env bash
# Fail if the text forge emits in its Runtime Tracing section has drifted from
# the agent-reliability-otel-labels repo it describes.
#
# Background: that text (internal/forge/tracingtext/) is a second copy of what
# agent-reliability-otel-labels documents. `go test` already checks it against a vendored
# copy of the library's key manifest; this script checks the vendored copy and
# the function names against the real thing.
#
# Checks two things:
#   1. testdata/forge/otel/otel-spec-keys.json equals <otel>/spec/keys.json
#      (line endings ignored).
#   2. Every identifier in testdata/forge/otel/api-names.txt still appears in
#      that language's binding source.
#
# Not covered: a third-party instrumentor renaming a package or call.
#
# Usage:
#   OTEL_REPO=../agent-reliability-otel-labels scripts/check-otel-sync.sh
#   scripts/check-otel-sync.sh                 # defaults to ../agent-reliability-otel-labels
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
otel="${OTEL_REPO:-$repo_root/../agent-reliability-otel-labels}"
vendored="$repo_root/testdata/forge/otel/otel-spec-keys.json"
names="$repo_root/testdata/forge/otel/api-names.txt"

if [ ! -d "$otel" ]; then
  echo "error: agent-reliability-otel-labels repo not found at '$otel' (set OTEL_REPO)" >&2
  exit 2
fi

status=0

if ! diff -q <(tr -d '\r' < "$vendored") <(tr -d '\r' < "$otel/spec/keys.json") >/dev/null 2>&1; then
  status=1
  echo "KEYS DRIFT: testdata/forge/otel/otel-spec-keys.json != agent-reliability-otel-labels spec/keys.json" >&2
  echo "  copy the new file in, then fix internal/forge/tracingtext/ until go test passes" >&2
fi

count=0
while IFS=$'\t' read -r lang name; do
  [ -z "$lang" ] && continue
  count=$((count + 1))
  case "$lang" in
    python)     hit="$(grep -rlw -- "$name" "$otel/python/src" 2>/dev/null || true)" ;;
    typescript) hit="$(grep -rlw -- "$name" "$otel/typescript/src" 2>/dev/null || true)" ;;
    go)         hit="$(grep -lw -- "$name" "$otel"/go/*.go 2>/dev/null | grep -v '_test\.go$' || true)" ;;
    *)
      status=1
      echo "UNKNOWN LANGUAGE '$lang' in testdata/forge/otel/api-names.txt" >&2
      continue
      ;;
  esac
  if [ -z "$hit" ]; then
    status=1
    echo "API DRIFT: the $lang binding no longer defines '$name'" >&2
  fi
done < "$names"

if [ "$count" -eq 0 ]; then
  echo "error: testdata/forge/otel/api-names.txt is empty; nothing was checked" >&2
  exit 2
fi

if [ "$status" -ne 0 ]; then
  echo "" >&2
  echo "forge's Runtime Tracing text is out of sync with agent-reliability-otel-labels." >&2
  exit 1
fi

echo "forge tracing text is in sync with agent-reliability-otel-labels ($count identifiers checked)"
