#!/usr/bin/env bash
#
# deploy-vps.sh — install or update AgentBoard on a Debian/Ubuntu VPS.
#
# Designed to be re-runnable. First run installs Docker, sets up unattended
# security upgrades, clones the repo, generates an auth token, builds the
# image, starts it behind Caddy, and waits until /api/health returns 200.
# Subsequent runs update the code to the requested ref and redeploy; the
# existing token and data volume are preserved.
#
# Usage:
#   scripts/deploy-vps.sh --host root@vps.example.com [options]
#
# Options:
#   --host USER@HOST       (required) SSH target. Must be root or have
#                          passwordless sudo.
#   --domain DOMAIN        Enable TLS via Caddy + Let's Encrypt. DNS for the
#                          domain must already point at the VPS. Without this
#                          flag the server runs on plain HTTP on port 80.
#   --repo URL             Git repo URL to clone on the VPS (default: the
#                          origin remote of the current checkout).
#   --ref REF              Branch, tag, or SHA to deploy (default: main).
#   --token TOKEN          Set a specific auth token (default: reuse existing
#                          one if present, otherwise generate a random one).
#   --data-dir DIR         Host path for persistent data (default:
#                          /var/lib/agentboard).
#   --app-dir DIR          Host path for the checked-out repo (default:
#                          /opt/agentboard).
#   --timeout SECONDS      Health-check timeout (default: 180).
#   --json                 Print deploy result as a single JSON line on stdout.
#                          Narrative output still goes to stderr.
#   --help                 Show this help and exit.
#
# Exit codes:
#   0   success
#   2   bad arguments
#   10  health check did not pass within timeout
#   20  ssh / network failure
#   30  docker build or up failed
#
# Scale notes: this script is designed to be callable from a control plane.
# See SCALE.md for the requirements (idempotency, structured output, explicit
# exit codes, cloud-init compatibility) it already satisfies.

set -euo pipefail

# ---------- defaults ----------
HOST=""
DOMAIN=""
REPO=""
REF="main"
TOKEN=""
DATA_DIR="/var/lib/agentboard"
APP_DIR="/opt/agentboard"
TIMEOUT=180
JSON_OUT=false

# ---------- logging ----------
# stderr = human narration; stdout = structured result (for --json).
log() { printf '\033[36m>>\033[0m %s\n' "$*" >&2; }
err() { printf '\033[31mERROR\033[0m %s\n' "$*" >&2; }
die() { err "$1"; exit "${2:-1}"; }

usage() {
	sed -n '2,/^set -euo/p' "$0" | sed -E 's/^# ?//;/^set -euo/d'
	exit 0
}

# ---------- argument parsing ----------
while [[ $# -gt 0 ]]; do
	case "$1" in
		--host)     HOST="$2"; shift 2 ;;
		--domain)   DOMAIN="$2"; shift 2 ;;
		--repo)     REPO="$2"; shift 2 ;;
		--ref)      REF="$2"; shift 2 ;;
		--token)    TOKEN="$2"; shift 2 ;;
		--data-dir) DATA_DIR="$2"; shift 2 ;;
		--app-dir)  APP_DIR="$2"; shift 2 ;;
		--timeout)  TIMEOUT="$2"; shift 2 ;;
		--json)     JSON_OUT=true; shift ;;
		-h|--help)  usage ;;
		*)          die "Unknown argument: $1" 2 ;;
	esac
done

[[ -z "$HOST" ]] && die "--host is required. See --help." 2

# Default --repo to the origin remote of the local checkout, so "deploy what
# I have pushed" is one flag lighter.
if [[ -z "$REPO" ]]; then
	REPO=$(git -C "$(dirname "$0")/.." remote get-url origin 2>/dev/null || true)
	[[ -z "$REPO" ]] && die "--repo not set and no git origin found. Pass --repo explicitly." 2
fi

# ---------- paths ----------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TPL_DIR="$SCRIPT_DIR/vps"
[[ -d "$TPL_DIR" ]] || die "Template directory not found: $TPL_DIR"

# ---------- ssh wrappers ----------
SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 -o ServerAliveInterval=30)

