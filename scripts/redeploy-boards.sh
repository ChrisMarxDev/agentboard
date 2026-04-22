#!/usr/bin/env bash
#
# redeploy-boards.sh — trigger a Coolify redeploy for one or every board.
#
# Without arguments, redeploys every application whose name starts with
# "agentboard-". Pass a board name to redeploy just that one.
#
# Required env:
#   COOLIFY_URL     e.g. https://coolify.example.com
#   COOLIFY_TOKEN   API bearer token
#
# Usage:
#   scripts/redeploy-boards.sh            # redeploy all
#   scripts/redeploy-boards.sh alice      # redeploy one
#   scripts/redeploy-boards.sh --force    # bypass Coolify's cache, rebuild from scratch

set -euo pipefail

log() { printf '\033[36m>>\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31mERROR\033[0m %s\n' "$*" >&2; exit "${2:-1}"; }

NAME=""
FORCE=false

while [[ $# -gt 0 ]]; do
	case "$1" in
		--force) FORCE=true; shift ;;
		-h|--help) sed -n '2,/^set -euo/p' "$0" | sed -E 's/^# ?//;/^set -euo/d'; exit 0 ;;
		--*) die "Unknown flag: $1" 2 ;;
		*)   NAME="$1"; shift ;;
	esac
done

[[ -z "${COOLIFY_URL:-}" || -z "${COOLIFY_TOKEN:-}" ]] && die "COOLIFY_URL and COOLIFY_TOKEN required." 2

COOLIFY_URL="${COOLIFY_URL%/}"
FORCE_Q=""
$FORCE && FORCE_Q="&force=true"

apps=$(curl -sS -H "Authorization: Bearer $COOLIFY_TOKEN" "$COOLIFY_URL/api/v1/applications")

if [[ -n "$NAME" ]]; then
	targets=$(echo "$apps" | jq --arg n "agentboard-$NAME" '[.[] | select(.name == $n)]')
else
	targets=$(echo "$apps" | jq '[.[] | select(.name | startswith("agentboard-"))]')
fi

count=$(echo "$targets" | jq 'length')
[[ "$count" == "0" ]] && die "No matching boards found." 1

log "Redeploying $count board(s) …"

fail=0
while read -r uuid name; do
	log "  → $name ($uuid)"
	resp=$(curl -sS -H "Authorization: Bearer $COOLIFY_TOKEN" \
		"$COOLIFY_URL/api/v1/deploy?uuid=$uuid$FORCE_Q")
	if echo "$resp" | jq -e 'has("message") and (.message | test("error|failed"; "i"))' >/dev/null 2>&1; then
		echo "$resp" | jq . >&2
		fail=$((fail+1))
	fi
done < <(echo "$targets" | jq -r '.[] | "\(.uuid) \(.name)"')

if (( fail > 0 )); then
	die "$fail board(s) failed to redeploy." 3
fi
log "All redeploys triggered."
