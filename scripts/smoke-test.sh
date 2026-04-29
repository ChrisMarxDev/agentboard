#!/usr/bin/env bash
# Smoke test for the files-first store at /api/data/<key> +
# /api/index, /api/search, /api/activity, /api/files/request-upload.
# Boots a fresh agentboard binary with a throwaway project + dummy
# auth token, hits every store endpoint, asserts the contract.
# Self-contained: runs anywhere `go` is on PATH.
#
# Usage:
#   scripts/smoke-test.sh [PORT]
set -euo pipefail

PORT=${1:-3399}
BINARY="${BINARY:-./agentboard}"
TOKEN="smoke-token-$$"
URL="http://localhost:$PORT"
TEST_PROJECT="/tmp/agentboard-store-smoke-$$"

GREEN='\033[0;32m'; RED='\033[0;31m'; NC='\033[0m'
PASS=0; FAIL=0

pass() { echo -e "${GREEN}PASS${NC} $1"; PASS=$((PASS + 1)); }
fail() { echo -e "${RED}FAIL${NC} $1: $2"; FAIL=$((FAIL + 1)); }

authed_curl() {
  curl -s -H "Authorization: Bearer $TOKEN" "$@"
}

assert_status() {
  local desc="$1" method="$2" path="$3" expected="$4"
  shift 4
  local status
  status=$(authed_curl -o /dev/null -w '%{http_code}' -X "$method" "$URL$path" "$@")
  if [ "$status" = "$expected" ]; then pass "$desc"; else fail "$desc" "want $expected got $status"; fi
}

assert_jq() {
  local desc="$1" path="$2" jq_expr="$3" expected="$4"
  local actual
  actual=$(authed_curl "$URL$path" | jq -r "$jq_expr" 2>/dev/null || echo "ERROR")
  if [ "$actual" = "$expected" ]; then pass "$desc"; else fail "$desc" "want '$expected' got '$actual'"; fi
}

echo "=== files-first store smoke test ==="
echo ""

# Build if missing
if [ ! -f "$BINARY" ]; then
  echo "Building $BINARY..."
  go build -o "$BINARY" ./cmd/agentboard
fi

# Boot
rm -rf "$TEST_PROJECT"
AGENTBOARD_AUTH_TOKEN="$TOKEN" "$BINARY" --path "$TEST_PROJECT" --port "$PORT" --no-open > /tmp/store-smoke-$$.log 2>&1 &
SERVER_PID=$!
sleep 2

cleanup() {
  kill $SERVER_PID 2>/dev/null || true
  wait $SERVER_PID 2>/dev/null || true
  rm -rf "$TEST_PROJECT"
}
trap cleanup EXIT

# --- Tier 1: index ---
assert_status "GET /api/index"        GET "/api/index" 200
assert_jq     "index has data array"     "/api/index" ".data | type" "array"

# --- Singleton ---
assert_status "PUT singleton fresh"      PUT "/api/data/smoke.k" 200 -H 'Content-Type: application/json' -d '{"value":42}'
assert_jq     "GET singleton value"      "/api/data/smoke.k" ".value" "42"
assert_jq     "shape == singleton"       "/api/data/smoke.k" "._meta.shape" "singleton"

VERSION=$(authed_curl "$URL/api/data/smoke.k" | jq -r "._meta.version")

assert_status "PUT with matching ver"    PUT "/api/data/smoke.k" 200 \
  -H 'Content-Type: application/json' -d "{\"_meta\":{\"version\":\"$VERSION\"},\"value\":100}"

assert_status "PUT with stale ver -> 412" PUT "/api/data/smoke.k" 412 \
  -H 'Content-Type: application/json' -d '{"_meta":{"version":"1999-01-01T00:00:00Z"},"value":999}'

# --- Merge (never conflicts) ---
assert_status "PATCH merge create"       PATCH "/api/data/smoke.cfg" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"a":1,"b":2}}'
assert_status "PATCH merge update"       PATCH "/api/data/smoke.cfg" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"a":99,"c":3}}'
assert_jq     "deep merge preserved b"    "/api/data/smoke.cfg" ".value.b" "2"
assert_jq     "deep merge updated a"      "/api/data/smoke.cfg" ".value.a" "99"
assert_jq     "deep merge added c"        "/api/data/smoke.cfg" ".value.c" "3"

