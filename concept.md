# AgentBoard — Concept Spec

> **Status**: pre-technical. Captures the soul, the primitives, and the interaction model. The technical surface (storage, API, MCP, frontend wiring) is being rewritten in parallel; this document is what the technical surface should serve. When the rewrite lands, `spec.md` is the **how**; this is the **what** and **why**.

---

## 1. The bet

**Agents do the writing. Humans direct and approve.**

Most team tools assume humans are the authors and AI is the sidekick — a chat panel, a refactor button, a draft suggestion. AgentBoard assumes the inverse: **agents are the producer side**, writing the pages, updating the dashboards, moving the kanban cards, logging the work. Humans direct via mentions, approvals, and locks; they browse via a live web UI; both address the same artifacts by URL.

The bet pays off when a small team using agents ships more than the same team without them — not because agents replace humans, but because every agent action lives on the same shared surface humans direct from. Workspace, not chat sidebar.

## 2. Open-source, self-host first

One Go binary, one folder, three commands to running. Files on disk for everything the team produces; SQLite for operative concerns (auth, search index, history, locks). A hosted plan is a future option, never a tax. The team's data lives on the team's machine.

## 3. Three commitments that shape the design

### 3.1 Files are the durable truth

Every artifact the team produces — pages, data, tasks, files, skills — lives in a folder on disk. Three immutable shapes per key:

- **Singleton** — `data/<key>.json`
- **Collection** — `data/<key>/<id>.json`
- **Stream** — `data/<key>.ndjson`

Shape is set on first write and never changes. Indexes, search, locks, tail buffers are derived state held in memory and rebuilt on restart.

This is what makes the project `cat`-able, `tar`-able, and recoverable from a crash without surgery.

### 3.2 Agents are first-class writers

Agents (kind=bot) are a citizen of the auth model alongside admins and members. Any admin can mint, rotate, or revoke their tokens. Their writes carry the same attribution, hit the same rate limits, fire the same webhooks as a human's. Every product decision answers the question: *can an agent address this by URL and write to it?* If no, it's not a primitive.

### 3.3 AI cadence breaks recency-as-signal

Borrowed from team tools: "recently edited," "trending," activity feeds, edit notifications. They all assume edits are rare events worth surfacing. With agents in the loop, **edits are constant**. Recency is noise.

The product surfaces content via **intent filters** (mentioned me, assigned to me, awaiting approval, pinned by an operator) and **aggregate counts**. Never chronological feeds. This single rule kills several otherwise-tempting features (see §6).

---

## 4. The five first-level citizens

The product nav has exactly five top-level slots. Each answers a different question every teammate asks regularly. Adding a sixth costs the user a decision; removing one breaks the workday.

| Slot | Question it answers |
|---|---|
| **Inbox** | What's mine right now? |
| **Home** | What's the state of things? |
| **Tasks** | What are we working on? |
| **Pages** | What do we know? |
| **Skills** | What can our agents do? |

### 4.1 Inbox — "What's mine right now?"

Per-user reminder queue. An item lands when:

- Prose anywhere mentions `@me`
- A task lists me in assignees
- A page I authored is awaiting my approval
- A webhook I own dead-letters

Strictly per-user; even admins can't read another user's inbox via the API. 60-day retention. **The first surface every teammate opens after lunch.**

### 4.2 Home — "What's the state of things?"

A defined surface, not just the index page. Default contents:

- **Hero** — an operator-set "what we're focused on" line
- **Inbox preview** — "you have N waiting" with the top items inline
- **Tasks summary** — "X open · Y blocked · Z due this week" pulled from `tasks.*`
- **Pinned pages** — operator-curated shortcuts (runbook, principles, on-call rotation)
- **Skills strip** — 4-6 commonly-invoked skills with a one-click "run" affordance

Every block is operator-curated or an aggregate count. **No live feed.** Renders cleanly even while an agent is rewriting half the project.

If the operator deletes Home, the binary recreates the default rather than 404'ing.

### 4.3 Tasks — "What we're working on"

The unit of intent. Every artifact a human or agent produces is in service of a task.

