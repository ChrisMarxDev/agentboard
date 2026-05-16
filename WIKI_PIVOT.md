# Pivot — Wiki Reduction

> **Status:** load-bearing for the next milestone. Written 2026-05-16.
> Supersedes the relevant sections of [`ROADMAP.md`](./ROADMAP.md) and
> sharpens [`spec.md`](./spec.md) §1. Companion to
> [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md). When this doc and the
> rest of the repo disagree, this doc wins for the scope below.

## 1. Why

AgentBoard's product is **"modern Confluence for non-technical teams,
with AI agents living inside the workspace."** The customer is a 5–50
person team — marketing, ops, product — whose current wiki is a stale
Confluence space, and who wants a private collaborative website where
their humans and their AI agents co-author pages.

Everything in this pivot serves that shape. The current codebase has
features that pre-date this framing (OAuth dance, outbound webhooks,
event-bus MCP tools, JSON-kanban renderer, workspace plugin contract)
and adds surface area without serving the wiki use case. They go.
The things the use case *does* need (per-path edit permissions,
groups, a non-terrible editor for `.md`, wiki chrome) get added.

This is a **net reduction in complexity**, not a sideways rewrite.
The deletions are larger than the additions.

## 2. North-star, in one paragraph

A team admin signs up, gets a workspace at
`<team>.agentboard.dev` (or self-hosted), and emails invite links to
their colleagues. Each colleague picks a username + password, lands on
the workspace, sees a top bar (workspace name, search, activity, their
avatar) and a side bar (page tree). The center pane shows whatever
file the URL points at — HTML pages render as-is, Markdown renders
through goldmark, directories render as a page index. Agents are
invited the same way humans are; they push commits via git smart-HTTP
using their bearer token, and the dashboard updates live via SSE.
Permissions are per-path write rules: Hanna's group can write under
`marketing/**`, the engineering group writes under `engineering/**`,
admins write everywhere. Read is unrestricted within a workspace.

## 3. What this pivot does NOT change

These are correct and stay untouched:

- Git as the substrate (`internal/gitserver/`).
- HTML/Markdown server-side rendering (`internal/html/server.go`).
- Bearer tokens + browser sessions for auth
  (`internal/auth/tokens.go`, `sessions.go`, `passwords.go`,
  `middleware.go`, `store.go`, `rules.go`).
- Magic-link invitations (`internal/invitations/`).
- SSE live-reload on push (`internal/server/sse.go` +
  `internal/gitserver/events.go`).
- FTS5 search over the worktree (`internal/search/`).
- Smart-HTTP git endpoint and the bearer-token gating on it.
- The `/me` and `/_admin` UIs (general shape; permission management
  gets added inside `/_admin`).

If a code change in this pivot would degrade any of the above, stop
and flag it.

---

## 4. Cuts

Execute the cuts in §4 **before** the adds in §5. Cuts are safer
(deletions), they shrink the surface the adds touch, and they leave
the tree in a buildable state at every step. Run `task build && task
test` after each cut.

### 4.1 OAuth (provider + protected-resource discovery)

**Why:** No customer has asked for Google/Microsoft SSO. Password +
magic-link invites cover the v1 audience. OAuth adds ~6 files,
schema migrations, route surface, and a discovery dance that's
load-bearing for nothing today. When a customer asks, re-add as a
single sign-in provider integration, not a full OAuth server.

**Delete:**

- `internal/auth/oauth.go`
- `internal/auth/oauth_schema.go`
- `internal/auth/oauth_store.go`
- `internal/server/handlers_oauth.go`
- `internal/server/handlers_oauth_test.go`
- Routes registered in `internal/server/server.go` around lines
  188–193:
  - `GET /.well-known/oauth-protected-resource`
  - `GET /.well-known/oauth-authorization-server`
  - `POST /oauth/register`
  - `GET /oauth/authorize`
  - `POST /oauth/authorize/decide`
  - `POST /oauth/token`
- Any OAuth-table migrations triggered from `internal/auth/migrate.go`
  (keep the file, drop the OAuth-specific migrations; preserve session
  + token + password migrations untouched).
- OAuth references in `internal/auth/store.go` if any (likely a method
  or two that resolves OAuth client → user).