# No atomic field-level increment / CAS — agents read-modify-write
# the whole doc under file-level _meta.version CAS.

# --- Collection ---
assert_status "Upsert task-1"            PUT "/api/data/smoke.kanban/task-1" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"title":"a","col":"todo"}}'
assert_status "Upsert task-2"            PUT "/api/data/smoke.kanban/task-2" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"title":"b","col":"todo"}}'
assert_jq     "list 2 items"             "/api/data/smoke.kanban" ".items | length" "2"
assert_jq     "merge_by_id preserves"     "/api/data/smoke.kanban/task-1" ".value.title" "a"
assert_status "MERGE_BY_ID col field"    PATCH "/api/data/smoke.kanban/task-1" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"col":"in-progress"}}'
assert_jq     "title preserved"           "/api/data/smoke.kanban/task-1" ".value.title" "a"
assert_jq     "col updated"               "/api/data/smoke.kanban/task-1" ".value.col" "in-progress"

# --- Stream ---
assert_status "Stream append single"     POST "/api/data/smoke.events?op=append" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"e":"signup"}}'
assert_status "Stream append batch"      POST "/api/data/smoke.events?op=append" 200 \
  -H 'Content-Type: application/json' -d '{"items":[{"e":"login"},{"e":"view"},{"e":"logout"}]}'
assert_jq     "stream tail 4 lines"      "/api/data/smoke.events?limit=10" ".lines | length" "4"
assert_jq     "shape stream"             "/api/data/smoke.events" "._meta.shape" "stream"

# --- Wrong shape ---
assert_status "APPEND on singleton -> 409" POST "/api/data/smoke.k?op=append" 409 \
  -H 'Content-Type: application/json' -d '{"value":1}'

# --- Search ---
assert_jq     "search finds value"        "/api/search?q=signup" ".results | length > 0" "true"

# --- History + activity ---
assert_jq     "history has entries"       "/api/data/smoke.k/history" ".entries | length > 0" "true"
assert_jq     "activity has entries"      "/api/activity?path_prefix=smoke" ".entries | length > 0" "true"

# # --- Presigned upload (spec §12) ---
MINT=$(authed_curl -X POST -H 'Content-Type: application/json' \
  "$URL/api/files/request-upload" -d '{"name":"smoke.txt","size_bytes":12}')
UPLOAD_URL=$(echo "$MINT" | jq -r .upload_url)
if [ "$UPLOAD_URL" != "null" ] && [ -n "$UPLOAD_URL" ]; then pass "Mint upload URL"; else fail "Mint upload URL" "no url returned"; fi

UPLOAD_STATUS=$(echo -n "Hello smoke" | curl -s -X PUT --data-binary @- -o /dev/null -w "%{http_code}" "$UPLOAD_URL")
[ "$UPLOAD_STATUS" = "200" ] && pass "Presigned PUT" || fail "Presigned PUT" "got $UPLOAD_STATUS"

REPLAY_STATUS=$(echo -n "replay" | curl -s -X PUT --data-binary @- -o /dev/null -w "%{http_code}" "$UPLOAD_URL")
[ "$REPLAY_STATUS" = "401" ] && pass "One-shot enforced" || fail "One-shot enforced" "got $REPLAY_STATUS (want 401)"

assert_status "Read back uploaded file"  GET "/api/files/smoke.txt" 200

# --- Delete ---
assert_status "Delete singleton"         DELETE "/api/data/smoke.k" 204
assert_status "GET after delete -> 404"  GET    "/api/data/smoke.k" 404
assert_status "Delete by id"             DELETE "/api/data/smoke.kanban/task-1" 204
assert_status "Delete collection needs confirm" DELETE "/api/data/smoke.kanban" 400

# --- Summary ---
echo ""
echo "=== Results ==="
echo "Passed: $PASS"
echo "Failed: $FAIL"
[ "$FAIL" -eq 0 ]
