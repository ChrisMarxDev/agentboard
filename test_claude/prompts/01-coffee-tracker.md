# Scenario 01 — Coffee tracker

Paste this as a single user message into a fresh Claude Code session (cwd = `test_claude/`).

---

I want to track how much coffee I drink each day on a dashboard.

Start from zero cups today. Show me:
- a big number for today's count
- a line chart of the last 7 days (make up plausible starter data — between 1 and 5 cups/day)

Put it at `/` (the home page). Tell me the URL when it's ready, then add 1 cup to today so I can watch the number change live.

---

### What this exercises

- `agentboard_list_components`, `agentboard_get_data_schema` (discovery)
- `agentboard_set` (scalar + array)
- `agentboard_write_page` (MDX with `<Metric>` and `<TimeSeries>`)
- `agentboard_append` or `agentboard_merge` for the live update
- SSE propagation to the open browser tab
