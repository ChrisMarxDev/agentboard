#!/usr/bin/env bash
# Smoke test for /api/v2 (files-first store).
# Boots a fresh agentboard binary with a throwaway project + dummy auth
# token, hits every v2 endpoint, asserts the contract. Self-contained:
# you can run this anywhere as long as `go` is on PATH.
#
# Usage:
#   scripts/v2-smoke-test.sh [PORT]
set -euo pipefail

PORT=${1:-3399}
BINARY="${BINARY:-./agentboard}"
TOKEN="smoke-token-$$"
URL="http://localhost:$PORT"
TEST_PROJECT="/tmp/agentboard-v2-smoke-$$"

GREEN='\033[0;32m'; RED='\033[0;31m'; NC='\033[0m'
PASS=0; FAIL=0

pass() { echo -e "${GREEN}PASS${NC} $1"; PASS=$((PASS + 1)); }
fail() { echo -e "${RED}FAIL${NC} $1: $2"; FAIL=$((FAIL + 1)); }

curl_v2() {
  curl -s -H "Authorization: Bearer $TOKEN" "$@"
}

assert_status() {
  local desc="$1" method="$2" path="$3" expected="$4"
  shift 4
  local status
  status=$(curl_v2 -o /dev/null -w '%{http_code}' -X "$method" "$URL$path" "$@")
  if [ "$status" = "$expected" ]; then pass "$desc"; else fail "$desc" "want $expected got $status"; fi
}

assert_jq() {
  local desc="$1" path="$2" jq_expr="$3" expected="$4"
  local actual
  actual=$(curl_v2 "$URL$path" | jq -r "$jq_expr" 2>/dev/null || echo "ERROR")
  if [ "$actual" = "$expected" ]; then pass "$desc"; else fail "$desc" "want '$expected' got '$actual'"; fi
}

echo "=== /api/v2 smoke test ==="
echo ""

# Build if missing
if [ ! -f "$BINARY" ]; then
  echo "Building $BINARY..."
  go build -o "$BINARY" ./cmd/agentboard
fi

# Boot
rm -rf "$TEST_PROJECT"
AGENTBOARD_AUTH_TOKEN="$TOKEN" "$BINARY" --path "$TEST_PROJECT" --port "$PORT" --no-open > /tmp/v2-smoke-$$.log 2>&1 &
SERVER_PID=$!
sleep 2

cleanup() {
  kill $SERVER_PID 2>/dev/null || true
  wait $SERVER_PID 2>/dev/null || true
  rm -rf "$TEST_PROJECT"
}
trap cleanup EXIT

# --- Tier 1: index ---
assert_status "GET /api/v2/index"        GET "/api/v2/index" 200
assert_jq     "index has data array"     "/api/v2/index" ".data | type" "array"

# --- Singleton ---
assert_status "PUT singleton fresh"      PUT "/api/v2/data/smoke.k" 200 -H 'Content-Type: application/json' -d '{"value":42}'
assert_jq     "GET singleton value"      "/api/v2/data/smoke.k" ".value" "42"
assert_jq     "shape == singleton"       "/api/v2/data/smoke.k" "._meta.shape" "singleton"

VERSION=$(curl_v2 "$URL/api/v2/data/smoke.k" | jq -r "._meta.version")

assert_status "PUT with matching ver"    PUT "/api/v2/data/smoke.k" 200 \
  -H 'Content-Type: application/json' -d "{\"_meta\":{\"version\":\"$VERSION\"},\"value\":100}"

assert_status "PUT with stale ver -> 412" PUT "/api/v2/data/smoke.k" 412 \
  -H 'Content-Type: application/json' -d '{"_meta":{"version":"1999-01-01T00:00:00Z"},"value":999}'

# --- Merge (never conflicts) ---
assert_status "PATCH merge create"       PATCH "/api/v2/data/smoke.cfg" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"a":1,"b":2}}'
assert_status "PATCH merge update"       PATCH "/api/v2/data/smoke.cfg" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"a":99,"c":3}}'
assert_jq     "deep merge preserved b"    "/api/v2/data/smoke.cfg" ".value.b" "2"
assert_jq     "deep merge updated a"      "/api/v2/data/smoke.cfg" ".value.a" "99"
assert_jq     "deep merge added c"        "/api/v2/data/smoke.cfg" ".value.c" "3"

# --- Increment ---
assert_status "INCREMENT first"          POST "/api/v2/data/smoke.counter?op=increment" 200 \
  -H 'Content-Type: application/json' -d '{"by":1}'
