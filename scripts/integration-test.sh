#!/usr/bin/env bash
# Integration test for AgentBoard
# Starts the server, runs API tests, optionally runs browser tests with $B (gstack browse)
set -euo pipefail

PORT=${1:-3399}
BINARY="./agentboard"
URL="http://localhost:$PORT"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'
PASS=0
FAIL=0

pass() { echo -e "${GREEN}PASS${NC} $1"; PASS=$((PASS + 1)); }
fail() { echo -e "${RED}FAIL${NC} $1: $2"; FAIL=$((FAIL + 1)); }

assert_status() {
  local desc="$1" method="$2" path="$3" expected="$4"
  shift 4
  local status
  status=$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' -X "$method" "$URL$path" "$@")
  if [ "$status" = "$expected" ]; then
    pass "$desc"
  else
    fail "$desc" "expected $expected, got $status"
  fi
}

assert_json() {
  local desc="$1" path="$2" jq_expr="$3" expected="$4"
  local actual
  actual=$(curl -s --max-time 5 "$URL$path" | python3 -c "import sys,json; d=json.load(sys.stdin); print($jq_expr)" 2>/dev/null || echo "ERROR")
  if [ "$actual" = "$expected" ]; then
    pass "$desc"
  else
    fail "$desc" "expected '$expected', got '$actual'"
  fi
}

echo "=== AgentBoard Integration Tests ==="
echo ""

# Build
echo "Building..."
if [ ! -f "$BINARY" ]; then
  go build -o "$BINARY" ./cmd/agentboard
fi

# Clean up any previous test project
TEST_PROJECT="/tmp/agentboard-test-$$"
rm -rf "$TEST_PROJECT"

# Start server
echo "Starting server on port $PORT..."
"$BINARY" --path "$TEST_PROJECT" --port "$PORT" --no-open > /dev/null 2>&1 &
SERVER_PID=$!
sleep 2

cleanup() {
  kill $SERVER_PID 2>/dev/null || true
  wait $SERVER_PID 2>/dev/null || true
  rm -rf "$TEST_PROJECT"
}
trap cleanup EXIT

echo ""
echo "--- API Tests ---"

# Health
assert_status "GET /api/health" GET "/api/health" 200
assert_json "health ok" "/api/health" "d['ok']" "True"
assert_json "health version" "/api/health" "d['version']" "0.1.0"

# Data operations
assert_status "SET value" PUT "/api/data/test.num" 200 -d '42' -H 'Content-Type: application/json'
assert_json "GET value" "/api/data/test.num" "d['value']" "42"

assert_status "SET object" PUT "/api/data/test.obj" 200 -d '{"a":1,"b":2}' -H 'Content-Type: application/json'
assert_status "MERGE object" PATCH "/api/data/test.obj" 200 -d '{"b":3,"c":4}' -H 'Content-Type: application/json'
assert_json "MERGE result" "/api/data/test.obj" "d['value']" "{'a': 1, 'b': 3, 'c': 4}"

assert_status "APPEND item 1" POST "/api/data/test.log" 200 -d '{"msg":"one"}' -H 'Content-Type: application/json'
assert_status "APPEND item 2" POST "/api/data/test.log" 200 -d '{"msg":"two"}' -H 'Content-Type: application/json'
assert_json "APPEND result count" "/api/data/test.log" "len(d['value'])" "2"

assert_status "UPSERT by ID" PUT "/api/data/test.items/abc" 200 -d '{"name":"test"}' -H 'Content-Type: application/json'
assert_status "GET by ID" GET "/api/data/test.items/abc" 200

assert_status "DELETE key" DELETE "/api/data/test.num" 200
assert_status "GET deleted key" GET "/api/data/test.num" 404

assert_status "Invalid JSON" PUT "/api/data/bad" 400 -d 'not json' -H 'Content-Type: application/json'

# Schema
assert_status "GET schema" GET "/api/data/schema" 200

# Pages
assert_status "GET pages list" GET "/api/pages" 200
assert_status "GET index page" GET "/api/pages/index" 200
assert_status "Write new page" PUT "/api/pages/test-page" 200 -d '# Test Page' -H 'Content-Type: text/markdown'
assert_status "Cannot delete index" DELETE "/api/pages/index" 400

# Components
assert_status "GET components" GET "/api/components" 200
assert_json "9 built-in components" "/api/components" "len(d)" "9"

# Config
assert_status "GET config" GET "/api/config" 200

# Skill
assert_status "GET skill" GET "/skill" 200

# MCP
assert_status "MCP initialize" POST "/mcp" 200 \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  -H 'Content-Type: application/json'

MCP_TOOLS=$(curl -s --max-time 5 -X POST "$URL/mcp" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d['result']['tools']))" 2>/dev/null)
if [ "$MCP_TOOLS" = "13" ]; then
  pass "MCP 13 tools registered"
else
  fail "MCP tools count" "expected 13, got $MCP_TOOLS"
fi

# SSE endpoint (verify it connects — use timeout since SSE is streaming)
SSE_STATUS=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$URL/api/events" 2>/dev/null || true)
if [ "$SSE_STATUS" = "200" ] || [ -z "$SSE_STATUS" ]; then
  pass "SSE endpoint connectable"
else
  fail "SSE endpoint" "unexpected status $SSE_STATUS"
fi

# Frontend
FRONTEND_STATUS=$(curl -s -o /dev/null -w '%{http_code}' "$URL/")
if [ "$FRONTEND_STATUS" = "200" ]; then
  pass "Frontend serves at /"
else
  fail "Frontend" "expected 200, got $FRONTEND_STATUS"
fi

echo ""
echo "--- Browser Tests (requires gstack browse) ---"

# Check if $B (gstack browse) is available
if command -v "$HOME/.claude/skills/gstack/browse/bin/browse" &>/dev/null || command -v browse &>/dev/null; then
  B="${HOME}/.claude/skills/gstack/browse/bin/browse"

  $B goto "$URL" 2>/dev/null
  sleep 1

  # Check page title
  PAGE_TEXT=$($B text 2>/dev/null || echo "")
  if echo "$PAGE_TEXT" | grep -q "AgentBoard"; then
    pass "Browser: AgentBoard title visible"
  else
    fail "Browser: title" "AgentBoard not found in page text"
  fi

  if echo "$PAGE_TEXT" | grep -q "Welcome"; then
    pass "Browser: Welcome content visible"
  else
    fail "Browser: welcome" "Welcome not found in page text"
  fi

  # Take a screenshot for evidence
  $B screenshot /tmp/agentboard-test-screenshot.png 2>/dev/null && pass "Browser: screenshot captured" || true
else
  echo "  (skipped — gstack browse not installed)"
fi

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
