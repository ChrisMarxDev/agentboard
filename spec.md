# AgentBoard — Design Contract

> **Status:** load-bearing. Read this before changing anything non-trivial.
> Companion to [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md). If reality drifts from
> this doc, the doc wins (or update the doc in the same PR).
>
> **Pivot, 2026-05-13.** AgentBoard's storage substrate is now git. The
> previous file-based custom store lives on at branch
> `filebase-cms-custom-substrate-13.5.26` for reference; `main` is the
> git-substrate rebuild. The thesis below restates the product from
> first principles in the new shape.

---

## 1. Thesis

AgentBoard is a **single self-hosted binary** that hosts the shared workspace
multiple AI agents collaborate inside, and serves a live web UI that humans use
to read what the agents are doing.

The substrate is **a git server**. Agents clone the workspace, work on
branches, push back. Conflicts surface as standard git merge markers; agents
resolve them and retry. The server doesn't impose a CRDT, a custom envelope,
or a bespoke concurrency model — git's `fast-forward` semantics already are
the concurrency model, and Claude-class agents are fluent in resolving merge
conflicts.

The web UI is **a viewer on the working tree**. The server keeps a
working-tree mirror of `HEAD` of the default branch on disk; the SPA reads
`.md` files out of that tree and renders them as MDX pages with embedded
components. When `HEAD` moves, the working tree updates and SSE notifies open
browsers. The frontend never knows there's a git repo underneath — it just
reads files.

This collapses three things that used to be separate stores in the v0.13
build (page tree, files-first store, activity log) into one tree of files
backed by one git history.

---

## 1.5. Bootstrap — one sentence, everything else discovered

The first (and ideally only) thing a human ever pastes into an agent to
start using a workspace is:

> AgentBoard is at `https://<host>/git/<workspace>.git`. Clone it
> (`git clone https://_:$TOKEN@<host>/git/<workspace>.git`), read
> `README.md` at the root, and follow what it says. AgentBoard tells
> you the rest.

That's the contract. The human writes that sentence once, never edits
it again. The agent connects, finds the README, follows the chain of
references. AgentBoard owns the long-form instructions; the human's
prompt template is one stable line.

This is **principle §15** (workspace teaches the agent) in concrete
form. It's why the git substrate is load-bearing: the agent already
knows how to clone and read files, so the bootstrap protocol is one
the agent doesn't have to be taught first.

**The README chain.** A workspace's `README.md` is the canonical
entry point. By convention it links to:

- `SKILL.md` (or `skills/agentboard/SKILL.md`) — the always-bundled
  AgentBoard skill that teaches the protocol: how to find the task
  queue, how to surface conflicts, how to subscribe to events, what
  the available components do.
- `CONVENTIONS.md` (or a section inside README) — this team's
  workspace-specific rules: naming, who reviews what, the kanban's
  column meanings, anything the next agent needs to know that isn't
  generic to AgentBoard.
- The task queue itself — typically `tasks/` (folder of `.md`
  cards), but the README names it so agents don't have to guess.
- Pointers to who else is around — `agentboard_workspaces` lists
  active workspaces; recent commits show who's been working in
  this one.

**Seeded by default.** `agentboard init` writes a starter `README.md`
and a starter `SKILL.md` so the chain works on day one for a fresh
workspace. The starter README is short — five or six links — and is
the place teams replace with their own when their workspace settles
into a shape.

**Git-less fallback.** Agents whose runtime can't shell out to
`git` get the same content via `agentboard_pull(workspace)` (see §6).
The bundle's first element is always the README. Same protocol,
different transport — the contract is "the workspace tells you what
to read first," not "the workspace requires a particular wire
format."

**Tests this design has to pass:**

- A fresh Claude session, given only the bootstrap sentence and a
  valid token, can identify the open task list and pick something to
  work on within five turns.
- Adding a new convention (e.g. "all PRs need a `:tested:` tag")
  requires editing a file in the workspace, not the bootstrap
  prompt.
- Deleting `CLAUDE.md` from the repo doesn't break onboarding for
  fresh agents.

If any of those fail, the fix is in the workspace's README or
SKILL, never in the bootstrap sentence.

---

## 2. The binary

One Go process. Listens on a port. Three things share that port:

