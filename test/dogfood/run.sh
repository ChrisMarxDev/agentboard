#!/usr/bin/env bash
# Dogfood self-test.
#
# Boots an ephemeral AgentBoard instance, redeems the bootstrap invite to
# get a real token, then hands a fresh Claude session the one-sentence
# bootstrap prompt from spec §1.5. Claude is run with --bare so the
# project's CLAUDE.md and skills don't leak in — the test measures the
# workspace's ability to teach the agent, not the harness's ability to
# load context.
#
# Outputs everything into test/dogfood/results/<timestamp>/ for review.
# The pass/fail verdict is a small set of heuristic checks; the real
# value of this test is the captured transcript.
#
# Usage:
#   test/dogfood/run.sh                  # default
#   AGENTBOARD_BIN=/path ./run.sh        # override binary
#   PORT=3199 ./run.sh                   # override port
#
# Exit codes:
#   0 — verdict PASSED
#   1 — verdict FAILED
#   2 — harness error (server didn't start, claude not installed, etc.)

set -euo pipefail

# ----- Configuration -----
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
AGENTBOARD_BIN="${AGENTBOARD_BIN:-$REPO_ROOT/agentboard}"
PORT="${PORT:-3199}"
HOSTPORT="localhost:$PORT"
BASE_URL="http://$HOSTPORT"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
RESULTS_DIR="$REPO_ROOT/test/dogfood/results/$TIMESTAMP"
DATA_DIR="$(mktemp -d -t ab-dogfood-XXXXXX)"
TESTER_HOME="$(mktemp -d -t ab-tester-home-XXXXXX)"

mkdir -p "$RESULTS_DIR"

# ----- Pre-flight -----
if [[ ! -x "$AGENTBOARD_BIN" ]]; then
  echo "harness: $AGENTBOARD_BIN missing or not executable" >&2
  echo "         build it first: task build" >&2
  exit 2
fi
if ! command -v claude >/dev/null 2>&1; then
  echo "harness: claude CLI not on PATH" >&2
  exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "harness: jq required for verdict parsing" >&2
  exit 2
fi

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  # Keep DATA_DIR around in results so failures are inspectable. Rename
  # it under the results dir so it shows up alongside the logs.
  if [[ -d "$DATA_DIR" ]]; then
    mv "$DATA_DIR" "$RESULTS_DIR/server-data" 2>/dev/null || true
  fi
  rm -rf "$TESTER_HOME"
}
trap cleanup EXIT

# ----- 1. Spin up an ephemeral AgentBoard instance -----
echo "▸ Booting AgentBoard on $HOSTPORT (data dir: $DATA_DIR)" >&2
"$AGENTBOARD_BIN" --path "$DATA_DIR" --port "$PORT" --no-open \
  > "$RESULTS_DIR/server.log" 2>&1 &
SERVER_PID=$!

# Wait for the server to answer health checks.
for i in $(seq 1 30); do
  if curl -sS --max-time 1 -o /dev/null "$BASE_URL/api/health" 2>/dev/null; then
    break
  fi
  sleep 0.5
  if [[ $i -eq 30 ]]; then
    echo "harness: server didn't become healthy in 15s" >&2
    cp "$RESULTS_DIR/server.log" /tmp/ab-dogfood-server.log.failed 2>/dev/null || true
    exit 2
  fi
done

# ----- 2. Redeem the first-admin invite to mint a token -----
INVITE_FILE="$DATA_DIR/.agentboard/first-admin-invite.url"
if [[ ! -f "$INVITE_FILE" ]]; then
  echo "harness: no first-admin-invite.url at $INVITE_FILE" >&2
  exit 2
fi
INVITE_ID=$(grep -oE 'inv_[A-Za-z0-9_-]+' "$INVITE_FILE" | head -1)
if [[ -z "$INVITE_ID" ]]; then
  echo "harness: couldn't parse invite id from $INVITE_FILE" >&2
  exit 2
fi
echo "▸ Redeeming invite $INVITE_ID" >&2
REDEEM_RESPONSE=$(curl -sS -X POST -H 'Content-Type: application/json' \
  "$BASE_URL/api/invitations/$INVITE_ID/redeem" \
  -d '{"username":"dogfood-tester","password":"dogfood-test-pw-12345"}')
TOKEN=$(echo "$REDEEM_RESPONSE" | jq -r '.token // empty')
if [[ -z "$TOKEN" ]]; then
  echo "harness: redeem failed. response: $REDEEM_RESPONSE" >&2
  exit 2
fi
echo "$TOKEN" > "$RESULTS_DIR/token.txt"
chmod 600 "$RESULTS_DIR/token.txt"

# ----- 3. Build the bootstrap prompt -----
PROMPT_TEMPLATE="$REPO_ROOT/test/dogfood/prompt.txt"
PROMPT=$(sed \
  -e "s|__BASE_URL__|$BASE_URL|g" \
  -e "s|__HOSTPORT__|$HOSTPORT|g" \
  -e "s|__TOKEN__|$TOKEN|g" \
  "$PROMPT_TEMPLATE")