ssh_exec() {
	# Runs the quoted command on the remote host. sudo -n is prepended so a
	# non-root user with passwordless sudo works; as root it's a no-op.
	ssh "${SSH_OPTS[@]}" "$HOST" "sudo -n bash -euo pipefail -c $(printf '%q' "$*")"
}

ssh_script() {
	# Pipes a heredoc script to remote bash. Used for multi-line blocks.
	ssh "${SSH_OPTS[@]}" "$HOST" "sudo -n bash -euo pipefail"
}

scp_file() {
	# Uploads a local file to the remote host under /tmp first, then sudo-moves
	# it into place so we don't need the SSH user to own APP_DIR.
	local src="$1" dst="$2"
	local tmp="/tmp/agentboard-upload.$$.$(basename "$dst")"
	scp "${SSH_OPTS[@]}" -q "$src" "$HOST:$tmp"
	ssh_exec "install -D -m 0644 '$tmp' '$dst' && rm -f '$tmp'"
}

# ---------- preflight ----------
log "Target: $HOST"
log "Repo:   $REPO @ $REF"
if [[ -n "$DOMAIN" ]]; then
	log "TLS:    enabled for $DOMAIN (anonymous ACME account)"
else
	log "TLS:    disabled — serving HTTP on port 80"
fi

log "Checking SSH + sudo..."
if ! ssh "${SSH_OPTS[@]}" "$HOST" 'sudo -n true' 2>/dev/null; then
	die "Cannot reach $HOST with passwordless sudo. Connect as root or configure sudoers first." 20
fi

# ---------- install docker + unattended upgrades ----------
log "Ensuring Docker + unattended-upgrades are installed..."
ssh_script <<'REMOTE'
export DEBIAN_FRONTEND=noninteractive

if ! command -v docker >/dev/null 2>&1; then
	. /etc/os-release
	apt-get update -qq
	apt-get install -y -qq ca-certificates curl gnupg git
	install -m 0755 -d /etc/apt/keyrings
	if [ ! -f /etc/apt/keyrings/docker.asc ]; then
		curl -fsSL "https://download.docker.com/linux/${ID}/gpg" -o /etc/apt/keyrings/docker.asc
		chmod a+r /etc/apt/keyrings/docker.asc
	fi
	echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/${ID} ${VERSION_CODENAME} stable" > /etc/apt/sources.list.d/docker.list
	apt-get update -qq
	apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
	systemctl enable --now docker
fi

if ! dpkg -s unattended-upgrades >/dev/null 2>&1; then
	apt-get install -y -qq unattended-upgrades
	dpkg-reconfigure -f noninteractive unattended-upgrades
fi

# git may be missing even if docker isn't (older images).
command -v git >/dev/null 2>&1 || apt-get install -y -qq git
REMOTE

# ---------- clone or update repo ----------
log "Syncing $REPO @ $REF to $APP_DIR..."
ssh_exec "mkdir -p '$APP_DIR' '$DATA_DIR' /etc/agentboard"
ssh_exec "chown -R root:root '$APP_DIR'"

ssh_script <<REMOTE
cd '$APP_DIR'
if [ -d .git ]; then
	git fetch --all --tags --prune
	git checkout '$REF'
	# Pull only if REF is a branch (not a tag or SHA); harmless if it isn't.
	git symbolic-ref -q HEAD >/dev/null && git pull --ff-only || true
else
	git clone '$REPO' .
	git checkout '$REF'
fi
REMOTE

# ---------- token handling ----------
# Reuse the existing token on re-run so the URL keeps working. Only generate
# when the instance is fresh or the operator explicitly passes --token.
if [[ -z "$TOKEN" ]]; then
	EXISTING=$(ssh_exec "grep -E '^AGENTBOARD_AUTH_TOKEN=' /etc/agentboard/env 2>/dev/null | cut -d= -f2- || true")
	if [[ -n "$EXISTING" ]]; then
		log "Reusing existing auth token from /etc/agentboard/env"
		TOKEN="$EXISTING"
	else
		TOKEN=$(openssl rand -hex 32)
		log "Generated new auth token"
	fi
fi

