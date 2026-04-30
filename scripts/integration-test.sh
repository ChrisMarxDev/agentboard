#!/usr/bin/env bash
# End-to-end integration test for AgentBoard.
#
# Boots a clean binary against a throwaway project, walks the
# bootstrap flow (first-admin invite + password redeem), exercises
# both credential paths (Bearer token + cookie session), and hits
# every public surface that ships in the binary today. Designed to
# be the smoke gate for "is this build shippable" — it covers
# bootstrap, auth, content, files-first store, files, components,
# MCP, SSE, and CSRF.
#
# Usage:
#   scripts/integration-test.sh [PORT]
#
# No external deps beyond `curl`, `jq`, `go`. Self-contained.

set -euo pipefail

PORT=${1:-3399}
BINARY="${BINARY:-./agentboard}"
URL="http://localhost:$PORT"
TEST_PROJECT="/tmp/agentboard-integration-$$"
COOKIE_JAR="/tmp/agentboard-jar-$$"

GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[0;33m'; NC='\033[0m'
PASS=0
FAIL=0

pass() { echo -e "${GREEN}PASS${NC} $1"; PASS=$((PASS + 1)); }
fail() { echo -e "${RED}FAIL${NC} $1: $2"; FAIL=$((FAIL + 1)); }
note() { echo -e "${YELLOW}~~${NC} $1"; }

# assert_status PROVIDES the auth header — every gated request uses
# Bearer auth except where we explicitly want to test the cookie /
# anonymous path.
authed() {
  curl -s -H "Authorization: Bearer $TOKEN" "$@"
}

assert_status() {
  local desc="$1" method="$2" path="$3" expected="$4"
  shift 4
  local status
  status=$(authed -o /dev/null -w '%{http_code}' -X "$method" "$URL$path" "$@")
  if [ "$status" = "$expected" ]; then
    pass "$desc"
  else
    fail "$desc" "expected $expected, got $status"
  fi
}

assert_jq() {
  local desc="$1" path="$2" jq_expr="$3" expected="$4"
  local actual
  actual=$(authed "$URL$path" | jq -r "$jq_expr" 2>/dev/null || echo "ERROR")
  if [ "$actual" = "$expected" ]; then
    pass "$desc"
  else
    fail "$desc" "expected '$expected', got '$actual'"
  fi
}

assert_jq_at_least() {
  local desc="$1" path="$2" jq_expr="$3" minimum="$4"
  local actual
  actual=$(authed "$URL$path" | jq -r "$jq_expr" 2>/dev/null || echo "0")
  if [ "$actual" -ge "$minimum" ] 2>/dev/null; then
    pass "$desc ($actual ≥ $minimum)"
  else
    fail "$desc" "expected ≥$minimum, got '$actual'"
  fi
}

echo "=== AgentBoard integration test ==="
echo ""

# ----- Build -----
echo "Building..."
if [ ! -f "$BINARY" ]; then
  go build -o "$BINARY" ./cmd/agentboard
fi

rm -rf "$TEST_PROJECT" "$COOKIE_JAR"

cleanup() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TEST_PROJECT" "$COOKIE_JAR"
}
trap cleanup EXIT

# ----- Boot -----
echo "Starting server on port $PORT..."
"$BINARY" --path "$TEST_PROJECT" --port "$PORT" --no-open \
  > /tmp/agentboard-integration-$$.log 2>&1 &
SERVER_PID=$!
sleep 2

# Boot health: only health is open before bootstrap.
HEALTH_STATUS=$(curl -s -o /dev/null -w '%{http_code}' "$URL/api/health")
[ "$HEALTH_STATUS" = "200" ] && pass "GET /api/health (open, pre-bootstrap)" \
  || fail "GET /api/health" "got $HEALTH_STATUS"

SETUP_STATUS=$(curl -s -o /dev/null -w '%{http_code}' "$URL/api/setup/status")
[ "$SETUP_STATUS" = "200" ] && pass "GET /api/setup/status (open)" \
  || fail "GET /api/setup/status" "got $SETUP_STATUS"

# ----- Bootstrap: first-admin invite redeem -----
note "Bootstrapping first admin via the invitation URL printed at boot"

INVITE_URL_FILE="$TEST_PROJECT/.agentboard/first-admin-invite.url"
for i in 1 2 3 4 5; do
  [ -f "$INVITE_URL_FILE" ] && break
  sleep 1
done
[ -f "$INVITE_URL_FILE" ] || { fail "first-admin-invite.url" "not written"; exit 1; }

INVITE_ID=$(grep -oE '/invite/[a-zA-Z0-9_-]+' "$INVITE_URL_FILE" | head -1 | sed 's|/invite/||')
[ -n "$INVITE_ID" ] && pass "first-admin invite minted at boot" \
  || { fail "invite parse" "couldn't extract id from $INVITE_URL_FILE"; exit 1; }

REDEEM_RESP=$(curl -s -X POST "$URL/api/invitations/$INVITE_ID/redeem" \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"integration-test-pw-1234"}' \
  -c "$COOKIE_JAR")
TOKEN=$(echo "$REDEEM_RESP" | jq -r '.token')
[ -n "$TOKEN" ] && [ "$TOKEN" != "null" ] && pass "Invitation redeem returns a PAT" \
  || { fail "Invitation redeem" "no token in response: $REDEEM_RESP"; exit 1; }

# ----- Auth surface -----
echo ""
note "Auth surface (token + cookie + CSRF)"

# Bearer auth on /api/me.
ME_STATUS=$(authed -o /dev/null -w '%{http_code}' "$URL/api/me")
[ "$ME_STATUS" = "200" ] && pass "GET /api/me with Bearer" \
  || fail "GET /api/me" "got $ME_STATUS"

# Cookie was set by the redeem response — /api/auth/me with cookie works.
ME_COOKIE_STATUS=$(curl -s -o /dev/null -w '%{http_code}' \
  -b "$COOKIE_JAR" "$URL/api/auth/me")
[ "$ME_COOKIE_STATUS" = "200" ] && pass "GET /api/auth/me with cookie" \
  || fail "GET /api/auth/me cookie" "got $ME_COOKIE_STATUS"

# Wrong-password login returns 401 with the same shape as wrong-username.
WRONG_PW=$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"wrong-password"}' "$URL/api/auth/login")
[ "$WRONG_PW" = "401" ] && pass "POST /api/auth/login wrong-password → 401" \
  || fail "Wrong-password" "got $WRONG_PW"