**Don't touch:** `internal/auth/tokens.go`, `sessions.go`,
`passwords.go`, `rules.go`, `middleware.go`. Those are the
human/agent auth path and stay.

**Acceptance:** `grep -ri oauth internal/` returns zero hits outside
of `migrate.go` (where an inert migration record may remain). `task
test` passes. `curl /.well-known/oauth-protected-resource` returns
404, not a JSON payload.

### 4.2 Outbound webhooks

**Why:** "Ping Slack on push" is a v2 feature. No paying customer is
asking for it. The package is self-contained, so removal is clean.

**Delete:**

- `internal/webhooks/` (the whole directory)
- `internal/server/handlers_webhooks.go`
- Webhook route(s) in `internal/server/server.go` (search for
  `handleWebhook` or `/_api/webhooks`)
- Wiring in `cmd/agentboard/` or `internal/cli/serve.go` that
  constructs the dispatcher and passes it to the server. The
  dispatcher should no longer be instantiated anywhere.

**Don't touch:** `internal/gitserver/events.go` — the in-process
event hub stays; it's the SSE source. Only the *outbound HTTP delivery*
side is going.

**Acceptance:** `grep -ri webhook internal/ cmd/` returns zero hits.
The build still has working SSE live-reload (open
`http://localhost:3000`, push a commit from another terminal, the
page updates).

### 4.3 Event-bus MCP tools (`agentboard_subscribe`, `agentboard_fire_event`)

**Why:** The remaining four tools (`agentboard_workspaces`, `_pull`,
`_propose`, `_resolve_conflict`) cover the core agent loop — discover
the workspace, fetch, propose changes, resolve conflicts. Subscribe +
fire_event are an event-bus surface oriented at agent-to-agent
coordination, which is over-built for a wiki where humans and agents
collaborate on shared pages. If an agent wants to know when something
changed, it polls or re-pulls. Cheap.

**Delete:**

- The two tool definitions in `internal/mcp/tools.go` (lines 92 and
  107 in the version this doc was written against — find by name).
- Their handlers in `internal/mcp/handlers.go`.
- Any tests that target only those two tools.

**Don't touch:** The four kept tools and their handlers.

**Acceptance:** `tools/list` over MCP returns exactly four tools,
not six. The dogfood test (`task test:dogfood`) passes — confirm
the bootstrap-from-cold-start flow doesn't depend on either deleted
tool. If it does, fix the bootstrap, not the deletion.

### 4.4 Taskboard / kanban-from-JSON renderer

**Why:** Cute, but one more convention to teach agents and one more
type-detection branch in the renderer. Agents can write a kanban
in HTML directly — same visual outcome, fewer rules. The
"look, structured data!" moment in the demo workspace gets reshot
with an HTML kanban.

**Delete:**

- `internal/html/taskboard.go`
- `internal/html/taskboard_test.go`
- The taskboard branch in `internal/html/server.go`'s renderFile
  dispatch (search for `taskboard` or the JSON-shape probe that
  hands off to it). The remaining JSON path should render as
  syntax-highlighted source.
- Any taskboard references in the demo workspace (the `/agency/`
  workspace per `CLAUDE.md`). Replace with a hand-written HTML
  kanban page, or just remove and let an agent regenerate later.

**Don't touch:** Markdown rendering, HTML rendering, directory
listing, or syntax highlighting.

**Acceptance:** No file in `internal/html/` references "taskboard".
A `.json` file in the workspace renders as syntax-highlighted JSON,
not as a kanban. The demo workspace still has a kanban-shaped page;
it's an `.html` file now.

### 4.5 Workspace plugin contract (doc only)

**Why:** `spec-plugins.md` describes a workspace-extension surface
that non-technical customers will never use. Leaving it referenced
in `CLAUDE.md` and `spec.md` invites future agents to build for it.

**Action:**

- Delete `spec-plugins.md`. (No code currently implements this contract
  per the new CLAUDE.md; if a reference slipped into the code, remove
  it.)
- Remove the bullet in `CLAUDE.md` that links to `spec-plugins.md`.
- Remove any cross-reference in `spec.md`.

**Acceptance:** `grep -r 'spec-plugins' .` (excluding `docs/archive/`)
returns zero hits.

