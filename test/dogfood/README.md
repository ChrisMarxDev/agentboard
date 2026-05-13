# Dogfood self-test

The §15 test mechanized.

CORE_GUIDELINES §15 says:

> *Could a fresh Claude session, given only the bootstrap sentence and
> nothing else, do useful work in this workspace within five turns?*

This directory is how we answer that, automatically.

## What it does

`run.sh` orchestrates one full round of the dogfood loop:

1. Builds the `agentboard` binary if it isn't already.
2. Boots an **ephemeral** AgentBoard instance on a side port (3199 by
   default) against a fresh `mktemp` data directory. Nothing about the
   live dogfood instance on `:3000` is touched.
3. Reads the bootstrap-admin invite the new instance prints, redeems it
   to mint a real bearer token.
4. Builds the **bootstrap prompt** by substituting `__BASE_URL__`,
   `__HOSTPORT__`, `__TOKEN__`, `__CLONE_DIR__` into `prompt.txt`. The
   resulting prompt is the *one sentence* a human would paste — no
   extra context.
5. Invokes a **clean** Claude session against that prompt:
   - `--print` for non-interactive run-and-quit.
   - `--bare` to skip the project's `CLAUDE.md`, skills, hooks, and
     auto-memory. The bootstrap sentence is the only context the agent
     gets.
   - `--output-format json` for parseable output.
   - `--permission-mode bypassPermissions` to let Bash + Edit run
     without prompts in the sandbox.
   - `--add-dir /tmp` so the agent can clone into `/tmp/<clone-dir>`.
   - `HOME=` redirected to a temp dir so the agent doesn't see the
     operator's auth keychain or claude config.
6. Captures everything into `results/<timestamp>/`:
   - `prompt.txt` — the exact bootstrap sentence the agent was given.
   - `claude.json` — the raw `claude --print` output.
   - `claude.response.txt` — extracted final response.
   - `claude.exit` — the exit code.
   - `claude.stderr.log` — claude's stderr.
   - `server.log` — the ephemeral AgentBoard's log.
   - `server-data/` — the AgentBoard project root after the run
     (worktree + bare repo + sqlite). Inspect this when a run fails.
   - `agent-clone/` — what the agent left in `/tmp/<clone-dir>`.
   - `workspace-state.txt` — ls + git log summary.
   - `verdict.json` — pass/fail + reasons.
7. Exits 0 on pass, 1 on fail, 2 on harness error.

## Pass criteria

Heuristic; the real value is the captured transcript. The verdict
flags failure when:

- Claude exited non-zero.
- Claude produced no response.
- The agent didn't actually clone the workspace (`agent-clone/README.md`
  missing).
- The agent's response doesn't reference README.
- The agent's response doesn't mention what the README pointed at
  (skill / convention / workspace / agentboard).

If any of those trip, the run is FAIL and we update either the
workspace (the README isn't doing its job) or the harness (the
verdict criteria are wrong) — *never* the prompt.

## Running it

```bash
task dogfood:test
# or directly:
./test/dogfood/run.sh
```

Results land in `test/dogfood/results/<timestamp>/`. Multiple runs
accumulate; the `.gitignore` keeps them all out of git.

## Tuning it

- **Bigger budget.** Default uses claude's default model. For a more
  rigorous test, set `--effort high` or change the model on the
  command line inside `run.sh`.
- **Different prompt.** Edit `prompt.txt`. Keep it under three
  sentences — the test loses meaning when the prompt does the work
  the workspace should do.
- **Different verdict.** Edit `run.sh` step 6. Add semantic checks
  (did the agent open `tasks/`, did it propose a card, etc.) as the
  workspace grows.

## What this is *not*

- Not a unit test. Hits live infrastructure (port, file system, the
  claude CLI, the network for any external calls the agent makes).
- Not deterministic. The agent's exact phrasing varies turn to turn.
- Not free. Each run consumes a Claude API budget the operator pays
  for. Run it before shipping a substrate-affecting change, not on
  every commit.
