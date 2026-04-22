# Road to v1

The concrete path from where AgentBoard is today to something a non-technical team can pick up and use safely. Synthesis of the decisions on `/product-direction`, `spec-plugins.md`, `/features/history-and-backup`, and the principles in `CORE_GUIDELINES.md`.

**v1 framing:** "GitHub for non-technical teams." One hosted instance per team. Agents + humans co-author the content tree. Remote-only, optimistic concurrency, self-cleaning history + GFS backups. Phased plugin ecosystem.

---

## Two cut lines

There are two release targets — a near-term dogfoodable cut that lets a small team test the product, and the real v1 that adds history + backup + full plugin foundations.

- **Dogfood cut (v0.5) — ~3 weeks, 5 milestones (D1–D5).** Minimum to test with a real team of 2–5 people. Explicitly defers history + backup; you rely on manual care + Git in the ops layer.
- **v1 cut — dogfood cut + milestones A/B/C/D/E/F.** Full safety net: optimistic concurrency, content_history, activity feed, admin restore, §10 contract tests, path ACLs.

```
[Phase 0 SHIPPED]
        │
        ├─→ [D1 concurrency] ──→ [D2 onboarding UX] ──→ [D3 path ACLs] ──→ [D4 plugin graceful + mentions] ──→ [D5 mobile]
        │                                                                                                            │
        │                                                                                              ━━━━━━━━━━━━━━┻━━━━━━━━━ v0.5 (dogfood cut)
        │                                                                                                            │
        └─→ [B history + backup] ──→ [C safety layer] ──→ [D UI surfaces] ──→ [E plugin contract] ──→ [F full workspaces]
                                                                                                                     │
                                                                                                              ━━━━━━━┻━━━━━━━━━ v1
                                                                                                                     │
                                                                                                      [G plugin sandboxing]
                                                                                                      [I ergonomics / search]
```

---

## Dogfood cut (v0.5) — what ships for a testable multi-user product

Goal: a team of 2–5 people (1–2 humans + their agents) can use one AgentBoard instance for a week without stepping on each other, without losing work, and on the devices the direction doc commits to (phone + cloud agent + browser). History, backup, and rollback are deferred — the team leans on conservative behavior + manual snapshots until we ship the full safety net.

**Why this cut exists.** History and backup are the biggest chunk of work in v1 (~2 weeks of Milestone B alone) and they only matter *after* something goes wrong. Before we ship them, the dogfood mitigations are: don't let two writes clobber each other (concurrency), don't let anyone write anywhere they shouldn't (path ACLs), keep a manual backup as a cron job (`sqlite3 .backup` + tar + scp — no UI needed). That gets us to testable without blocking on the big piece.

**What you accept by shipping this cut:**
- If something goes wrong (a bad edit, an agent loop), recovery is *command-line only*. No history restore UI.
- No activity feed showing "who did what" — attribution lives on individual pages (last-edited-by) and in server logs.
- No rate-limit-triggered alerts. The path ACLs + rate limits in D3 prevent the big-blast-radius case; subtler misuse goes unobserved.

**What you *don't* accept:** silent write clobbering (D1), any token writing anywhere (D3), missing-brick pages hard-failing (D4), the phone being unusable (D5). Those stay blocking because they're *current* failure modes a test team would hit on day one.

### D1 — Optimistic concurrency on pages + data (~1 week)

Same design as Milestone A below but scoped: pages and data keys only (skip files for v0.5 — binary upload clobbering is rarer and lower-impact). Adds `If-Match` + returns 412 with current state.

- [ ] Pages: include `etag` in `GET /api/content/:path`; accept `If-Match` on PUT/DELETE.
- [ ] Data keys: `updated_at` is already returned; accept `If-Match` on the write verbs; 412 includes current value.
- [ ] MCP tool wrapper: read → edit → write-with-If-Match with 3-attempt retry + semantic merge.
- [ ] Bruno contract tests for the happy path + stale-write path.

**Why blocking:** two agents on the same page silently clobber today. That's the first thing a team of two will hit.

### D2 — Team onboarding UX (~3–5 days)

The CLI is fine for self-host; for team dogfood the bootstrap has to happen in the browser.

- [ ] First-run setup page at `/admin/setup` — if no admin users exist, show a "Create first admin" form (minimal: username + display name). The form consumes a one-time bootstrap code created via `agentboard admin mint-bootstrap-code` on the host. Code expires on use.
- [ ] Admin invite flow on `/admin`: "Invite user" generates a token and returns a share-link preloaded with the token + a login form. Click once → your session has the token.
- [ ] Clear 401 pages: when the user lands unauthenticated, the login surface says "paste your invite token" with the format hint. No dead 401 text.
- [ ] `agentboard admin list-tokens --user <name>` rotation UX: surface on `/admin` too.

