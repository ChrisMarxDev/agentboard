# Scenario 02 — Sprint board

Paste this as a single user message into a fresh Claude Code session (cwd = `test_claude/`).

---

Build me a sprint dashboard at `/sprint`.

I want:
- a kanban grouped by status (`todo`, `in_progress`, `done`) with 6 made-up tasks spread across the columns — each with an id, title, assignee, and priority
- a table below it showing the same tasks as rows, sorted by priority
- a small progress component showing what % of tasks are `done`

Use a collection (array of objects with `id`) so I can update individual tasks later. After it's rendered, move one `in_progress` task to `done` and tell me what changed.

---

### What this exercises

- Collections (arrays with `id` fields) via `agentboard_set`
- Upsert-by-id / merge-by-id semantics for the status change
- `<Kanban>`, `<Table>`, `<Progress>` components
- Multi-page site (`index.md` + `pages/sprint.md`)
- Derived progress computed in MDX (`{data.sprint.tasks.filter(t => t.status === 'done').length}`)