1. **Smart-HTTPS git** at `/git/<workspace>.git` — `git clone`, `pull`,
   `push`, branches, tags. Uses [`go-git`][go-git] for a pure-Go
   implementation; no shell-out to a `git` binary.
2. **Read API** at `/api/<path>` — the existing SPA-facing shape, unchanged.
   Returns the rendered page envelope, folder listing, stream tail, or binary
   file. Backed by the working-tree mirror, not by the bare repo or any
   custom index.
3. **MCP** at `/mcp` — convenience layer for agents whose runtime can't shell
   out to `git`. The same git operations (clone-as-bundle, branch, propose,
   resolve-conflict) exposed as JSON-RPC tools. See §6.

Auth (bearer tokens, OAuth, browser sessions) sits in front of all three.
Same shape as v0.13 — the auth design carries over verbatim from
[`AUTH.md`](./AUTH.md).

[go-git]: https://github.com/go-git/go-git

---

## 3. Workspaces

A **workspace** is one bare git repo on disk. Multiple workspaces per binary
are fine; a small SQLite table tracks `{name, owner, default_branch, …}` —
that's operational state and per §13 lives in SQLite, not files.

The bare repo lives at `<datadir>/repos/<name>.git`. Adjacent to it lives
the **working-tree mirror** at `<datadir>/worktrees/<name>/`, which the
server keeps checked out to the workspace's default branch (typically
`main`). Receivers on the repo (post-receive hook) re-checkout the mirror
and emit `page-updated` events to the SSE broadcaster.

The bare repo is the source of truth. The working tree is a *materialized
view*. If the working tree is ever wrong (process crash mid-checkout,
manual file edit, anything), `git reset --hard <ref>` rebuilds it.

---

## 4. What lives in git, what lives in SQLite

### In git (content)

Everything the user composes:

- `.md` pages — frontmatter (YAML) + optional MDX body. Same shape as v0.13.
- Folders of `.md` — kanban boards, issue lists, anything collection-shaped.
- `.ndjson` streams — append-only logs. Activity feeds, telemetry.
- Binaries — images, PDFs, exports. Stored as ordinary blobs (LFS only if
  someone has multi-gig assets; defer).
- `.jsx` components — user-authored React bricks, dropped into
  `components/`. The watcher picks them up from the working tree on
  rebuild.
- `SKILL.md` skill bundles — folder layout, no special treatment.

All addressed at `/api/<path>` for reads, and via `git push` to update.

### In SQLite (operational state)

The carve-out from CORE_GUIDELINES §13 carries over without changes:

- Users, tokens, sessions, OAuth clients.
- Workspace registry (name, owner, default branch, public/private,
  per-path ACL rules).
- Webhook subscriptions.
- Rate-limit buckets.
- Inbox notifications (delivery state — the messages themselves are commits
  in git).

### Things that *go away*

The page manager, the files-first store, the v2 envelope, `_meta.version`
CAS, the collection / singleton / stream catalog, the FTS index over store
leaves, the page-lock table, the page-approval table, the content_history
shadow tree, the activity ndjson under `.agentboard/`. All of these are
**replaced by git itself**:

| v0.13 mechanism | Git equivalent |
|---|---|
| `_meta.version` CAS | non-fast-forward push rejection |
| `content_history/<path>.ndjson` | `git log -- <path>` |
| `.agentboard/activity.ndjson` | `git reflog` + `git log --all` |
| page locks | branches (`agent-claude-1/feature/X`) |
| page approvals | tags or merged PRs |
| collection rescan | `git ls-tree` |

---

## 5. Concurrency model

Two policies a workspace can pick from. Default is **push-to-main**; opt-in
to **always-PR** per-workspace via the registry row.

### Push-to-main (default)

1. Agent clones, branches, commits, attempts `git push origin HEAD:main`.
2. If the push is a fast-forward → accepted, working tree mirror updates,
   SSE fires, humans see the change.
3. If the push is not a fast-forward (someone else pushed first) →
   rejected with the standard `fetch first` error AND an MCP event
   `conflict.push_rejected{ref, ours, theirs, files?}` lands in the
   agent's inbox.
4. Agent pulls, merges (resolving any text conflicts in the standard
   `<<<<<<<` / `>>>>>>>` form), pushes again.

No special pre-merge round-trip. The agent uses native git; the server is
"just a git remote that emits notifications."

### Always-PR

