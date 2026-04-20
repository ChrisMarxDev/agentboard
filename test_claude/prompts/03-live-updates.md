# Scenario 03 — Live updates

Paste this as a single user message into a fresh Claude Code session (cwd = `test_claude/`).

---

Add a deploy log to my dashboard — a page at `/deploys` that shows:
- the most recent deploy status (e.g. "deploying" / "success" / "failed") as a status badge
- a running log of the last 20 deploy events, newest at the top, each with a timestamp, commit sha, and environment

Make sure I have my browser open to the page, then simulate three deploys happening a few seconds apart: the first succeeds, the second fails, the third is still deploying. I want to see each one show up live without me refreshing.

---

### What this exercises

- `agentboard_append` to a log collection
- `agentboard_merge` on a status object
- SSE push — the page must update without a reload
- `<Status>` and `<Log>` components
- Ordering semantics (newest-first)