# CSRF: cookie-authed write without X-CSRF-Token is 403.
CSRF_RESP=$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  -b "$COOKIE_JAR" -H 'Content-Type: application/json' \
  -d '{"label":"csrf-test"}' \
  "$URL/api/users/alice/tokens")
[ "$CSRF_RESP" = "403" ] && pass "Cookie POST without CSRF → 403" \
  || fail "CSRF enforcement" "got $CSRF_RESP"

# Bearer skips CSRF: same POST with bearer succeeds (mints a 2nd token).
BEARER_POST=$(authed -o /dev/null -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -d '{"label":"bearer-test"}' \
  "$URL/api/users/alice/tokens")
[ "$BEARER_POST" = "201" ] && pass "Bearer POST skips CSRF → 201" \
  || fail "Bearer skip CSRF" "got $BEARER_POST"

# ----- Files-first store: /api/<key> -----
echo ""
note "Files-first store"

assert_status "PUT singleton"   PUT "/api/test.k" 200 \
  -H 'Content-Type: application/json' -d '{"value":42}'
assert_jq     "GET singleton"   "/api/test.k" '.value' "42"
assert_jq     "shape singleton" "/api/test.k" '._meta.shape' "singleton"

VERSION=$(authed "$URL/api/test.k" | jq -r '._meta.version')
assert_status "PATCH merge create"  PATCH "/api/test.cfg" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"a":1,"b":2}}'
assert_status "PATCH merge update"  PATCH "/api/test.cfg" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"a":99,"c":3}}'
assert_jq     "deep merge a=99"     "/api/test.cfg" '.value.a' "99"
assert_jq     "deep merge b=2"      "/api/test.cfg" '.value.b' "2"
assert_jq     "deep merge c=3"      "/api/test.cfg" '.value.c' "3"

assert_status "Upsert collection 1"  PUT "/api/test.kanban/task-1" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"title":"a","col":"todo"}}'
assert_status "Upsert collection 2"  PUT "/api/test.kanban/task-2" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"title":"b","col":"todo"}}'
assert_jq     "List collection (2)"  "/api/test.kanban" '.items | length' "2"

assert_status "Stream append"  POST "/api/test.events:append" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"e":"signup"}}'
assert_jq "Stream tail" "/api/test.events?limit=10" '.lines | length' "1"

assert_status "Wrong shape (append on singleton) → 409" \
  POST "/api/test.k:append" 409 \
  -H 'Content-Type: application/json' -d '{"value":1}'

assert_status "DELETE singleton" DELETE "/api/test.k" 204

# ----- Content (MDX pages) -----
echo ""
note "Content / MDX pages"

assert_status "GET catalog"      GET "/api/index" 200
assert_status "GET home page"    GET "/api/" 200
assert_status "Write new page"   PUT "/api/scratch" 200 \
  -H 'Content-Type: text/markdown' -d $'# Scratch\n\nIntegration test page.'