**Why blocking:** a team of five is not going to SSH into the host five times.

### D3 — Path ACLs + rate limits (~1 week)

Minimum safety floor. Combines the path-scoped half of Milestone F with the cheapest pieces of C.

- [ ] Extend the auth rules engine (already exists, `access_mode` + `rules_json`) to support `{op: allow_write, path_prefix: "workspaces/alice/"}`. Middleware enforces on mutating routes → 403 on violations.
- [ ] Seeded-on-first-login: every user gets `content/workspaces/<username>/` auto-created with write access. `shared/` has open write for all active users.
- [ ] Per-token rate limit: default 30 writes/min (config knob). 429 on exceed.
- [ ] Admin rules UI on `/admin/users/:username` — list of path-prefix rules with add/remove.

**Why blocking:** one token with write-everywhere is fine for a solo dogfood but fails the "one compromised agent wipes the tree" test on a team.

### D4 — Plugin graceful degradation + attribution (~3–5 days)

Carved out of Milestone E (full contract suite is v1). Two concrete things:

- [ ] Missing-brick placeholder: page references `<UnknownBrick/>` → page renders with a dotted-border "brick not installed" card in place, not a compile error. Rest of the page still renders.
- [ ] `@mention` rendering in MDX: `@alice` → lookup in users table → render as a colored badge with avatar. If the user exists, link to a (simple) user page; if not, render as plain text.
- [ ] "Last edited by" footer on every page: reads from a new lightweight `page_meta(path, last_actor, last_at)` table updated on every write. Three columns, not a full history table — just the most recent write.

**Why blocking:** without the placeholder, a teammate deleting a `.jsx` file kills every page referencing the brick. Without attribution, nobody knows who wrote what and the "who did what" pressure-test is impossible.

### D5 — Mobile polish (~1 week)

The direction doc commits to phone + cloud agents being first-class. Dogfood on the phone has to work or the direction is theoretical.

- [ ] Sidebar becomes a slide-over drawer under a breakpoint. Hamburger toggle. Swipe-to-close.
- [ ] Touch-target audit: Grab checkboxes, folder chevrons, page action menu, tab order — all ≥ 44px hit areas.
- [ ] Grab tray positions above the keyboard on mobile; buttons scale up.
- [ ] Token-paste login works on phone Safari + Chrome (verify).
- [ ] Smoke test: a team member can open a page → grab three cards → copy to share sheet → paste into ChatGPT mobile, on an iPhone. That's the 🎯 use case.

**Why blocking:** mobile is in the pitch. A pitch that doesn't ship on the pitch's hardware isn't real.

### What stays explicitly deferred from the dogfood cut

- **All of Milestone B (history + backup).** Replaced by a manual cron job in the install script: `sqlite3 .backup + tar + scp`. Operator's problem for v0.5. Admin UI comes in v1.
- **Anomaly alerts + mass-revert** (parts of C). Rate limits in D3 cover the "runaway loop" case; mass-revert waits for content_history.
- **History menu in page actions** (part of D). Users know what they wrote; v0.5 assumes good memory + Git on the ops side.
- **Full §10 contract suite** (part of E). D4's graceful placeholder covers the LOC the test would catch; the smoke test matrix is v1.
- **Branch-scoped workspaces** (Milestone F Phase 2). Path ACLs cover 90% of the pain.
- **Plugin sandboxing** (Milestone G). First-party + upload-JSX is enough for the dogfood team to author their own bricks.

### Total effort

5 milestones × 3–5 days each = **~3 weeks of focused work** to v0.5. Then B/C/D/E/F turn it into v1.

---

## Phase 0 — Shipped today

What's already in the binary and running on the dogfood instance.