# ---------- upload compose + caddyfile ----------
log "Uploading compose + Caddyfile..."
scp_file "$TPL_DIR/docker-compose.yml" "$APP_DIR/scripts/vps/docker-compose.deploy.yml"

if [[ -n "$DOMAIN" ]]; then
	RENDERED=$(mktemp)
	sed "s#{{DOMAIN}}#$DOMAIN#g" "$TPL_DIR/Caddyfile.tls.tpl" > "$RENDERED"
	scp_file "$RENDERED" "$APP_DIR/scripts/vps/Caddyfile"
	rm -f "$RENDERED"
else
	scp_file "$TPL_DIR/Caddyfile.http" "$APP_DIR/scripts/vps/Caddyfile"
fi

# ---------- env file (source of truth for config) ----------
log "Writing /etc/agentboard/env..."
ssh_script <<REMOTE
umask 077
cat > /etc/agentboard/env <<EOF
# Managed by scripts/deploy-vps.sh. Re-running the script will preserve
# AGENTBOARD_AUTH_TOKEN unless --token is passed explicitly.
AGENTBOARD_AUTH_TOKEN=$TOKEN
AGENTBOARD_DOMAIN=${DOMAIN:-}
AGENTBOARD_DATA_DIR=$DATA_DIR
EOF
chmod 600 /etc/agentboard/env
REMOTE

# ---------- build + up ----------
COMPOSE_DIR="$APP_DIR/scripts/vps"
COMPOSE="docker compose -f '$COMPOSE_DIR/docker-compose.deploy.yml' --env-file /etc/agentboard/env --project-directory '$COMPOSE_DIR'"

log "Building image (this can take a few minutes on first run)..."
if ! ssh_exec "cd '$APP_DIR' && $COMPOSE build"; then
	die "docker compose build failed" 30
fi

log "Starting containers..."
if ! ssh_exec "cd '$APP_DIR' && $COMPOSE up -d --remove-orphans"; then
	die "docker compose up failed" 30
fi

# ---------- health check ----------
HOSTNAME=${HOST#*@}
if [[ -n "$DOMAIN" ]]; then
	URL="https://$DOMAIN"
else
	URL="http://$HOSTNAME"
fi

log "Waiting for $URL/api/health (timeout ${TIMEOUT}s)..."
deadline=$(( $(date +%s) + TIMEOUT ))
healthy=false
while (( $(date +%s) < deadline )); do
	if curl -fsS -o /dev/null --max-time 5 -k "$URL/api/health" 2>/dev/null; then
		healthy=true
		break
	fi
	sleep 3
done

if ! $healthy; then
	err "Did not become healthy in ${TIMEOUT}s. Recent logs:"
	ssh_exec "cd '$APP_DIR' && $COMPOSE logs --tail 80" >&2 || true
	exit 10
fi

VERSION=$(ssh_exec "cd '$APP_DIR' && git rev-parse --short HEAD")

# ---------- output ----------
if $JSON_OUT; then
	printf '{"url":"%s","token":"%s","version":"%s","host":"%s","domain":"%s"}\n' \
		"$URL" "$TOKEN" "$VERSION" "$HOST" "$DOMAIN"
else
	printf >&2 '\n\033[32m✓\033[0m AgentBoard deployed.\n\n'
	printf >&2 '  URL:     %s\n'   "$URL"
	printf >&2 '  Token:   %s\n'   "$TOKEN"
	printf >&2 '  Version: %s (%s)\n' "$VERSION" "$REF"
	printf >&2 '  Host:    %s\n'   "$HOST"
	printf >&2 '  Data:    %s\n\n' "$DATA_DIR"
	printf >&2 'Save the token — it gates every request except /api/health.\n'
	printf >&2 'Re-run this script with the same --host to update.\n\n'
	printf >&2 'Auth header examples:\n'
	printf >&2 "  curl -H 'Authorization: Bearer %s' %s/api/data\n" "$TOKEN" "$URL"
	printf >&2 "  curl -u :%s %s/api/data\n" "$TOKEN" "$URL"
	printf >&2 '  curl %s/api/data?token=%s\n' "$URL" "$TOKEN"
fi