assert_status "INCREMENT again"          POST "/api/v2/data/smoke.counter?op=increment" 200 \
  -H 'Content-Type: application/json' -d '{"by":41}'
assert_jq     "counter is 42"             "/api/v2/data/smoke.counter" ".value" "42"

# --- CAS action ---
assert_status "CAS mismatch -> 409"      POST "/api/v2/data/smoke.counter?op=cas" 409 \
  -H 'Content-Type: application/json' -d '{"expected":999,"new":0}'

# --- Collection ---
assert_status "Upsert task-1"            PUT "/api/v2/data/smoke.kanban/task-1" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"title":"a","col":"todo"}}'
assert_status "Upsert task-2"            PUT "/api/v2/data/smoke.kanban/task-2" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"title":"b","col":"todo"}}'
assert_jq     "list 2 items"             "/api/v2/data/smoke.kanban" ".items | length" "2"
assert_jq     "merge_by_id preserves"     "/api/v2/data/smoke.kanban/task-1" ".value.title" "a"
assert_status "MERGE_BY_ID col field"    PATCH "/api/v2/data/smoke.kanban/task-1" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"col":"in-progress"}}'
assert_jq     "title preserved"           "/api/v2/data/smoke.kanban/task-1" ".value.title" "a"
assert_jq     "col updated"               "/api/v2/data/smoke.kanban/task-1" ".value.col" "in-progress"

# --- Stream ---
assert_status "Stream append single"     POST "/api/v2/data/smoke.events?op=append" 200 \
  -H 'Content-Type: application/json' -d '{"value":{"e":"signup"}}'
assert_status "Stream append batch"      POST "/api/v2/data/smoke.events?op=append" 200 \
  -H 'Content-Type: application/json' -d '{"items":[{"e":"login"},{"e":"view"},{"e":"logout"}]}'
assert_jq     "stream tail 4 lines"      "/api/v2/data/smoke.events?limit=10" ".lines | length" "4"
assert_jq     "shape stream"             "/api/v2/data/smoke.events" "._meta.shape" "stream"

# --- Wrong shape ---
assert_status "APPEND on singleton -> 409" POST "/api/v2/data/smoke.k?op=append" 409 \
  -H 'Content-Type: application/json' -d '{"value":1}'

# --- Search ---
assert_jq     "search finds value"        "/api/v2/search?q=signup" ".results | length > 0" "true"

# --- History + activity ---
assert_jq     "history has entries"       "/api/v2/data/smoke.k/history" ".entries | length > 0" "true"
assert_jq     "activity has entries"      "/api/v2/activity?path_prefix=smoke" ".entries | length > 0" "true"

# # --- Presigned upload (spec §12) ---
MINT=$(curl_v2 -X POST -H 'Content-Type: application/json' \
  "$URL/api/v2/files/request-upload" -d '{"name":"smoke.txt","size_bytes":12}')
UPLOAD_URL=$(echo "$MINT" | jq -r .upload_url)
if [ "$UPLOAD_URL" != "null" ] && [ -n "$UPLOAD_URL" ]; then pass "Mint upload URL"; else fail "Mint upload URL" "no url returned"; fi

UPLOAD_STATUS=$(echo -n "Hello smoke" | curl -s -X PUT --data-binary @- -o /dev/null -w "%{http_code}" "$UPLOAD_URL")
[ "$UPLOAD_STATUS" = "200" ] && pass "Presigned PUT" || fail "Presigned PUT" "got $UPLOAD_STATUS"

REPLAY_STATUS=$(echo -n "replay" | curl -s -X PUT --data-binary @- -o /dev/null -w "%{http_code}" "$UPLOAD_URL")
[ "$REPLAY_STATUS" = "401" ] && pass "One-shot enforced" || fail "One-shot enforced" "got $REPLAY_STATUS (want 401)"

assert_status "Read back uploaded file"  GET "/api/files/smoke.txt" 200

# --- Delete ---
assert_status "Delete singleton"         DELETE "/api/v2/data/smoke.k" 204
assert_status "GET after delete -> 404"  GET    "/api/v2/data/smoke.k" 404
assert_status "Delete by id"             DELETE "/api/v2/data/smoke.kanban/task-1" 204
assert_status "Delete collection needs confirm" DELETE "/api/v2/data/smoke.kanban" 400

# --- Summary ---
echo ""
echo "=== Results ==="
echo "Passed: $PASS"
echo "Failed: $FAIL"
[ "$FAIL" -eq 0 ]