### 4.6 Restore handler — decision: KEEP

**Decision (2026-04-30):** Keep `internal/server/handlers_restore.go`.
It powers the "(restore)" button on the history view: given a
`path` + `sha`, it re-commits that historical file content as a new
commit on the default branch, attributed to the cookie-auth'd user.
Cookie + CSRF gated, same as `/_api/edit`. This is wiki gold —
"revert this page to last Friday" is a feature non-technical teams
expect.

The file's existing 5-line header comment is already clear; no code
change needed for §4.6. When §5.2 lands, restore must go through the
same permission check as `/_api/edit` (the actor needs write access
on the path being restored) — track that in §5.2's enforcement
point 2.

**Acceptance:** None for this step beyond recording the decision.

---

## 5. Adds

Execute adds in order. Each add lands buildable, tested, and
deployable on its own. The implementation agent should commit and
push between adds, not after.

### 5.1 Groups

**Goal:** Named groups of users that the permission rules in §5.2
can reference. Without this, every rule has to enumerate users.

**Schema (SQLite, additive migration in `internal/auth/migrate.go`):**

```sql
CREATE TABLE groups (
  id TEXT PRIMARY KEY,            -- ulid
  name TEXT NOT NULL UNIQUE,      -- "marketing", "engineering"
  created_at INTEGER NOT NULL,
  created_by TEXT NOT NULL        -- user id
);

CREATE TABLE group_members (
  group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  added_at INTEGER NOT NULL,
  PRIMARY KEY (group_id, user_id)
);
```

**Package:** `internal/groups/` with the standard shape
(`groups.go` defining the model + store, `groups_test.go`).
Functions: `Create`, `Delete`, `Rename`, `AddMember`, `RemoveMember`,
`Members(groupID)`, `MemberOf(userID)`, `List()`, `Get(name)`.

**HTTP:** Admin-only routes under `/_api/admin/groups`:
- `GET /_api/admin/groups` → list
- `POST /_api/admin/groups` → create `{name}`
- `DELETE /_api/admin/groups/{name}`
- `POST /_api/admin/groups/{name}/members` → add `{username}`
- `DELETE /_api/admin/groups/{name}/members/{username}`

**Admin UI:** Add a "Groups" panel to `handlers_adminui.go`. List
groups, click in, manage members. Same visual style as the existing
users panel.

**CLI:** Add `agentboard admin groups list|create|delete|add|remove`
to `internal/cli/admin.go` for filesystem-access recovery.

**Acceptance:**
- Admin can create a group, add 3 users, remove 1, delete the group.
  Via UI, via API, via CLI.
- `task test` covers create / add / remove / cascade-on-user-delete.
- A non-admin gets 403 on every group endpoint.

### 5.2 Per-path write permissions

**Goal:** A version-controlled rules file at the workspace root.
Pushes that touch paths outside the pusher's allowed set are rejected
with a clear error.

**File format:** `.agentboard/permissions.yaml` (version-controlled
inside the workspace itself — this is intentional; changes to
permissions are auditable as commits).

```yaml
# .agentboard/permissions.yaml
#
# DEFAULT IS PERMISSIVE: with no file present, every workspace member
# can write everywhere. Rules below RESTRICT specific paths to specific
# groups. First matching rule wins. Paths with no matching rule remain
# open to all members.
version: 1
rules:
  - paths: ["finance/**", "legal/**"]
    write: [finance, admins]           # restricted: only finance + admins
  - paths: ["engineering/secrets/**"]
    write: [admins]                    # locked down: admins only
  # No `**` fallback rule needed — anything not matched stays open.
  # Writes to `.agentboard/permissions.yaml` itself are structurally
  # admin-only, enforced in code regardless of these rules.
```

**Semantics:**
- **Default is permissive.** If `.agentboard/permissions.yaml` is
  absent, or present but empty, every workspace member can write to
  every path. Rules narrow this default — they restrict, they don't
  enable. This matches wiki convention (Confluence, Notion): open
  by default, lock specific paths.
- Rules evaluated top to bottom; **first matching rule wins** per
  changed path. A rule whose `write` list grants the user's group
  → allowed. A rule whose `write` list excludes the user → denied.