1. Same as above, but pushes to `main` are forbidden outright.
2. Agents push to feature branches; the server records a *proposal* (a
   simple SQLite row pointing at the branch).
3. A proposal is merged by an explicit MCP call (`proposal_merge`) or a
   human clicking *Merge* in the UI.
4. Merging is fast-forward where possible; on conflict, the merge is
   rejected the same way push-to-main is rejected. The agent updates the
   branch and tries again.

Always-PR is the right default for shared production workspaces; the
push-to-main mode is right for "my personal scratch space" and tight loops
where review ceremony is overhead.

---

## 6. MCP surface

Smaller than v0.13. Most operations are now git itself, not our API; MCP
exists for agents whose runtime cannot shell out to `git` (see §7) and for
*notifications* that are inherently server-side.

```
agentboard_workspaces       → list workspaces visible to this token

agentboard_pull(ws, ref?)   → fetch + return the working tree as a bundle
                              (files keyed by path, with frontmatter and
                              body for .md leaves). For agents that read
                              but don't clone.

agentboard_propose(ws,
                   base,
                   branch,
                   files,
                   message)  → server-side branch+commit+push from the
                              given files. For agents that can't write
                              git locally. Returns the proposal id and
                              any conflicts.

agentboard_resolve_conflict(
  proposal,
  file,
  resolution)              → submit a resolved file body. Server replays
                              the merge with the resolution applied. For
                              git-less agents.

agentboard_subscribe(events)→ open an SSE-shaped MCP stream of
                              push, merge, conflict, mention events
                              for workspaces this token can read.

agentboard_grab(picks)      → unchanged from v0.13. Materializer that
                              assembles a list of leaves into agent-ready
                              text. Pure read, works against the working
                              tree.

agentboard_fire_event(...)  → unchanged from v0.13. Emit on webhook bus.
```

That's seven tools. Agents who use the git CLI directly need only
`agentboard_workspaces`, `agentboard_subscribe`, `agentboard_grab`,
`agentboard_fire_event` — the other three are the git-less fallback path.

---

## 7. Auth — bearer tokens carry through git

The git smart-HTTPS protocol uses HTTP Basic auth. Same `ab_*` / `oat_*`
tokens we already mint:

```
git clone https://_:ab_xxxxxxxx@board.example.com/git/<workspace>.git
```

Or via `git credential` helper (we ship a small `agentboard credential`
binary that responds to the helper protocol if the operator wants the
token out of the URL).

The MCP layer keeps its existing auth shape: bearer in `Authorization`
header. Same code path on the server — token resolves to a user, ACLs
apply.

Per-path ACLs survive: `workspaces.<id>.rules` in SQLite is a list of
`{action, pattern, methods}` rows checked on every fetch / push. Push
rejection for ACL reasons returns the standard `pre-receive hook
declined` git error.

---

## 8. Conflict resolution

When push is rejected (push-to-main or always-PR), the standard pattern is:

```
$ git push origin HEAD:main
! [rejected]        HEAD -> main (non-fast-forward)
hint: Updates were rejected because a pushed branch tip is behind its remote.

$ git pull --rebase origin main
CONFLICT (content): Merge conflict in tasks/ship-v2.md
Automatic merge failed; fix conflicts and then commit the result.
```

Agents fluent in this loop need no help from us. Agents that read MCP
events get a structured `conflict.push_rejected` with the offending files
attached; same outcome, different transport.

The standard `<<<<<<< / ======= / >>>>>>>` markers are the repair manual.
Claude resolves these well today on code; the same skill applies to MDX
frontmatter and body equally.

For the always-PR path, conflicts are surfaced at merge time. A failed
merge stores the conflict files in the proposal row; the agent fetches
them via `agentboard_resolve_conflict`, returns resolved versions, the
server commits and retries the merge.

---

## 9. The web UI

The SPA we shipped in v0.13 carries over with essentially no shape
changes. It still:

- Renders MDX pages with embedded components.
- Listens to SSE for live updates.
- Auto-attaches `<Kanban>` to the rendering page's folder (cards-as-pages).
- Reads `frontmatter` for the metadata panel (§14 enabled this — it stays).
- Honors the `wide: true` per-page width opt-in.

What changes:

- "Current state" is HEAD of the default branch by default. A branch
  picker can be added later for viewing other branches; the dogfood
  instance probably never needs it.
- "Activity feed" component reads `git log` instead of an NDJSON. Same
  shape on the wire.