echo "$PROMPT" > "$RESULTS_DIR/prompt.txt"

# ----- 4. Run a clean Claude session against the bootstrap prompt -----
echo "▸ Invoking claude in a fresh /tmp cwd (no CLAUDE.md, no project memory)" >&2
TESTER_CWD=$(mktemp -d -t ab-tester-cwd-XXXXXX)
trap "cleanup; rm -rf $TESTER_CWD" EXIT

# Context isolation strategy: run from a fresh /tmp dir so no project
# CLAUDE.md or memory loads (memory is keyed by project path); the
# operator's ~/.claude has no global CLAUDE.md or skills to leak in
# (verified for this box). We don't use --bare because it disables
# keychain reads and the operator authenticates via keychain rather
# than ANTHROPIC_API_KEY.
#
# --allowedTools enumerates the tool surface the bootstrap needs and
# skips per-tool permission prompts in --print mode without invoking
# --dangerously-skip-permissions (which is blocked under root).
# --add-dir /tmp lets the agent clone into /tmp/$CLONE_DIR.
CLAUDE_RC=0
(
  cd "$TESTER_CWD"
  claude \
    --print \
    --add-dir /tmp \
    --output-format json \
    --allowedTools "Bash Read Write Edit Grep Glob" \
    -- \
    "$PROMPT" \
    > "$RESULTS_DIR/claude.json" \
    2> "$RESULTS_DIR/claude.stderr.log"
) || CLAUDE_RC=$?

echo "$CLAUDE_RC" > "$RESULTS_DIR/claude.exit"

# Extract the assistant's final response text (when claude exited cleanly).
if [[ $CLAUDE_RC -eq 0 ]] && [[ -s "$RESULTS_DIR/claude.json" ]]; then
  jq -r '.result // .content // empty' "$RESULTS_DIR/claude.json" \
    > "$RESULTS_DIR/claude.response.txt" 2>/dev/null || true
fi

# ----- 5. Capture the workspace state -----
WORKTREE="$DATA_DIR/.agentboard/worktrees/dogfood"
{
  echo "=== server worktree contents ==="
  ls -la "$WORKTREE" 2>&1 || true
  echo ""
  echo "=== git log on the bare repo ==="
  git --git-dir="$DATA_DIR/.agentboard/repos/dogfood.git" log --oneline --all 2>&1 || true
  echo ""
  echo "=== agent cwd (where it actually worked) ==="
  ls -la "$TESTER_CWD" 2>&1 || true
} > "$RESULTS_DIR/workspace-state.txt"

# Save the agent's cwd so failure forensics has it.
if [[ -d "$TESTER_CWD" ]]; then
  cp -a "$TESTER_CWD" "$RESULTS_DIR/agent-cwd" 2>/dev/null || true
fi

# ----- 6. Verdict -----
PASS=true
REASONS=()

if [[ $CLAUDE_RC -ne 0 ]]; then
  PASS=false
  REASONS+=("claude exited with code $CLAUDE_RC")
fi

RESPONSE=""
if [[ -f "$RESULTS_DIR/claude.response.txt" ]]; then
  RESPONSE=$(cat "$RESULTS_DIR/claude.response.txt")
fi

if [[ -z "$RESPONSE" ]]; then
  PASS=false
  REASONS+=("claude produced no response text")
fi

# Did the agent actually wire cwd to the workspace? After the
# init/remote/fetch/checkout dance, README.md should be sitting in
# cwd — that's the whole point of the cwd-is-the-workspace pattern.
if [[ ! -f "$RESULTS_DIR/agent-cwd/README.md" ]]; then
  PASS=false
  REASONS+=("agent did not materialize README.md into its cwd")
fi

# Did the agent's response acknowledge the bootstrap chain (README →
# something else)? Loose check: mentions README and at least one of
# skill / convention / workspace.
if [[ -n "$RESPONSE" ]]; then
  if ! grep -qiE 'readme' <<< "$RESPONSE"; then
    PASS=false
    REASONS+=("agent response did not reference README")
  fi
  if ! grep -qiE 'skill|convention|workspace|agentboard' <<< "$RESPONSE"; then
    PASS=false
    REASONS+=("agent response did not mention what the README pointed to")
  fi
fi

# Build the verdict file.
{
  echo "{"
  echo "  \"timestamp\": \"$TIMESTAMP\","
  echo "  \"base_url\": \"$BASE_URL\","
  echo "  \"claude_exit\": $CLAUDE_RC,"
  echo "  \"passed\": $PASS,"
  echo -n "  \"reasons\": "
  if [[ ${#REASONS[@]} -eq 0 ]]; then
    echo "[]"
  else
    printf '%s\n' "${REASONS[@]}" | jq -R . | jq -sc .
  fi
  echo "}"
} > "$RESULTS_DIR/verdict.json"

# ----- Output summary -----
echo "" >&2
echo "▸ Results: $RESULTS_DIR" >&2
echo "" >&2
if $PASS; then
  echo "✓ PASSED" >&2
  exit 0
else
  echo "✗ FAILED" >&2
  printf '  - %s\n' "${REASONS[@]}" >&2
  exit 1
fi