- Paths with **no matching rule** stay open to all members.
- `paths` uses `doublestar` glob syntax (the Go library
  `github.com/bmatcuk/doublestar/v4` — add to `go.mod`).
- `write` is a list of group names. The special group `admins` is
  synthetic: it always contains every user with role `admin`.
- **Structural admin-only path**: writes that touch
  `.agentboard/permissions.yaml` always require the actor to have
  role `admin`, regardless of what the YAML says. Enforced in code,
  not in YAML. Otherwise a non-admin could commit a YAML granting
  themselves admin and then commit anything.
- Bot tokens inherit the permissions of the user they're attached to
  (matches existing token model in `internal/auth/tokens.go`).

**Parsing + evaluation:** New package `internal/permissions/`:
- `Rules` struct, `Parse(yaml []byte) (Rules, error)`,
  `Rules.Allow(actor Actor, changedPaths []string) error` returning
  the offending paths in the error. `Actor` carries the user's role
  (admin / member / bot) and the names of the groups they belong to;
  the synthetic `admins` group is added when role == admin.
- `LoadFromWorkspace(wt *gitserver.Worktree) (Rules, error)` reads
  `.agentboard/permissions.yaml` from the working tree. If absent or
  empty, return a zero-value `Rules{}` — which evaluates to
  "everything allowed", per the permissive default.
- The structural admin-only check on `.agentboard/permissions.yaml`
  paths lives in `Rules.Allow` and short-circuits before rule
  evaluation. It is *not* configurable from YAML.

**Enforcement point 1 — git push:** In `internal/gitserver/`, add a
pre-receive check. When the smart-HTTP endpoint receives a push:
1. Walk the diff against `HEAD` to enumerate changed paths.
2. Resolve the pushing user (+ role + groups) from the bearer token.
3. Load `Rules` from the *current* tree (pre-push) — simpler than
   reaching into pending git plumbing for the post-push tree. The
   structural admin-only check on `.agentboard/permissions.yaml`
   (see Semantics) blocks the "non-admin grants themselves writes"
   attack regardless of which tree we load from, so the current-tree
   read is safe.
4. Reject the push with a clean error message if any path is
   disallowed. The error reaches the agent as a normal git push
   failure with a server-side reason.

**Enforcement point 2 — `/_api/edit` and `/_api/restore`:** In
`handlers_edit.go` and `handlers_restore.go`, run the same check
against the proposed file path before committing. Restore (§4.6) is
just an edit with the body sourced from a historical sha, so it gets
the same gate.

**Enforcement point 3 — `agentboard_propose` MCP tool:** Same
check inside `internal/mcp/handlers.go`, before staging the commit.

**Admin UI:** "Permissions" panel under `/_admin` showing:
- Current rules (rendered from the YAML in the default branch)
- A linked-tree view of "who can write where"
- An editor for the YAML (textarea + validation on save → commits to
  workspace as the admin user)

**Acceptance:**
- With no `.agentboard/permissions.yaml`, any member can push, edit,
  or propose a change to any path. (Permissive default holds.)
- Add a rule restricting `engineering/secrets/** → admins`. A
  non-admin user pushes a change to `engineering/secrets/foo.md` →
  push rejected with a clear error naming the disallowed path. Same
  user, via `/_api/edit`, gets a 403. Via `agentboard_propose`,
  gets a structured error. Via `/_api/restore`, gets the same 403.
- A non-admin who tries to push a change touching
  `.agentboard/permissions.yaml` gets rejected, regardless of rules
  (structural admin-only).
- An admin edits `.agentboard/permissions.yaml`, commits, and the
  new rules take effect on the next push.
- `task test` covers: absent YAML → all members can write anywhere;
  rule order (first match wins); overlapping paths; restricted-path
  rejection; bot token inheriting its owner's groups; the structural
  self-edit-of-permissions block.
- Read remains unrestricted: any authenticated workspace member can
  read any file.

### 5.3 Markdown editor (textarea + live preview)

**Goal:** A non-technical user can click "Edit" on a Markdown page,
type, see a preview, save, and be back on the rendered page. Ships
fast; not a WYSIWYG. (TipTap-grade WYSIWYG is parked for after this
pivot — see §7.)

