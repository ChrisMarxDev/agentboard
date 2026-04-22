# AgentBoard — Direction

This doc sets *who* AgentBoard is for, *what bet we're making*, and *what we're deliberately not committing to yet*. It sits alongside `CORE_GUIDELINES.md` (product invariants) and `spec.md` (design). When this doc and another conflict, this one wins on scope and positioning; the other wins on implementation.

> **A note on confidence.** Parts of this doc are settled (the ICP, the agent-ownership model). Parts are one plausible direction among several (the inbox, the git-backed content tree). The founder is using AgentBoard to figure out what AgentBoard should be; the shape beyond the next ~90 days emerges from that dogfood. Sections flagged **exploratory** are sketches we're trying on, not decisions. Revisit every quarter.

---

## 1. The bet (settled)

**The system of record for a team's work, where agents write on behalf of the humans accountable for that work.**

Three claims packed in:

1. **Agents are the writers.** Humans read, curate, and override. Agents are the default author, not a sidecar.
2. **Every agent belongs to a human.** No "the company's agent." Every write is attributable to a specific person's agent, and that person is accountable.
3. **One workspace, one tree, one truth.** Pages, data, files, tasks, components live together. Mixed teams read the same surface regardless of who (or whose agent) wrote what.

## 2. Who it's for — v1 wedge (settled)

**Small product teams, 3–30 people, at least one person technical enough to set up agents.**

Founders, PM-led squads, small agencies, ops teams at sub-100-person companies. Already gluing Notion + Linear + Slack + cron scripts together. Have LLM budget. Tolerate a rough edge.

This is the wedge, not the endgame. Non-technical end users come later, on the back of a hosted tier and a polished shell — not by selling into them today.

## 3. The collaboration loop (partly settled, partly exploratory)

**Settled:**
- Users are first-class. Identity, presence, avatars, @mentions exist. People need to be seen in the workspace.
- Agents are attached to users. Every agent credential is scoped to a person. Agent writes are tagged "by X's agent." Permissions inherit from the human; an agent can't do what its user can't.
- Tasks assign to people, not to agents. A task goes to Sarah; Sarah's agents (or Sarah herself) handle it.
- No PR / approval / review gate blocks writes. A single change does not break the company.

**Exploratory — the "inbox + reversed review" hypothesis:**

The loop we're trying on is *agent-writes-first, human-reviews-after*. Rather than a human writing and agents assisting, agents claim assigned tasks and complete them; humans then review outcomes through an inbox and accept, reject, or ask for changes. See `spec-inbox.md` for a sketch.

A working principle if this direction holds: **optimistic commit, post-hoc review.** Changes land immediately; the inbox is the audit, not the gate.

This is **one direction, not a decision.** Plausible alternatives still on the table: chat-first interfaces, real-time collaborative editing, a lightweight review gate after all. If the inbox hypothesis falls over in dogfood, we change course here without rewriting the rest of the doc.

**Deliberately unresolved:**
- How notifications land (through a user's agent? a traditional inbox? something else?)
- How @mentions route (probably agent-mediated, but unproven)
- Whether tasks get sub-tasks, priorities, dependencies — Jira has all of them; we may need none

## 4. What we are explicitly not (settled)

- **Not GitHub.** No PRs, no branches-as-concept, no merge reviews. If we end up with a review flow, it is post-hoc and optional, never a gate.
- **Not Notion + AI.** We invert the default author. Agents write structured data; humans read and curate. Notion assumes humans write and AI assists.
- **Not a Claude-specific tool.** REST and MCP APIs are open. Any LLM, any agent harness, any automation can participate.
- **Not a developer dashboard platform.** Analytical components are table stakes, not the product. The product is a team canvas.
- **Not built for non-technical end users yet.** Their turn comes after hosted + polish. Trying to serve them now means serving nobody well.

## 5. Storage and versioning (exploratory)

We're considering a split where the content tree (pages, components, files, skills) is managed by **git under the hood**, while operational state (users, auth, tasks, mentions, notifications, presence) lives in **SQL (SQLite)**, and the KV data store stays in SQL for speed with optional git snapshots for cold history.

**Why this might be right:** the inbox/review UX needs cheap diffs and cheap revert. Git gives both for free. Workspaces become portable (git clone). Plugins and workspaces speak the same versioning language.

**The hard constraint if we go this way:** git must stay invisible to users. No SHAs in UI, no branches surfaced, no "please resolve this merge conflict" prompts. That's a product discipline, enforced by a single-writer serializer and by translating every git error before it reaches UI.

**Still undecided:** whether we do this at all, what commit granularity looks like, whether binary-file bloat forces LFS, how we handle repo corruption.

This is one plausible direction, not a commitment. If we don't go here, we'd build diff/revert/history primitives ourselves — possible, just costly.

## 6. Tech positioning is two pitches (settled)

- **To end users (hosted SaaS, eventually):** *"One shared workspace where your team's agents keep everyone aligned."*
- **To collaborators, self-hosters, and us:** *"Single Go binary, pure-Go SQLite, no external runtime, runs anywhere."*

The second pitch is plumbing. It attracts contributors and makes hosting trivial. It is *not* the pitch we lead with for end users, and the product shell should not assume users who can run a CLI.

## 7. Roles (settled-minimal)

Minimal role model for v1. Expand only when a use case forces it.

- **Owner** — workspace admin, billing, role assignment.
- **Member** — regular user, owns their own agents, read/write.
- **Viewer** — read-only. No agents.
- **Guest** — scoped read/write on named pages or namespaces. For external collaborators. (May be v2.)

"Agent" is not a separate role. An agent is a credential owned by a Member or Owner; the human's permissions are the agent's permissions.

## 8. Scope consequences for the next ~90 days (intentionally loose)

Direction over deadlines. In rough priority:

1. **Multi-user auth and identity** — per-person login, agent credentials scoped to a user, write attribution visible in UI. *Settled we want this.*
2. **User-owned agents** — credential model above plus a "my agents" page where a Member manages their own. *Settled we want this.*
3. **Task assignment primitive** — assign-to-person model. Shape TBD; see `spec-inbox.md`. *Direction is to explore, not necessarily to ship.*
4. **Components as plugins** — the `plugin.yaml` format agreed earlier, with connector slots reserved in the manifest but not implemented. *Settled.*
5. **Storage-layer decision** — commit or reject the git-backed content direction. Needs a prototype, not just a spec. *Exploratory.*

Explicitly deferred:
- Connector supervision (user-supervised instructions-only is the v1 escape hatch).
- PR / review / approval flow as a blocking gate. May never ship.
- Hosted tier + non-technical UX polish.
- Marketplace / plugin hub UI.

## 9. How we decide when this doc is wrong

The founder is building AgentBoard by using it daily. When dogfood reveals that a stated *not* is actually needed, or a stated *yes* isn't paying off, we amend this doc — in the same commit as the change of course, never retroactively. A stale direction doc is worse than no direction doc. The **exploratory** flags are the places we most expect this to happen.