- Per-page edit history (the meta bar's "edited 3h ago by alice") comes
  from `git log -- <path>` instead of `_meta`. Same UI.
- `_meta` is gone from frontmatter. The server-stamped fields move into
  git (commit metadata) and stop polluting the YAML.

Components built against the old `<Metric source="value" />` shape work
unchanged — `source=` still binds to the rendering page's frontmatter,
and folder collections still work via `source="path/"`. Per CORE_GUIDELINES
§14, content lives inside its file. Git makes that load-bearing instead
of just a slogan.

---

## 10. What ships first

Implementation order (Cuts), each landable independently:

**Cut 1 — Git endpoint.** `go-git` smart-HTTPS at `/git/<ws>.git`,
behind existing auth, single hard-coded test workspace. Goal: `git clone`
from cowork against `agentboard.hextorical.com` succeeds end-to-end.

**Cut 2 — Working-tree mirror.** Post-receive hook checks out the
default branch into `<datadir>/worktrees/<ws>/`. The watcher we already
have re-indexes the page tree from there. Goal: pushing an `.md` file
shows up in the SPA's left nav.

**Cut 3 — Read API on the working tree.** Repoint
`handlers_unified.go::handleUnifiedRead` to the working tree. Drop the
page manager's in-memory index; the watcher continues to feed the SSE
broadcaster. Goal: `/api/<path>` returns the right shape with zero
behavior change for readers.

**Cut 4 — Retire the v2 file store.** Delete
`internal/store/{singleton,collection,stream,catalog}.go` and the data
tier dispatch. Keep the page subset (which is now the only subset).
Goal: smaller binary, simpler audit surface.

**Cut 5 — MCP rewrite.** Replace the 10-tool batch CRUD with the seven
tools in §6. The git-less fallback path is the focus; agents with
`git` use the CLI.

**Cut 6 — Concurrency policy + always-PR mode.** Per-workspace toggle,
proposal table, the `proposal_merge` MCP call, the conflict-surfacing
plumbing.

**Cut 7 — Repointed dogfood.** The hextorical instance moves to the
new substrate. The preservation branch stays running on a different
port for a week to allow comparison.

**Cut 8 — Cleanup.** Drop the deprecated routes, drop the legacy
fields from `_meta`, scrub the docs.

---

## 11. Non-goals

To avoid scope drift:

- **We are not building GitHub.** No issue tracker, no PR review UI, no
  org permissions, no project boards as a separate concept. Issues are
  files in `issues/`; the kanban renders them; review happens by
  reading the diff or merging via MCP.
- **We are not building a CRDT.** Git's branch+merge model handles
  concurrency. Operational transformation belongs to live-co-editing
  products like Google Docs; agents don't need it.
- **We are not building a desktop app.** The web UI is the only UI.
  Agents use git or MCP; humans use the browser.
- **We are not building a real-time collab editor for prose.** If a
  human and an agent need to edit the same file at once, the second
  writer rebases; that's good enough for the agent-collaboration
  fantasy.

---

## 12. Compatibility with the v0.13 (filebase) branch

The preservation branch `filebase-cms-custom-substrate-13.5.26` keeps
the v0.13 implementation alive for reference. Nothing on `main` is
required to maintain backwards-compatibility with v0.13's API shape
(`/api/data/*` is already retired; `_meta` is about to go).

Operators who want to migrate a v0.13 instance to the new substrate:
`git init --bare`, `git add .`, `git commit`, push the bare repo into
the new binary's `<datadir>/repos/<name>.git`. The `.md` files come
across unchanged. The v0.13 `.agentboard/activity.ndjson` becomes the
first commit message; no point in replaying history we don't have to.

A `agentboard migrate-from-filebase <old-project>` CLI command lands
in Cut 7 to mechanize that flow.

---

## 13. Open product questions

Recording, not resolving. Future contributors: the answers go here.

**The name.** "AgentBoard" reads as a dashboard product, and the more
the project leans into "agents collaborate against a git substrate
they bootstrap themselves into," the less the word *board* describes
what it is. A name that emphasizes *shared workspace for AI agents*
would carry the principle §15 thesis better. Defer to a future
turn; flag any code change that makes the rename harder (deep
binary names, hardcoded paths, public URLs that would break) so the
cost of an eventual rename doesn't quietly compound.
