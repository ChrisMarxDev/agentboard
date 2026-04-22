#!/usr/bin/env bash
#
# new-board.sh — provision a new AgentBoard instance on the shared Coolify host.
#
# Creates a Coolify application named "agentboard-<name>", sets env vars
# (auth token, project name, data path), and triggers an initial deploy.
# Prints the URL and auth token as JSON on success.
#
# Required env (set once in your shell or direnv):
#   COOLIFY_URL              e.g. https://coolify.example.com
#   COOLIFY_TOKEN            API bearer token (create in Coolify UI → Keys & Tokens)
#   COOLIFY_PROJECT_UUID     UUID of the Coolify project to create apps in
#   COOLIFY_SERVER_UUID      UUID of the target server
#   COOLIFY_ENVIRONMENT_NAME e.g. production
#   BOARDS_DOMAIN            e.g. boards.example.com (board FQDN is <name>.<BOARDS_DOMAIN>)
#
# Optional env:
#   AGENTBOARD_GIT_REPO      Git repo URL (default: origin remote of this checkout)
#   AGENTBOARD_GIT_BRANCH    Branch to track (default: main)
#
# Usage:
#   scripts/new-board.sh <name> [--token TOKEN] [--fqdn FQDN]
#
#   <name>         board name (lowercase, kebab-case recommended, e.g. "alice")
#   --token        use this auth token (default: random 32-byte hex)
#   --fqdn         override the full FQDN (default: <name>.<BOARDS_DOMAIN>)
#
# Output: one JSON line on stdout:
#   { "name": "...", "url": "...", "token": "...", "uuid": "..." }
#
# Persistent storage: Coolify's public application-create API does not cover
# storage mounts, so after the first deploy completes add a Persistent Storage
# entry in the Coolify UI with destination /data. This is a one-time step per
# board (~10 seconds of clicking). Subsequent redeploys preserve the volume.
# See HOSTING.md → "Multi-board via Coolify" for the full flow.
#
# Exit codes:
#   0   success
#   2   bad arguments / missing env
#   3   Coolify API error

set -euo pipefail

# ---------- logging ----------
log() { printf '\033[36m>>\033[0m %s\n' "$*" >&2; }
err() { printf '\033[31mERROR\033[0m %s\n' "$*" >&2; }
die() { err "$1"; exit "${2:-1}"; }

usage() {
	sed -n '2,/^set -euo/p' "$0" | sed -E 's/^# ?//;/^set -euo/d'
	exit 0
}

# ---------- argument parsing ----------
NAME=""
TOKEN=""
FQDN=""

while [[ $# -gt 0 ]]; do
	case "$1" in
		--token) TOKEN="$2"; shift 2 ;;
		--fqdn)  FQDN="$2"; shift 2 ;;
		-h|--help) usage ;;
		--*) die "Unknown flag: $1" 2 ;;
		*)   [[ -z "$NAME" ]] && NAME="$1" || die "Unexpected argument: $1" 2; shift ;;
	esac
done

[[ -z "$NAME" ]] && die "Board name required. Example: scripts/new-board.sh alice" 2
[[ "$NAME" =~ ^[a-z0-9][a-z0-9-]*$ ]] || die "Name must be lowercase kebab-case (got: $NAME)" 2

# ---------- env validation ----------
required=(COOLIFY_URL COOLIFY_TOKEN COOLIFY_PROJECT_UUID COOLIFY_SERVER_UUID COOLIFY_ENVIRONMENT_NAME BOARDS_DOMAIN)
missing=()
for var in "${required[@]}"; do
	[[ -z "${!var:-}" ]] && missing+=("$var")