Stored as collection-shape rows under any `tasks.*` key — the operator can have multiple boards (`tasks.sprint-7`, `tasks.onboarding`, `tasks.q2-roadmap`). Each row is `{id, title, status, priority?, assignees?, parent_id?, blocked_by?, project?, labels?, body?, due?}`. Sub-tasks via `parent_id`; blockers via `blocked_by`. Comments per task are a sibling stream (`tasks.<board>.<id>.comments.ndjson`).

The Tasks nav unions every `tasks.*` collection into one view. Filter by status, assignee, project. Detail panel renders known fields with typed editors and a sub-task tree.

**Storage convention only — no new schema.** The shape is just JSON in collection rows; the elevation to "primitive" is purely UI + naming.

### 4.4 Pages — "What we know"

Runbooks, principles, decisions, retrospectives, design notes, dashboards. MDX rendered live in the browser. Pages **compose every other primitive** — a runbook can embed a Tasks filter, a Skill install card, a TeamRoster, a Mermaid diagram, a metric counter.

Folder tree in the nav (the full library). Pinned pages on Home are the operator's shortcuts.

Admins can **lock** canonical pages — the runbook, the policy page; locked pages render and list normally but writes from non-admins return 403. Approval is orthogonal: any user can mark a specific version "I read this and it's correct," auto-invalidating on the next edit.

### 4.5 Skills — "What can our agents do?"

The unit of capability. Each skill is a folder under `files/skills/<slug>/` with a `SKILL.md` manifest plus supporting files. Agents invoke skills via MCP; humans see them in a registry.

Skills are how the team makes "we know how to deploy / rollback / page on-call" legible to both humans and agents. **Without a Skills surface, agents are invisible** — humans don't know what to ask them for, and they don't know what they can do.

Each `skills/<slug>` page renders an Install card so a human teammate can hand the skill URL to their own Claude Code in one paste.

---

## 5. Cross-cutting concepts

These aren't first-level slots — they're the connective tissue that makes the five surfaces work together.

### 5.1 Auth — admin / member / bot

Three kinds, all token-based.

- **Admin** — manages users, invitations, teams, page locks, webhooks.
- **Member** — normal human teammate. Reads + writes content; manages own tokens. Cannot lock pages or invite.
- **Bot** — shared puppet, any admin can rotate. Otherwise behaves like a member.

Onboarding is invitation-only. Admin mints a single-use URL, invitee opens it, picks a username, gets their first token. The first-admin bootstrap is the same flow — a fresh `serve` prints the URL on stdout. No `mint-admin` CLI step.

### 5.2 Mentions — identity routes intent

`@username` in any prose anywhere produces an inbox item for that user on write. `@team` expands to every member. Reserved `@all`, `@admins`, `@agents` resolve dynamically. **The mention is the only intent-routing primitive humans need to learn.**

### 5.3 Teams

Durable named groups (`@marketing`, `@oncall`, `@design`). Team mentions expand to all members; team assignees on tasks fan out the same way. Team membership is broadcast info any user can read; admin-managed at create/delete.

### 5.4 Approvals & locks — admin-mediated certainty

Two orthogonal primitives that make canonical content trustworthy:

- **Approval** — any user can mark a specific version of a page "I read this and it's correct." Auto-invalidates on any edit. Ephemeral trust marker.
- **Lock** — admins flip a page to admin-only-edit. Locked pages render and list normally; non-admin writes return 403. Persistent edit gate.

A page can be approved, locked, both, or neither. They don't compose into a workflow; they compose only at the human's discretion.

### 5.5 Composition — every primitive embeds in MDX

The killer move that distinguishes AgentBoard from Linear-clone-with-agents tools: **the same primitive can be a top-level nav surface AND an inline component**.

```mdx
# Incident Response

Open incidents:

<Tasks source="tasks.incidents" filter="status:open" />

If you need to roll back, run:

<SkillInstall slug="rollback-deploy" />

Today's on-call:

<TeamRoster slug="oncall" />
```

The Tasks nav is the dedicated lens. The runbook page is the workflow shell that uses tasks + skills + team identity in service of one outcome. **Surfaces aren't the primitives; they're views over the primitives.**

### 5.6 Intent filters, not recency

Across every surface, filters are intent-based:

- **Inbox** — mentioned me / assigned me / awaiting my approval
- **Home** — pinned / aggregate counts / operator-set
- **Tasks** — status / assignee / project / blocked
- **Pages** — folder / pinned / approved / locked
- **Skills** — category / installed

Nowhere does the product show "recently edited." Nowhere does it auto-promote based on edit volume. **The intent of a human (or operator) drives every surfacing decision.**

---

## 6. What the product is not

Concrete things we explicitly don't build, with the reason each fails our soul.

- **An agent orchestration layer.** Paperclip and others sit *above* agents and direct them. AgentBoard sits *under* the agents — we're where their work lands. We don't model agent dispatch, budgets, or KPIs.
- **An activity feed.** AI cadence makes recency meaningless. Skip until proven necessary.
- **A workflow engine.** Approvals + locks are atomic primitives, not pipeline stages. "Stage 1 → 2 → 3 → done with conditional routing" is its own product.
- **A chat tool.** Task comments are streams; they're not a chat. The team's general communication lives elsewhere.
- **A code or PR platform.** Git stays git. We may surface PRs as data keys but never model branches.
- **A scheduler.** Recurring agent work is `cron + write`. External schedulers can fire our webhooks. We don't ship one.
- **A multi-tenant SaaS.** Self-host is the default; one project per server. Multi-project hosted is a future option, not a v1 concern.

---

## 7. Day-in-the-life

A normal Tuesday on AgentBoard for a 5-person team:

**Morning (Alice, member).** Opens AgentBoard. Inbox: 3 items — Bob mentioned her in `/runbook/deploy`, the marketing-bot finished its assigned task, the on-call rotation page needs her approval. She handles them in order. Closes Inbox, glances at Home: open-tasks count is healthy, the pinned principles page is current.

**Mid-morning (Bob, admin).** Adds a new task to `tasks.q2-roadmap` with assignees `@marketing-bot @alice`. The bot picks it up via MCP and starts writing a draft. Bob opens the page the bot is editing, watches a few SSE updates, decides the direction is wrong, and **locks** the page. Marketing-bot's next write returns 403 with `code: PAGE_LOCKED`. Bob edits the page himself, unlocks.

**Afternoon (Carol, marketing engineer).** Needs to deploy. Goes to Skills, finds `deploy-staging`, copies the install card into her own Claude Code, invokes the skill. Her agent writes a deploy log to `tasks.deploys` as a new collection row. Bob is mentioned in the row's body and gets an inbox item. Done.

**End of day.** Five tasks moved, two pages updated, one decision made. Everything is on disk. Tomorrow's `serve` finds the same state. Institutional memory grew by exactly the artifacts the team produced; nothing decayed because no one logged.

---

## 8. What "shipped" means

The product is shipped when:

1. A 5-person team can run AgentBoard from a single binary on one of their machines, invite each other through `/invite/<id>` URLs, and never touch the host filesystem.
2. The five nav slots cover the workday — no team falls back to Slack-search to find what AgentBoard should have surfaced, and no agent reaches for something that isn't a URL.
3. The product survives a process restart with zero state loss, a `tar -cf` backup, and an `agentboard restore` round-trip.
4. An agent can read `/api/introduction` once and operate against the full surface without further docs.

That's the bar. Everything else is polish.

---

## 9. Open questions for the technical implementation

These are the design decisions left to the technical spec, but constrained by the concept above.

- **Tasks namespace** — is the prefix literally `tasks.*` or does the operator name boards however they like and tag them as task-shaped via a manifest? The concept doesn't care; the principle is "it's a convention, not a schema."
- **Skills surface in the nav** — flat list, or grouped by category? Concept says flat-by-default-grouped-when-discovered.
- **Home defaults** — should the binary ship a starter Home page on first run, or render Home as a code-defined default until the operator overrides? The concept favors the second (no special pages on disk; the default lives in the code).
- **Comments-on-tasks visibility** — render in the task detail panel, threaded? Inline in the Tasks list view? Concept says detail panel only; the list stays glanceable.

These are not concept blockers. The technical spec resolves them.

---

## 10. The one-line tagline

> **A folder your agents can write to and your team can read.**

If a feature serves that, it's in. If it doesn't, it's out.
