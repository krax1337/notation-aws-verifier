#!/usr/bin/env bash
# Runs govulncheck and fails on any reachable (symbol-level) vulnerability that is
# not listed in .govulncheck-ignore. govulncheck has no native ignore mechanism.
set -euo pipefail

GOVULNCHECK="${GOVULNCHECK:-go run golang.org/x/vuln/cmd/govulncheck@v1.8.0}"
IGNORE_FILE="${IGNORE_FILE:-.govulncheck-ignore}"

report="$(mktemp)"
trap 'rm -f "$report"' EXIT

# Human-readable output first; its exit code is decided by the filter below.
$GOVULNCHECK ./... || true
$GOVULNCHECK -format json ./... >"$report"

ignored="$(grep -Ev '^[[:space:]]*(#|$)' "$IGNORE_FILE" 2>/dev/null | awk '{print $1}' || true)"

found="$(jq -r 'select(.finding != null and .finding.trace[0].function != null) | .finding.osv' "$report" | sort -u)"

unignored="$(comm -23 <(printf '%s\n' "$found" | sed '/^$/d') <(printf '%s\n' "$ignored" | sed '/^$/d' | sort -u))"

for id in $ignored; do
  if printf '%s\n' "$found" | grep -qx "$id"; then
    echo "ignored: $id (listed in $IGNORE_FILE)"
  fi
done

if [ -n "$unignored" ]; then
  echo "reachable vulnerabilities not in $IGNORE_FILE:" >&2
  printf '  %s\n' $unignored >&2
  exit 1
fi
echo "govulncheck: no unignored reachable vulnerabilities"