assert_status "Read it back"     GET "/api/scratch" 200
assert_status "DELETE protected index → 400" DELETE "/api/index" 400
assert_status "DELETE scratch"   DELETE "/api/scratch" 200

# ----- Files (binary upload via presigned URL) -----
echo ""
note "Files (presigned upload + serve)"

MINT=$(authed -X POST -H 'Content-Type: application/json' \
  "$URL/api/files/request-upload" -d '{"name":"smoke.txt","size_bytes":12}')
UPLOAD_URL=$(echo "$MINT" | jq -r '.upload_url')
[ "$UPLOAD_URL" != "null" ] && [ -n "$UPLOAD_URL" ] && pass "Mint upload URL" \
  || fail "Mint upload URL" "no url returned: $MINT"

UPLOAD_STATUS=$(echo -n "Hello smoke" | curl -s -X PUT --data-binary @- \
  -o /dev/null -w '%{http_code}' "$UPLOAD_URL")
[ "$UPLOAD_STATUS" = "200" ] && pass "Presigned PUT" \
  || fail "Presigned PUT" "got $UPLOAD_STATUS"

REPLAY_STATUS=$(echo -n "replay" | curl -s -X PUT --data-binary @- \
  -o /dev/null -w '%{http_code}' "$UPLOAD_URL")
[ "$REPLAY_STATUS" = "401" ] && pass "One-shot upload-token enforced" \
  || fail "One-shot enforced" "got $REPLAY_STATUS (want 401)"

assert_status "GET uploaded file" GET "/api/files/smoke.txt" 200

# ----- Components catalog -----
echo ""
note "Component catalog"

assert_status     "GET components"     GET "/api/components" 200
assert_jq_at_least "Component count"   "/api/components" '. | length' 30

# ----- MCP -----
echo ""
note "MCP (JSON-RPC over Streamable HTTP)"

INIT_RESP=$(curl -s -X POST "$URL/mcp" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}')
[ -n "$INIT_RESP" ] && pass "MCP initialize returns a response" \
  || fail "MCP initialize" "no response"

TOOL_COUNT=$(curl -s -X POST "$URL/mcp" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | jq -r '.result.tools | length' 2>/dev/null || echo 0)
# Cut 6 collapsed the MCP surface from ~38 tools across 10 domains to
# exactly 10 (8 generic batch CRUD + agentboard_grab + _fire_event).
# Per spec §6, this count is locked. Adding a tool requires a spec
# update and a corresponding bump here.
if [ "$TOOL_COUNT" = "10" ]; then
  pass "MCP tools/list = 10 tools (spec §6 lock)"
else
  fail "MCP tools/list" "expected exactly 10 tools, got '$TOOL_COUNT'"
fi

# ----- Index + search -----
echo ""
note "Index + search"

assert_status     "GET /api/index"      GET "/api/index" 200
assert_jq_at_least "/api/index has data" "/api/index" '.data | length' 1

# ----- SSE -----
echo ""
note "SSE"

SSE_STATUS=$(authed -o /dev/null -w '%{http_code}' --max-time 2 \
  "$URL/api/events" 2>/dev/null || true)
if [ "$SSE_STATUS" = "200" ] || [ -z "$SSE_STATUS" ]; then
  pass "SSE endpoint connects"
else
  fail "SSE" "unexpected status $SSE_STATUS"
fi

# ----- OAuth discovery (unauth-readable) -----
echo ""
note "OAuth / MCP authorization-server discovery"

PRM_STATUS=$(curl -s -o /dev/null -w '%{http_code}' \
  "$URL/.well-known/oauth-protected-resource")
[ "$PRM_STATUS" = "200" ] && pass "Protected-resource metadata (RFC 9728)" \
  || fail "PRM" "got $PRM_STATUS"

ASM_STATUS=$(curl -s -o /dev/null -w '%{http_code}' \
  "$URL/.well-known/oauth-authorization-server")
[ "$ASM_STATUS" = "200" ] && pass "Authorization-server metadata (RFC 8414)" \
  || fail "ASM" "got $ASM_STATUS"

# ----- Logout -----
echo ""
note "Logout flow"

LOGOUT_STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST \
  -b "$COOKIE_JAR" "$URL/api/auth/logout")
[ "$LOGOUT_STATUS" = "200" ] && pass "POST /api/auth/logout" \
  || fail "Logout" "got $LOGOUT_STATUS"

# /api/auth/me with the same cookie jar should now 401.
POST_LOGOUT=$(curl -s -o /dev/null -w '%{http_code}' \
  -b "$COOKIE_JAR" "$URL/api/auth/me")
[ "$POST_LOGOUT" = "401" ] && pass "Cookie /me 401 after logout" \
  || fail "Post-logout /me" "got $POST_LOGOUT"

# ----- Summary -----
echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
