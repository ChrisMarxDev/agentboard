#!/usr/bin/env bash
#
# list-boards.sh — list every AgentBoard instance running on the Coolify host.
#
# Queries Coolify's API for applications whose name starts with "agentboard-"
# and prints one row per board. Pass --json for machine-readable output.
#
# Required env:
#   COOLIFY_URL     e.g. https://coolify.example.com
#   COOLIFY_TOKEN   API bearer token
#
# Usage:
#   scripts/list-boards.sh [--json]

set -euo pipefail

log() { printf '\033[36m>>\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31mERROR\033[0m %s\n' "$*" >&2; exit "${2:-1}"; }

JSON_OUT=false
[[ "${1:-}" == "--json" ]] && JSON_OUT=true
[[ "${1:-}" == "-h" || "${1:-}" == "--help" ]] && { sed -n '2,/^set -euo/p' "$0" | sed -E 's/^# ?//;/^set -euo/d'; exit 0; }

[[ -z "${COOLIFY_URL:-}" || -z "${COOLIFY_TOKEN:-}" ]] && die "COOLIFY_URL and COOLIFY_TOKEN required." 2

COOLIFY_URL="${COOLIFY_URL%/}"

resp=$(curl -sS -H "Authorization: Bearer $COOLIFY_TOKEN" "$COOLIFY_URL/api/v1/applications")

# Filter down to agentboard-* apps.
boards=$(echo "$resp" | jq '[.[] | select(.name | startswith("agentboard-"))]')

if $JSON_OUT; then
	echo "$boards"
	exit 0
fi

count=$(echo "$boards" | jq 'length')
if [[ "$count" == "0" ]]; then
	log "No boards found."
	exit 0
fi

# Pretty table. Column widths are intentionally coarse; redirect through column
# if you need something prettier.
printf '%-28s %-10s %s\n' NAME STATUS URL
printf '%-28s %-10s %s\n' ---- ------ ---
echo "$boards" | jq -r '.[] | [.name, (.status // "unknown"), (.fqdn // "-")] | @tsv' \
	| while IFS=$'\t' read -r name status url; do
		printf '%-28s %-10s %s\n' "$name" "$status" "$url"
	done