**Current state:** `internal/server/handlers_edit.go` exists; assume
it's a basic edit endpoint. Read it first to see what's already
there.

**Add:**
- A `?edit=1` query mode on Markdown page routes that swaps the
  rendered HTML for a two-pane layout: textarea on the left,
  live-rendered preview on the right (rendered server-side from the
  textarea content via a debounced `POST /_api/preview` call, or
  client-side via a tiny markdown library — pick the server-side
  route to keep frontend dependencies near zero).
- A "Save" button that does the existing edit-submit (which already
  goes through git as a commit attributed to the editing user).
- A "Cancel" button that returns to the rendered view.
- An "Edit" link in the page chrome on Markdown pages (top bar or
  near the title) — only visible if the viewer has write permission
  on this path.

**Don't add:** A rich-text WYSIWYG, syntax highlighting beyond
basic monospace, image-paste handling, slash-commands, autosave. All
v2.

**Acceptance:**
- Logged-in user with write access opens `marketing/brief.md`, clicks
  Edit, sees the source in a textarea, types, sees the preview
  update, saves, lands on the new rendered page, sees a commit with
  their name in the activity feed.
- Without write access on that path, the "Edit" link does not appear,
  and `?edit=1` returns 403.

### 5.4 Wiki-feel chrome polish

**Goal:** The dashboard feels like a wiki, not like a file browser.
Small additions, no architectural changes.

**Add:**
- **Breadcrumbs** in the top bar derived from the URL path. Each
  segment is a link to that directory. (Implement in
  `internal/html/server.go` rendering helper; pass into the template.)
- **Page header on Markdown + HTML pages**: title (h1 from the file,
  or filename fallback), "Last edited by `<user>` `<relative-time>`"
  derived from `git log -1 <path>`. The `gitserver` package already
  has commit-lookup helpers; surface them.
- **Activity feed** at `/_activity`: paginated `git log` across the
  default branch, rendered with author, message, timestamp, and
  links to changed files. Limit 50 commits per page.
- **Page tree side bar mode**: the existing directory listing should
  also surface as a collapsible tree in the side bar, not just as
  the center pane on directory URLs. Default expanded one level deep
  from the current page's location.

**Acceptance:**
- Navigating to `/marketing/campaigns/q3-launch.md` shows
  `Home / marketing / campaigns / q3-launch.md` in the top bar,
  each segment clickable.
- The page header shows "Last edited by Hanna 2 hours ago".
- `/_activity` shows the last 50 commits with author + message +
  changed-file links.
- The side bar tree shows the current page's neighbors and the
  current page is highlighted.

### 5.5 Permission management UI inside `/_admin`

**Goal:** A non-technical admin can manage groups and permissions
without editing YAML by hand or reading docs.

**Add to `handlers_adminui.go`:**
- A "Groups" page (built in §5.1, polished here).
- A "Permissions" page that:
  - Renders the current rules as a table: path → groups that can
    write → "who's in those groups" expander.
  - Has an "Add rule" form: path glob input, multi-select of groups.
  - Has "Edit YAML directly" as an escape hatch (textarea +
    validation + commit-on-save).
- A "Who can edit this page?" sidebar widget on every page (visible
  to admins) that shows the resolved rule for the current path and
  the groups + users it grants.

**Acceptance:**
- An admin can: create groups, add members via UI, write a
  restrictive rule via UI (`finance/** → finance, admins`), and
  verify the rule takes effect — a member of the `marketing` group
  fails to edit `finance/q3.md` with a clear error, and any member
  can still edit unrestricted paths.
- All of the above without ever opening a terminal or editing a YAML
  file by hand.
- The "Who can edit this page?" widget on a restricted page shows
  the matched rule's groups + members. On an unrestricted page it
  shows "all members" (the permissive default).

---

## 6. Things explicitly NOT in this pivot

The following came up during planning and are deliberately deferred:

- **Read-restrictions per path.** Substantially harder than write
  restrictions (filter sidebar, search, directory listings, every
  render path). The escape hatch is: confidential content goes in a
  separate workspace.
- **TipTap / Lexical WYSIWYG editor.** §5.3's textarea+preview is
  enough for v1. Real WYSIWYG is a 2–3 week post-pivot project.
