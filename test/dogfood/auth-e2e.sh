#!/usr/bin/env bash
# Auth E2E self-test.
#
# Boots an ephemeral AgentBoard, then walks the full human-facing auth
# loop against it: anonymous probe → GET /login form → POST /login with
# good credentials → /_api/me works → GET /logout → anonymous again.
# Plus the /invite/<id> happy path.
#
# Used by the dev loop to verify auth fixes don't regress.
#
# Usage:
#   test/dogfood/auth-e2e.sh                # ephemeral, picks a free port
#   BASE_URL=https://agentboard.hextorical.com test/dogfood/auth-e2e.sh
#
# Exit 0 = all assertions passed; 1 = any failure (with diagnostic).

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="${AGENTBOARD_BIN:-$REPO_ROOT/agentboard}"

# Pick mode: local (ephemeral) or remote (provided BASE_URL).
if [[ -n "${BASE_URL:-}" ]]; then
  MODE="remote"
  REMOTE_BASE="$BASE_URL"
else
  MODE="local"
  PORT="${PORT:-38150}"
  REMOTE_BASE="http://localhost:$PORT"
  DATA_DIR="$(mktemp -d -t ab-e2e-XXXXXX)"
  JAR="$(mktemp -t ab-e2e-jar-XXXXXX)"
  "$BIN" --path "$DATA_DIR" --port "$PORT" --no-open \
    > "$DATA_DIR/server.log" 2>&1 &
  SRV_PID=$!
  trap '
    [[ -n "${SRV_PID:-}" ]] && kill "$SRV_PID" 2>/dev/null || true
    [[ -d "$DATA_DIR" ]] && rm -rf "$DATA_DIR"
    [[ -f "$JAR" ]] && rm -f "$JAR"
  ' EXIT
  # Wait for server to answer.
  for _ in $(seq 1 30); do
    if curl -sS --max-time 1 -o /dev/null "$REMOTE_BASE/_api/health" 2>/dev/null; then
      break
    fi
    sleep 0.5
  done
fi

if [[ "$MODE" == "remote" ]]; then
  JAR="$(mktemp -t ab-e2e-jar-XXXXXX)"
  trap 'rm -f "$JAR"' EXIT
fi

PASS=0
FAIL=0
FAILURES=()

check() {
  local name="$1" cmd="$2"
  local out rc
  out="$(bash -c "$cmd" 2>&1)" || rc=$? && rc=${rc:-0}
  if [[ $rc -eq 0 ]]; then
    PASS=$((PASS+1))
    echo "  ✓ $name"
  else
    FAIL=$((FAIL+1))
    FAILURES+=("$name → $out")
    echo "  ✗ $name"
    echo "      $out"
  fi
}

echo "▸ Mode: $MODE  Base: $REMOTE_BASE"

# ----- 1. Anonymous probes -----
echo "▸ Anonymous probes"
check "GET /_api/health is 200" \
  "[ \$(curl -sS -o /dev/null -w '%{http_code}' '$REMOTE_BASE/_api/health') = 200 ]"
check "GET /_api/me is 401 (no creds)" \
  "[ \$(curl -sS -o /dev/null -w '%{http_code}' '$REMOTE_BASE/_api/me') = 401 ]"
check "GET / returns 200 (anonymous dashboard read)" \
  "[ \$(curl -sS -o /dev/null -w '%{http_code}' '$REMOTE_BASE/') = 200 ]"

# ----- 2. Login UI -----
echo "▸ Login UI"
check "GET /login renders an HTML form (status 200)" \
  "[ \$(curl -sS -o /dev/null -w '%{http_code}' '$REMOTE_BASE/login') = 200 ]"
check "GET /login body contains username + password inputs" \
  "curl -sS '$REMOTE_BASE/login' | grep -qE 'name=\"username\"' && \
   curl -sS '$REMOTE_BASE/login' | grep -qE 'name=\"password\"'"
check "GET /logout redirects (302)" \
  "[ \$(curl -sS -o /dev/null -w '%{http_code}' '$REMOTE_BASE/logout') = 302 ]"