### Core
- Single binary, zero runtime deps. VPS deploy script (`task deploy:vps`). Coolify control plane wired (`scripts/new-board.sh`, `.github/workflows/redeploy-coolify.yml`) per `SCALE.md`.
- One-tree content model (§9 consolidation). `content/` holds pages + files; every folder is routable. Generic FileViewer fallback.
- KV data store with 7 write ops, SSE broadcaster, `data_history` retention (the model we'll mirror for `content_history`).
- 21 built-in components. ApiList as the generic `/api/*` list renderer (§9).
- Grab (cards + headings + whole-page picks, three output formats).
- Skills hosting (`content/skills/*/SKILL.md` + `GET /api/skills`).
- 19 MCP tools.
- Sidebar with unified content tree + client-side search (`/` shortcut).
- 10 principles codified in `CORE_GUIDELINES.md`, mirrored at `/principles`.

### Auth (just landed)
- Username is identity. `@alice` IS the user. Immutable + reserved forever.
- One credential class: bearer tokens (`ab_...`). No sessions, no passwords.
- `kind: admin | agent`. Admin unlocks `/api/admin/*`.
- CLI: `agentboard admin mint-admin / rotate / rename-user / list`.

### Docs
- `CLAUDE.md`, `CORE_GUIDELINES.md`, `AUTH.md`, `HOSTING.md`, `SCALE.md`, `spec-plugins.md`, `DOGFOOD_NOTES.md` — all in sync with shipped code.
- Seeded `SKILL.md` teaches external agents the API contract and the "never write to disk directly" rule.

**What this is not yet:** a safe multi-user product. Every writer has full privileges; a stale-write clobbers silently; there's no audit trail or rollback; no guardrail against a runaway agent.

---

## The v1 cut line — six milestones

Each milestone is self-contained. Ship in order; each one raises the product's safety bar by one layer.

### A. Optimistic concurrency everywhere (~1 week)

**The primitive that makes multi-writer safe.** Every mutating endpoint accepts `If-Match: <etag-or-updated-at>`, returns `412 Precondition Failed` on stale writes with the current state in the body. MCP tools + SDK wrap read→edit→write with automatic retry + semantic merge.

- [ ] Add `If-Match` handling to `PUT /api/content/*`, `PUT /api/data/*`, `PATCH /api/data/*`, `POST /api/data/*`, `DELETE /api/content/*`, `DELETE /api/data/*`.
- [ ] Include current `etag` / `version` in GET responses (files already have etag; pages need one; data keys need `updated_at` normalized).
- [ ] 412 body: `{ current_source, current_version, current_updated_by, current_updated_at }`.
- [ ] MCP tool wrapper: read → edit → write-with-If-Match. On 412, fetch current state + re-apply the intent → retry (bounded to 3 attempts).
- [ ] Bruno contract tests for all the write endpoints.

**Principles:** §7 (reliable rails), §10 (compositions not components — rollback of a conflicting write uses the same rails).

**Unblocks:** Milestone B (history captures versions), C (mass-revert by token needs versioned writes), E (plugin `requires:` frontmatter uses the same version concept).

### B. History + backup core (~2 weeks)

The three-layer design from `/features/history-and-backup`. All three ship together because they share the same compactor + observability surface.

- [ ] `content_history` table: `id, path, version, actor, source (BLOB), size, created_at, UNIQUE(path, version)`. Index on `(path, created_at DESC)`.
- [ ] `activity` table: `id, actor, action, path, size_before, size_after, created_at`. Indexes on `(created_at DESC)` and `(actor, created_at DESC)`.
- [ ] Write-transaction wrapper: INSERT history + DELETE "not-in-top-50-per-path" + INSERT activity, atomic.
- [ ] Nightly compactor goroutine: hot/warm/cool/cold tiering (see history-and-backup page for the table). `VACUUM` after. Config knobs in `agentboard.yaml`.
- [ ] GFS backup cron baked into the VPS install script (was always planned in SCALE.md point 8): hourly tar → 24 / 7 / 4 / 12 retention. `rclone sync` to S3/R2 optional.
- [ ] `POST /api/content/<path>/restore?version=X` endpoint — restores by writing a NEW version (rollback is itself in history).
- [ ] Same for `POST /api/data/<key>/restore?version=X`.
- [ ] MCP tools: `agentboard_list_history(path)`, `agentboard_restore_version(path, version)`, `agentboard_list_activity(filter?)`.

**Principles:** §7, §10. This is also the "faulty agent nuked my doc" safety net the user asked for.

**Unblocks:** C (mass-revert needs content_history), D (UI surfaces these tables), F (activity is where you SEE a workspace misuse).

### C. Safety features against bad tokens (~1 week)

The "one malicious / buggy agent can't kill the tree" layer.

- [ ] Per-token rate-limit middleware. Default `writes_per_min: 30`. Configurable in `agentboard.yaml`.
- [ ] Bulk-delete gate: more than N delete operations in a window requires admin confirmation (config flag for now, admin-approval UI in Phase 2).
- [ ] `POST /api/admin/revert-by-token` — mass-revert all content_history rows by a given actor within a time window. Backed by content_history.
- [ ] Token revocation surfaces immediately: cache invalidation in the auth middleware on rotate/revoke.
- [ ] Anomaly heuristic: activity feed flags a token doing >N writes in <M minutes.

**Principles:** §7, §10.

### D. UI surfaces for history + admin (~1–2 weeks)

Everything in B/C is SQL tables until someone can see it.

- [ ] Activity feed component (new built-in: `<ActivityFeed/>`) for the home page. Renders the last N rows of the activity table with icons per action, time ago, actor avatar.
- [ ] History menu in PageActionsMenu: "History" opens a side panel with version list, each row clickable to show diff + restore button.
- [ ] Admin `/admin/storage` page: current history size per top-level folder, oldest-rotation-eligible version per tier, backup schedule, one-shot "Compact now" + "Snapshot now" buttons.
- [ ] Admin `/admin/restore` page: list of snapshots, Restore button with confirmation modal.
- [ ] Home page gets a default activity feed card in the seeded `index.md`.

**Principles:** §5 (non-technical reader — must be glance-able), §6 (one-way flow — these pages read; writes happen server-side).

### E. Plugin foundations — §10 contract made real (~1–2 weeks)

Current components work but the plugin-ecosystem invariants aren't enforced. This milestone makes §10 + `spec-plugins.md` real for Phase 1 (first-party + upload-JSX).

- [ ] `meta.allowed_hosts` field in component manifests (currently only `props`). Enforced via CSP for uploaded bricks.
- [ ] `requires:` frontmatter on pages:
  ```yaml
  ---
  requires:
    GithubIssues: ^1.2.0
  ---
  ```
  Page renderer checks installed versions, renders a placeholder if mismatch.
- [ ] Graceful placeholder component: "Brick `X` not installed" / "requires v2, found v1" / "manifest error". Never hard-fails the page.
- [ ] §10 contract test suite: for every built-in brick, a smoke test proves an old composition source still renders under the current implementation. Fails CI on a breaking change without a version bump.
- [ ] `agentboard_list_components` MCP tool returns versions. Agents that author pages can reason about what's installed.

**Principles:** §3, §8, §10.

**Unblocks:** G (sandbox path layers on top of this manifest shape).

### F. Workspaces (path-scoped ACLs) (~1 week)

The first real multi-user primitive. Path-scoped only for v1; branch-scoped review gates are Phase 2.

- [ ] Extend the existing rules engine (`auth.rules_json`) to support `path_prefix` rules per user.
  ```json
  [{"op":"allow_write","path_prefix":"workspaces/alice/"},
   {"op":"allow_write","path_prefix":"shared/"}]
  ```
- [ ] Middleware enforces: write to a path outside allowed prefixes → 403 with a clear message.
- [ ] Admin UI for editing user rules (extends existing `/admin` user page).
- [ ] Seeded workspace pattern: `content/workspaces/<username>/` created on first login; user gets write by default. Shared content has open write to all members.
- [ ] Activity feed records the actor vs. target-path mismatch if someone with allow_write but outside their workspace pokes into another area (surfaces misconfiguration).

**Principles:** §3, §9 (workspaces are folder conventions + a thin ACL layer, not a separate storage system).

---

## The v1 cut line

After A–F, the product is:

- ✅ Safe for multiple agents + humans on the same instance (optimistic concurrency).
- ✅ Observable (activity feed, history, storage metrics).
- ✅ Recoverable (per-file rollback, full backup + restore, mass-revert by token).
- ✅ Protected (rate limits, bulk-delete gate, workspace ACLs).
- ✅ Forgiving (missing bricks don't break pages, no hard compile errors).
- ✅ Plugin-ready at the contract level (Phase 1 — upload JSX with manifests).

That's v1. Ship.

---

## Post-v1 milestones (sequenced, not gating)

### G. Plugin sandboxing — spec-plugins Phase 2 (~2 weeks)

- [ ] `agentboard plugins install <url>` CLI. Fetches bundle + signed manifest from any public URL.
- [ ] Bundle format: ESM module + `manifest.yaml` + signature. Simple hash + author pubkey.
- [ ] Sandboxed iframe per third-party brick. `postMessage` bridge for prop-scoped data reads.
- [ ] CSP per-brick, computed from `manifest.allowed_hosts`.
- [ ] `agentboard plugins uninstall / update / pin`.
- [ ] Revocation tombstone so existing pages degrade gracefully.

**Why not v1:** first-party + upload-JSX bricks cover the dogfood use case. Third-party adoption needs the distribution story first.

### H. Mobile + polish (~1 week)

- [ ] Phone-sized sidebar (drawer/off-canvas).
- [ ] Touch-sized interactive elements (checkboxes, Grab picks, menus).
- [ ] Grab on phones (probably the #1 use case: read on phone → grab sections → paste into agent).
- [ ] Verify the auth + mint flow works on mobile.

### I. Ergonomics (per DOGFOOD_NOTES) (~1 week)

- [ ] `GET /api/tree` — lightweight manifest (no source bodies). Unblocks faster agent orientation + CLI tools.
- [ ] `?prefix=` filter on `/api/content`, `/api/files`, `/api/data`. Saves round-trip tax.
- [ ] `?fields=path,title` projection filter.
- [ ] Bulk + prefix delete (`POST /api/content/bulk-delete`, `DELETE /api/data?prefix=...`).
- [ ] `--project` inheritance for `admin` CLI (falls through to default today → silent wrong-project operations).

### J. Full-text search (~1 week)

- [ ] SQLite FTS5 index over page sources + file metadata.
- [ ] `GET /api/search?q=...` returns `{path, score, snippet}`.
- [ ] Sidebar search escalates from title match to content match on no hits.
- [ ] MCP `agentboard_search` tool.

### K. Data-key reverse index

- [ ] Table mapping data keys → pages referencing them via `source=`.
- [ ] Keeps orphan keys surfaceable ("this key is read by 0 pages — safe to delete").
- [ ] Used in `/admin/storage` for orphan cleanup.

### L. Review gate for shared paths (workspaces Phase 2)

- [ ] Write to a flagged path goes to `pending` state.
- [ ] Another user approves; then it lands.
- [ ] Configurable per-folder via `.agentboard.yml`.
- [ ] Opt-in only.

### M. Plugin registry — spec-plugins Phase 3

- [ ] `plugins.agentboard.dev` — curated metadata only, bundles self-hosted.
- [ ] `agentboard plugins install <name>` resolves via registry.
- [ ] Configurable alternate registries.

### N. Richer concurrency

- [ ] WebSocket channel for optimistic-UI updates on conflict (show "alice just saved" before the server responds).
- [ ] CRDT for collections (Kanban ordering, Log append) so concurrent writes merge without If-Match bounce-back.

---

## Risks + assumptions

- **Optimistic concurrency assumes AI can merge semantically.** If real-world conflicts produce bad merges, we need a human-review path. Watch the kill signal on `/product-direction`.
- **The sandboxed iframe trust model is load-bearing for the ecosystem.** Getting it wrong (too permissive OR too restrictive) kills third-party plugin adoption. G is a real design investment, not a copy-paste.
- **Backup restore UX is bimodal**: either nobody uses it (means we never tested it in panic) or people use it wrong. Instrument it — activity feed entry on every restore — so we see usage.
- **Workspaces as folder conventions need a story for "shared prose that any team member edits."** Current plan: `shared/` gets open-write for all members. Worth dogfooding before calling it done.

---

## What shouldn't go in v1

Rejected or deferred:

- **Local sync client.** Decision made on `/product-direction`: remote-only. Mobile + cloud agents fit naturally; offline work is a minority use case. Revisit only if a kill signal fires.
- **Real-time collaborative editing.** Not in scope. Edits are async, SSE broadcasts the result not the keystroke.
- **Multi-tenant binary.** One instance per team stays (principle §2).
- **Git under the hood.** History is SQLite, backup is tar. Git adds no capability we need once optimistic concurrency + content_history lands.
- **A marketplace with transactions.** Plugin distribution is open-web (git / public URLs). Registry is metadata-only if we ship one at all.
- **Email-based auth recovery.** `AUTH.md` is explicit: filesystem recovery via CLI only. No SMTP anywhere.

---

## Where this document lives

- This file (`ROADMAP.md`) — the plan.
- `/product-direction` — the *why* behind the plan.
- `CORE_GUIDELINES.md` — the principles the plan has to pass through.
- `/features/history-and-backup` — the full design for Milestone B.
- `spec-plugins.md` — the full contract for Milestones E and G.
- `DOGFOOD_NOTES.md` — the ergonomics wishlist that feeds Milestone I.
- `SCALE.md` — hosted infrastructure; Milestone B's backup work satisfies its install-script point 8.
- `AUTH.md` — the auth primitives Milestones C and F build on.