done
if (( ${#missing[@]} > 0 )); then
	die "Missing required env: ${missing[*]}. See --help." 2
fi

GIT_REPO="${AGENTBOARD_GIT_REPO:-$(git -C "$(dirname "$0")/.." remote get-url origin 2>/dev/null || true)}"
GIT_BRANCH="${AGENTBOARD_GIT_BRANCH:-main}"
[[ -z "$GIT_REPO" ]] && die "AGENTBOARD_GIT_REPO not set and no git origin found." 2

# Normalize: Coolify wants https URL, not git@github.com:... form.
if [[ "$GIT_REPO" =~ ^git@github\.com:(.+)\.git$ ]]; then
	GIT_REPO="https://github.com/${BASH_REMATCH[1]}"
fi
GIT_REPO="${GIT_REPO%.git}"

# ---------- derived values ----------
APP_NAME="agentboard-$NAME"
FQDN="${FQDN:-$NAME.$BOARDS_DOMAIN}"
URL="https://$FQDN"
TOKEN="${TOKEN:-$(openssl rand -hex 32)}"
COOLIFY_URL="${COOLIFY_URL%/}"

# ---------- helpers ----------
api() {
	local method="$1" path="$2" body="${3:-}"
	local args=(-sS -X "$method" -H "Authorization: Bearer $COOLIFY_TOKEN")
	if [[ -n "$body" ]]; then
		args+=(-H "Content-Type: application/json" -d "$body")
	fi
	curl "${args[@]}" "$COOLIFY_URL/api/v1$path"
}

check_ok() {
	local response="$1" context="$2"
	if echo "$response" | jq -e 'has("message") and (.message | test("error|failed|invalid"; "i"))' >/dev/null 2>&1; then
		err "Coolify API error during $context:"
		echo "$response" | jq . >&2 || echo "$response" >&2
		exit 3
	fi
}

# ---------- preflight ----------
log "Board:      $NAME"
log "App name:   $APP_NAME"
log "FQDN:       $FQDN"
log "Repo:       $GIT_REPO @ $GIT_BRANCH"
log "Coolify:    $COOLIFY_URL"

# ---------- 1. create application ----------
log "Creating Coolify application …"
create_body=$(jq -n \
	--arg project_uuid      "$COOLIFY_PROJECT_UUID" \
	--arg server_uuid       "$COOLIFY_SERVER_UUID" \
	--arg environment_name  "$COOLIFY_ENVIRONMENT_NAME" \
	--arg git_repository    "$GIT_REPO" \
	--arg git_branch        "$GIT_BRANCH" \
	--arg name              "$APP_NAME" \
	--arg dockerfile_location "./Dockerfile" \
	--arg ports_exposes     "3000" \
	--arg domains           "$URL" \
	'{
		project_uuid: $project_uuid,
		server_uuid: $server_uuid,
		environment_name: $environment_name,
		git_repository: $git_repository,
		git_branch: $git_branch,
		name: $name,
		build_pack: "dockerfile",
		dockerfile_location: $dockerfile_location,
		ports_exposes: $ports_exposes,
		domains: $domains,
		instant_deploy: false
	}')

create_resp=$(api POST "/applications/public" "$create_body")
check_ok "$create_resp" "application create"

APP_UUID=$(echo "$create_resp" | jq -r '.uuid // empty')
[[ -z "$APP_UUID" ]] && { err "No uuid in create response:"; echo "$create_resp" | jq . >&2; exit 3; }
log "Created app uuid=$APP_UUID"

# ---------- 2. set environment variables ----------
set_env() {
	local key="$1" value="$2"
	local body
	body=$(jq -n --arg k "$key" --arg v "$value" '{key:$k, value:$v, is_preview:false, is_literal:true}')
	local resp
	resp=$(api POST "/applications/$APP_UUID/envs" "$body")
	check_ok "$resp" "set env $key"
}

log "Setting environment variables …"
set_env AGENTBOARD_AUTH_TOKEN "$TOKEN"
set_env AGENTBOARD_PROJECT    "$NAME"
set_env AGENTBOARD_PATH       "/data"

# ---------- 3. deploy ----------
log "Triggering initial deploy …"
deploy_resp=$(api GET "/deploy?uuid=$APP_UUID")
check_ok "$deploy_resp" "deploy"

# ---------- 4. output ----------
log "Done. Remember to add Persistent Storage mount /data in the Coolify UI"
log "before authoring data you want to keep across redeploys."

jq -n \
	--arg name  "$NAME" \
	--arg url   "$URL" \
	--arg token "$TOKEN" \
	--arg uuid  "$APP_UUID" \
	'{name:$name, url:$url, token:$token, uuid:$uuid}'