# ----- 3. Invite flow (local mode only — claims the first admin) -----
if [[ "$MODE" == "local" ]]; then
  echo "▸ Invite happy path"
  INV_FILE="$DATA_DIR/.agentboard/first-admin-invite.url"
  check "invite file exists" "[ -f '$INV_FILE' ]"
  INV_ID="$(grep -oE 'inv_[A-Za-z0-9_-]+' "$INV_FILE" | head -1 || true)"

  check "GET /invite/<id> renders form (200)" \
    "[ \$(curl -sS -o /dev/null -w '%{http_code}' '$REMOTE_BASE/invite/$INV_ID') = 200 ]"
  check "invite form has username + password inputs" \
    "curl -sS '$REMOTE_BASE/invite/$INV_ID' | grep -qE 'name=\"username\"' && \
     curl -sS '$REMOTE_BASE/invite/$INV_ID' | grep -qE 'name=\"password\"'"

  # POST the form to claim the admin account. Capture cookies in $JAR.
  REDEEM_HTTP="$(curl -sS -c "$JAR" -o /dev/null -w '%{http_code}' \
    -X POST -d 'username=e2eadmin&password=auth-e2e-test-pw-2026' \
    "$REMOTE_BASE/invite/$INV_ID")"
  check "POST /invite/<id> redirects (302)" "[ '$REDEEM_HTTP' = 302 ]"
  check "session cookie present in jar after redeem" \
    "grep -q agentboard_session '$JAR'"
  check "csrf cookie present in jar after redeem" \
    "grep -q agentboard_csrf '$JAR'"

  # /_api/me must now work over the cookie.
  check "GET /_api/me with cookie returns 200" \
    "[ \$(curl -sS -b '$JAR' -o /dev/null -w '%{http_code}' '$REMOTE_BASE/_api/me') = 200 ]"
  check "GET /_api/me body has expected username" \
    "curl -sS -b '$JAR' '$REMOTE_BASE/_api/me' | grep -q 'e2eadmin'"

  # GET / should show "@e2eadmin" in the header now.
  check "GET / shows @username after login" \
    "curl -sS -b '$JAR' '$REMOTE_BASE/' | grep -qE '@e2eadmin'"

  # /logout drops the cookie and redirects.
  rm -f "$JAR.new"
  LOGOUT_HTTP="$(curl -sS -b "$JAR" -c "$JAR.new" -o /dev/null -w '%{http_code}' \
    "$REMOTE_BASE/logout")"
  check "GET /logout returns 302" "[ '$LOGOUT_HTTP' = 302 ]"
  mv "$JAR.new" "$JAR"
  check "session cookie cleared (or invalid) after logout" \
    "[ \$(curl -sS -b '$JAR' -o /dev/null -w '%{http_code}' '$REMOTE_BASE/_api/me') = 401 ]"

  echo "▸ Re-login as the claimed account"
  rm -f "$JAR" && touch "$JAR"
  LOGIN_HTTP="$(curl -sS -c "$JAR" -o /dev/null -w '%{http_code}' \
    -X POST -d 'username=e2eadmin&password=auth-e2e-test-pw-2026' \
    "$REMOTE_BASE/login")"
  check "POST /login (good creds) redirects (302)" "[ '$LOGIN_HTTP' = 302 ]"
  check "/_api/me with fresh login cookie returns 200" \
    "[ \$(curl -sS -b '$JAR' -o /dev/null -w '%{http_code}' '$REMOTE_BASE/_api/me') = 200 ]"

  echo "▸ Bad-credentials probe"
  BAD_HTTP="$(curl -sS -o /tmp/bad-login.html -w '%{http_code}' \
    -X POST -d 'username=e2eadmin&password=wrong' \
    "$REMOTE_BASE/login")"
  check "POST /login (bad creds) re-renders form (200, not 302)" \
    "[ '$BAD_HTTP' = 200 ]"
  check "bad-login response shows an error message" \
    "grep -qi 'incorrect' /tmp/bad-login.html"
fi

echo
echo "▸ Result: $PASS passed, $FAIL failed"
if [[ $FAIL -gt 0 ]]; then
  echo
  printf '  - %s\n' "${FAILURES[@]}"
  exit 1
fi
exit 0