- **Comments / discussions on pages.** Confluence has them; we don't
  for v1. Add when a customer asks.
- **`@username` mentions.** Was on the old roadmap; defer.
- **Saved searches.** Defer.
- **Branch picker UI.** Defer — workspaces are always-`HEAD` in v1.
- **Hosted multi-tenant on a shared origin.** When we host customers,
  we host them on separate VMs (one binary per customer). See
  `HOSTING.md` for the per-customer-VM deploy. Subdomain-per-tenant
  on a shared instance is a separate, larger project.
- **Read-access OAuth/SSO.** Re-add when a paying customer asks.
- **Outbound webhooks / event subscriptions.** Cut in §4.2 and §4.3;
  re-add as a focused feature when a real workflow needs them.

If the implementation agent finds itself touching any of the above
during this pivot, stop and ask.

---

## 7. Sequenced rollout

Land in this order. Each cut and each add is a buildable, testable
commit. Push between them.

1. **§4.1** Cut OAuth.
2. **§4.2** Cut outbound webhooks.
3. **§4.3** Cut the two event-bus MCP tools.
4. **§4.4** Cut the kanban renderer.
5. **§4.5** Delete `spec-plugins.md` and inbound references.
6. **§4.6** Decide on `handlers_restore.go`: keep + document, or cut.
7. **§5.1** Add groups (model, store, HTTP, admin UI, CLI).
8. **§5.2** Add per-path write permissions (parser, enforcement at
   three points, admin UI, tests).
9. **§5.3** Add Markdown edit mode (textarea + preview).
10. **§5.4** Add wiki chrome (breadcrumbs, page header, activity feed,
    page tree).
11. **§5.5** Polish admin UI for groups + permissions management.

After §11, refresh:
- `CLAUDE.md` — remove pointers to deleted files, add pointers to new
  packages (`internal/groups/`, `internal/permissions/`), update the
  MCP-tool count from 6 to 4.
- `spec.md` — update §6 (MCP surface) to four tools.
- `ROADMAP.md` — remove items now done or deleted (subscribe long-poll,
  saved searches if listed, kanban improvements, OAuth-related). Add
  the §6 deferrals as explicit "not now" entries.
- `seams_to_watch.md` — note the agent-HTML-in-same-origin trust
  assumption (already implicit; make it explicit since we're scaling
  up customer count).
- `SKILL.md` at the workspace root — update agent-facing instructions
  to reflect the four MCP tools and the permission rules file.

## 8. Acceptance for the pivot as a whole

When this pivot is done, the following all hold:

- `grep -ri 'oauth\|webhook\|taskboard\|spec-plugins' internal/ cmd/`
  returns zero hits in active code.
- `tools/list` on MCP returns four tools.
- A fresh `task build` produces a binary that, on first boot:
  1. Prints an admin invite URL.
  2. Lets the admin claim it via browser.
  3. Lets the admin create a group via UI, add a user via UI, write
     a permission rule via UI.
  4. The newly-added user can edit a page in their allowed path via
     the textarea editor, and the commit shows up in `/_activity`.
- `task test` and `task test:dogfood` both pass.
- The demo workspace (`/agency/`) has at least one HTML kanban page
  (replacement for the JSON one), one Markdown page edited via the
  in-product editor, and a `permissions.yaml` showing real groups.

That's the line. Cross it and the product is ready for paying
customers who want a hosted single-tenant deploy on a dedicated VM.

## 9. Open questions for the human

Implementation agent: do **not** decide these. Ship the rest, leave a
note in the PR, let Chris answer.

1. **Editor frontend dep.** The §5.3 editor needs a Markdown preview.
   Server-side via existing goldmark = zero new deps; client-side via
   e.g. `marked.min.js` = one new JS dep. Default to server-side
   unless that adds noticeable latency under local dev.
2. **`permissions.yaml` location.** Spec says `.agentboard/permissions.yaml`
   at workspace root. Confirm there isn't already a competing
   `.agentboard/` convention being used (e.g. `first-admin-invite.url`
   per AUTH.md). If there is, fit in; don't conflict.
3. **Activity feed scope.** §5.4 says default-branch only. If
   workspaces ever do branch-per-feature, the feed needs to choose.
   Park for now; document in the feed's code comment what it does.
