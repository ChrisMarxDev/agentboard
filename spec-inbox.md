# spec-inbox — Task inbox + reversed review flow

> **Status: exploratory.** This is one shape the "how humans and agents share work" primitive could take. It is not a committed design. Treat this doc as a sketch for debate, not a roadmap item. The founder is using AgentBoard to figure out what AgentBoard should be; this file captures one plausible answer to that question, to be kept, reshaped, or discarded as dogfood tells us more.

## The idea in one paragraph

Humans review outcomes, not inputs. A task is assigned to a person; their agent claims it and does the work before the person sees it; the person later reviews what the agent did and accepts, rejects (revert), or asks for changes. This inverts the Notion/Linear/Monday + AI pattern ("human writes → AI helps → human reviews own work"). The inbox is where a person sees their agents' work pending review.

## Why this shape might fit

- Agent-native by default. Tasks don't sit in "to do" waiting for a human to prompt an AI.
- No PR or branching flow required. The agent writes directly; review is post-hoc.
- Reuses primitives we already have — tasks as data records, SSE push, MCP tools.
- Cheap to make real if the content tree is git-backed (diffs and revert come for free).

## Why we might not do it

- Assumes every user has an agent. UX has to gracefully accept "I'll just do it myself" without feeling like you fell off the golden path.
- Puts weight on "outcome summaries" written by the agent. Bad summaries make review painful.
- Privileges review-as-default behavior. Many teams won't actually review their agents' output; the inbox becomes notifications-they-ignore.
- Could collapse back toward a PR flow in practice ("nothing ships until reviewed") even if the architecture is optimistic. Worth watching.

## Task data model (sketch, not schema)

```json
{
  "id": "task_01HX...",
  "title": "Update Q2 launch page with revised numbers",
  "description": "Finance sent new Q2 figures; reconcile with the current launch page.",
  "assignee_id": "user_sarah",
  "created_by": "user_chris",
  "status": "awaiting_review",
  "created_at": "2026-04-22T10:00:00Z",
  "due_at": "2026-04-23T17:00:00Z",
  "context_refs": [
    { "kind": "page", "path": "content/q2-launch.md" },
    { "kind": "data", "key": "finance.q2.revised" }
  ],
  "agent_summary": "Updated the four revenue figures in the Overview section. Flagged one inconsistency: marketing headcount in the budget table doesn't match finance.headcount.current.",
  "agent_changes_ref": "a3f8c91"
}
```

### Status lifecycle (sketch)

```
open → claimed → awaiting_review → accepted
                                 → rejected   (revert agent changes)
                                 → reassigned (back to open, with comment)
```

Transitions are writes by whoever makes them. Agent does `open → claimed → awaiting_review`. Assignee human does the final transition.

## Inbox panes (sketch)

| Pane | Shows | Who acts |
|---|---|---|
| **To review** | Tasks where your agent finished, awaiting your review | You |
| **Assigned to you** | Tasks in `open` or `claimed` where you're the assignee | Your agent (or you) |
| **Waiting on others** | Tasks you created, assigned to someone else | Nothing — just visibility |
| **Mentions** | Places you've been @mentioned | You or your agent |

The review pane is the load-bearing one. Each item shows the task + original description, the agent's outcome summary, a diff panel of what changed, and four actions:

- **Accept** — close the task
- **Reject & revert** — undo the agent's changes, close the task as rejected
- **Needs changes** — reassign back to the same user with a comment (agent takes another pass)
- **I'll handle it** — reassign to yourself as a human, revert the agent's changes

## Agent pickup mechanism (sketch)

Two channels, either can fire first:

1. **Pull** (reliable): agent periodically calls MCP tool `agentboard_my_tasks`, which returns tasks where `assignee_id` matches the agent's owner and status is `open`. Agent picks the top one, PATCHes status to `claimed`.
2. **Push** (responsive): SSE stream broadcasts `task.assigned` events scoped to the assignee. Long-running agent sessions subscribe.

A task can also be explicitly self-claimed by a human ("I'll do this one"); their agent never sees it.

## How a git-backed content tree would plug in

Only relevant if we go the git-for-content direction (see `DIRECTION.md` §5). If we do:

- The agent's changes are one or more commits with a known SHA, stored on the task as `agent_changes_ref`.
- **Reject & revert** = `git revert <sha>`.
- The review-panel diff = `git diff <sha>~..<sha>`.
- History is free — "what did the agent do last month" is `git log`.

If we don't go that direction, we'd need to build diff/revert/history primitives on top of the data store. Doable, just costly. This is one of the two main reasons the git question matters.

## Open questions — all genuinely open

1. **How are tasks created?** Human typing in a UI? Another agent? A connector? All three?
2. **Can tasks have sub-tasks?** If yes, does one agent handle the parent or spawn sub-agents for children?
3. **UX for "needs changes"?** Free-text comment? Inline diff comments? A conversation thread?
4. **How do we keep the review pane from becoming ignored email?** Noise management is the single biggest UX risk; unsolved.
5. **Escalation when agents can't finish** — stuck, missing context, hit a permission wall. Does the task go to a `blocked` state? Who notices?
6. **Multiple agents per user — who claims what?** User declares "research agent handles research tasks"? System infers? Neither?
7. **Are tasks versioned (git) or mutable (SQL)?** Probably mutable with state transitions logged. Not certain.
8. **Do we need labels / priorities / dependencies?** Jira has them. We may need none. Let dogfood decide.
9. **Deadlines and reminders?** Who reminds whom — the agent, the system, a teammate?
10. **Private tasks?** Workspace-wide visibility by default, but the bar for "actually private" is unresolved.

## What this spec is not

- Not an implementation plan. Nothing here says "build in week 1."
- Not a schema commitment. The data model above is illustrative.
- Not a claim this is the direction. It is one coherent sketch. Other shapes — chat-first, real-time collaborative editing, a PR-style gate after all — remain on the table.

## Related

- `DIRECTION.md` — v1 positioning and the reversed-review idea at a principle level
- `spec.md` — existing design (pages, data, components)
- `spec-sessions.md` — existing notes on how agent sessions attribute writes
