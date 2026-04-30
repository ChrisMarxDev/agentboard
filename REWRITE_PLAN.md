# Rewrite Plan — Cuts 5 & 6

> Hand this doc to another agent. Self-contained. After both cuts ship, archive this file under `docs/archive/REWRITE-cuts-5-6.md`.

## What this is

Two PRs that bring the implementation in line with [`spec.md`](./spec.md) (just locked) and [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md) §13.

The current state: 38 MCP tools across 10 domains, two parallel read paths over the content tree (`internal/mdx/` for pages, `internal/store/` for data), known stringification bugs, mismatched read-shapes (`read_page` returns body only). The spec target: 10 MCP tools (8 generic batch CRUD + grab + fire_event), one read/write path, native JSON values, full envelope on read, shape warnings instead of shape validation.

Auth and operational state (users, tokens, sessions, teams, webhooks, locks, OAuth, inbox, share, content_history index, activity index) **stay in SQLite**. This rewrite is content-tier only.

## Read before starting

In this order:

1. **[`spec.md`](./spec.md)** — the locked contract. §1 (content vs operational), §3 (frontmatter), §5 (REST), §6 (MCP — always-plural shape), §8 (suggested shapes), §11 (cut order). Source of truth; if reality drifts, the spec wins.
2. **[`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md)** — 13 principles. §8 (schemas document, don't enforce), §12 (responses are repair manuals), §13 (content is files, operational stays in SQLite).
3. **[`ISSUES.md`](./ISSUES.md)** — known bugs, tagged. Every `[cut 6]` issue is a verification step for this rewrite. Every `[obsolete]` issue resolves automatically when the surface is removed.
4. **[`CHANGES.md`](./CHANGES.md)** — what shipped in cuts 1–4 (the previous rewrite). Reference for "what already happened."
5. **[`docs/archive/REWRITE-cuts-1-4.md`](./docs/archive/REWRITE-cuts-1-4.md)** — gotchas from the prior rewrite. Internally inconsistent, but the "Gotchas" section saves time.

## Out of scope

Do **not** touch:

- `internal/auth/` — users, tokens, sessions, passwords, invitations, OAuth.
- `internal/teams/`, `internal/locks/`, `internal/webhooks/`, `internal/inbox/`, `internal/share/`, `internal/invitations/` — operational SQLite stores.
- `internal/db/` — the SQLite wrapper.
- The 32 built-in components — their `source=` semantics are unchanged.
- The frontend's `<DataContext>` / `useData` hook — read paths are unchanged from the SPA's POV (it talks to the broker, not `internal/store/` directly).
- Any auth surface (`/api/auth/*`, `/api/admin/*`, `/api/users/*`, `/oauth/*`, `/invite/*`).
- Schema validation. Per §8 + §13, the server stores whatever it parses. Warnings only — never 400 a write for shape.

## During the rewrite

The dogfood instance lives at `https://agentboard.hextorical.com` (Cloudflare Tunnel → `localhost:3000`) and runs as a host-native binary in tmux session `agentboard` on the Hetzner box. See `project_dogfood_run_pattern.md` in agent memory for restart commands.

After each cut, rebuild + restart the dogfood instance and verify:

```bash
PATH=/usr/local/go/bin:/usr/local/node22/bin:$PATH task -d /root/agentboard build
tmux send-keys -t agentboard C-c && sleep 1 && tmux send-keys -t agentboard Up Enter
curl -s -o /dev/null -w 'local %{http_code}\n' http://localhost:3000/api/health
curl -s -o /dev/null -w 'public %{http_code}\n' https://agentboard.hextorical.com/api/health
```

Both should return 200. The `/test-vet` content built during issue discovery (vet office with 6 patient cards, 8 appointments, kanban auto-attach, sheet, mermaid, etc.) is the canonical visual smoke test — every page should still render after each cut.

---

# Cut 5 — pages + store file-layer merge

## Goal

One read path, one write path, one watcher, one CAS for the entire content tree. Today there are two parallel layers (`internal/mdx/` for pages, `internal/store/` for store data) operating on the same on-disk tree. After this cut, `internal/store/` owns everything; `internal/mdx/` becomes a thin MDX-parsing utility.

## Why first

Cut 6 (MCP collapse to 10 tools) physically can't land without this. The 8 generic tools dispatch by path through one store; with two stores, the dispatcher would have to branch on path prefix, which is the current per-domain mess just hidden inside the tool layer.

## Files in scope

```
internal/mdx/           — collapse: page.go, refs.go, watch.go fold into store/
internal/store/         — gain: page semantics (frontmatter parse, MDX awareness)
internal/server/        — handlers_view.go, handlers_content.go, handlers_store.go: rewire
internal/components/    — manager.go: watcher consolidation
cmd/agentboard/         — wire-up changes
```

Tests:
```
internal/store/*_test.go — extend to cover former mdx behaviors
internal/mdx/*_test.go   — most cases migrate; some die with internal/mdx/
bruno/tests/             — should pass unchanged
scripts/integration-test.sh — should pass unchanged (45 assertions today)
scripts/smoke-test.sh    — should pass unchanged (35 assertions)
```

## Concrete steps

1. **Inventory.** Walk both packages with `gopls symbols` and `gopls references`. Document what each function does and who calls it. The seam to find: every place `internal/mdx/` and `internal/store/` both touch the same path. Examples to grep for:
   - `ScanPages`, `RefStore`, `PageManager` — mdx's surface
   - `Doc`, `Singleton`, `Collection`, `Stream`, `Envelope` — store's surface
   - Watcher: there are TWO file watchers today (one per package). Confirm and plan the merger.

2. **Define the unified Doc shape.** A `.md` page IS a singleton (frontmatter + body). A folder of `.md` is a collection. Streams are `.ndjson`. The current `internal/store/Doc` is most of the way there; extend it to carry `body string` (currently store treats everything as frontmatter-only).

3. **One watcher.** Pick `internal/store/`'s watcher as the survivor (or build a new one). Remove `internal/mdx/watch.go`. The watcher emits one event type: "leaf changed" with the path. Subscribers (RefStore, frontend SSE broadcaster, FTS5 indexer) listen on the same channel.

4. **One CAS.** Today pages have an ETag (HTTP-style); store has `_meta.version` (server-monotonic timestamp). Unify on `_meta.version` per spec §3 — pages adopt the timestamp version. The `If-Match` header still works because handlers compare the header to `_meta.version` directly.

5. **Rewire handlers.**
   - `handlers_content.go`: route handlers for `/api/content/*` call `store.ReadDoc` / `store.WriteDoc` / `store.PatchDoc` / `store.DeleteDoc` instead of `mdx.PageManager.*`.
   - `handlers_store.go`: same — routes for `/api/data/*` call the same store methods. (After Cut 5, the routes still exist as separate URLs. Cut 6's spec §5 unifies them under `/api/<path>` — that's a Cut 6 task.)
   - `handlers_view.go`: the view broker reads from the unified store. The three-layer resolution (frontmatter splat → folder-source → store) collapses to one source.

6. **Fix Issue 3 in this cut.** Initial PUT on a path with no existing leaf must succeed without `If-Match`. Today returns 409. The fix is in the unified `WriteDoc` — if the path doesn't exist, accept the write regardless of `If-Match`. Add a regression test.

7. **Fix Issue 4 in this cut.** PATCH error message contradicts accepted shape. Either fix the parser to accept `{"detail": ...}` / `{"patch": ...}` as the message claims, OR fix the message to say only `{"value": ...}` is accepted. Pick the simpler one (fix the message; only `{"value": ...}` and the body-replace shape need to work).

8. **Watcher reentry guard.** From the prior rewrite (`docs/archive/REWRITE-cuts-1-4.md` Gotchas): direct disk writes update the watcher but not the RefStore — the page renders but `<Kanban source="tasks/">` returns empty. The unified store should refuse direct disk writes (or at least: the watcher should rebuild RefStore on every change, no matter the source). Verify with: write a `.md` directly to the dogfood content folder, confirm RefStore picks it up.

9. **Delete `internal/mdx/`.** Or leave it as a tiny utility package containing only the MDX/frontmatter parser if anything else depends on those types. Prefer full deletion if achievable.

10. **Update `CHANGES.md`** with a "Cut 5" entry summarizing the merge.

## Acceptance criteria

- `internal/mdx/` is gone (or reduced to a parsing utility with < 100 LOC).
- One file watcher in `internal/server/` boot sequence.
- All bruno tests pass: `task test:bruno`.
- All Go tests pass: `task test:go`.
- Integration test passes: `task test:integration` (45 assertions).
- Smoke test passes: `bash scripts/smoke-test.sh` (35 assertions).
- Dogfood instance: every page renders, `/test-vet` is intact, no new render errors via `/api/errors`.
- Issue 3 (initial-PUT-without-If-Match): regression test added, passes.
- Issue 4 (PATCH error message): regression test added, passes.

## Risks

- **The watcher merger is the riskiest part.** Two watchers means two sources of truth for "what's on disk." Consolidation has to be airtight or pages start falling out of sync with the broker bundle.
- **Direct-disk-write reentry.** The "don't write to content/* directly" rule only existed because the watcher and RefStore were independent. After Cut 5, agents (and humans dropping files in) should be safe — verify this with an explicit test that drops a `.md` into the tree via shell and checks that it shows up in `/api/index` and is queryable via `<Kanban source="...">`.
- **CAS compatibility.** Pages today hand out HTTP ETags; if any test hard-codes ETag format, it'll break on the timestamp version. Grep for `etag` and `ETag` before flipping.

---

# Cut 6 — MCP collapse to 10 tools

## Goal

Replace the 38 MCP tools with the 10 in spec §6:

```
agentboard_read(paths)              — paths: [string]
                                       → [{path, frontmatter, body, version, warnings?}]
agentboard_list(path)
agentboard_search(q, scope?)

agentboard_write(items)             — items: [{path, frontmatter?, body?, version?}]
agentboard_patch(items)             — items: [{path, frontmatter_patch?, body?, version?}]
agentboard_append(path, items)      — items: [any]
agentboard_delete(items)            — items: [{path, version?}]

agentboard_request_file_upload(items) — items: [{name, size_bytes}]

agentboard_grab(picks)              — cross-page materialize
agentboard_fire_event(event, body?)
```

All write/read/delete tools are **always-plural batch**. Best-effort partial-success semantics. Native JSON values (no double-stringification). Full envelope on read.

## Files in scope

```
internal/mcp/
  server.go              — tool registration; trim to 10 entries
  tools.go               — page-domain tools: DELETE most; rewrite remaining as generic
  store_tools.go         — store-domain tools: collapse into the generic CRUD
  team_tools.go          — DELETE (admin moves to REST+CLI)
  lock_tools.go          — DELETE (lock is a frontmatter flag, not a tool)
  webhook_tools.go       — DELETE (admin moves to REST+CLI; fire_event stays via tools.go)
  privilege_test.go      — keep; verify the forbidden-substring test still applies

internal/server/         — REST changes:
  spec §5 unifies /api/content + /api/data under /api/<path>; this is the Cut 6 REST work
  the per-domain routes can stay as backward-compat shims OR be removed (pre-launch, no migrators
  per spec preamble — remove them, simplify the router)

.claude/skills/agentboard/SKILL.md — rewrite with the new 10-tool surface, the path-layout spec,
                                     and the suggested-shape catalog from spec §8

frontend/src/routes/InviteRedeem.tsx — bootstrap prompt mentions agentboard_get_skill and the
                                       MCP tool surface; keep the bootstrap working but update
                                       the references
```

## Concrete steps

1. **Schemas first.** Define the JSON Schema for each new tool. Be explicit about the array shape. Example for `agentboard_write`:

   ```json
   {
     "type": "object",
     "properties": {
       "items": {
         "type": "array",
         "minItems": 1,
         "items": {
           "type": "object",
           "properties": {
             "path": {"type": "string"},
             "frontmatter": {"type": "object"},
             "body": {"type": "string"},
             "version": {"type": "string"}
           },
           "required": ["path"]
         }
       }
     },
     "required": ["items"]
   }
   ```

   The MCP wrapper passes `frontmatter` through as native JSON. Specifically — the bug in Issue 1 is that `frontmatter.value: 23` arrives at the server as `frontmatter.value: "23"`. Fix the wrapper to NOT call `JSON.stringify` on already-decoded payloads. A regression test: write `{value: 23}`, read back, assert `value` is `23` (number), not `"23"` (string).

2. **Shape warnings.** Implement spec §6 Shape warnings + §8 path→shape mapping. New file `internal/store/shapes.go`:

   ```go
   type ShapeHint struct {
       Glob              string
       SuggestedFields   []string
   }

   var Shapes = []ShapeHint{
       {Glob: "tasks/**/*.md", SuggestedFields: []string{"title", "status"}},
       {Glob: "*/tasks/*.md", SuggestedFields: []string{"title", "status"}},
       {Glob: "metrics/**/*.md", SuggestedFields: []string{"value", "label"}},
       {Glob: "skills/*/SKILL.md", SuggestedFields: []string{"name", "description"}},
   }
   ```

   On every write, after the leaf lands, run the path through `Shapes`. For each matching glob, check if the suggested fields are present in frontmatter. Missing fields → emit a `shape_hint` warning into `result.warnings`. Per spec §6, warnings never block the write.

3. **Unified read.** `agentboard_read(paths)` returns `{path, frontmatter, body, version}` for each path. Delete the body-only `read_page` and the body-only response shape. (Issue 6 resolves automatically.)

4. **Unified search.** `agentboard_search(q, scope?)` — one tool, one FTS5 index, optional scope filter (`pages | data | all`). Delete `agentboard_search_pages`. (Issue 5 resolves.)

5. **Bearer-to-user resolution on the MCP path.** Issue 7: MCP writes attribute to `"agent"` instead of the bearer's actual user. Find where `modified_by` is set on writes from the MCP handler vs the REST handler — the REST middleware resolves the bearer to the user; the MCP path skips this. Wire it through. Add a test: write through MCP with a known token, read back, assert `modified_by` is the token's user.

6. **Delete the per-domain tool files.** `team_tools.go`, `lock_tools.go`, `webhook_tools.go` go away entirely. Their admin operations move to REST + CLI:
   - Team management: already via `/api/admin/teams/*` and CLI; verify the surface is complete.
   - Lock toggle: now a frontmatter patch on the page (`_meta.locked: true`). Admin gating via the existing rules engine.
   - Webhook subscribe/revoke: `/api/admin/webhooks/*`. `fire_event` stays in MCP because it's the *only* webhook surface an authoring agent legitimately needs.
   - Errors: `agentboard_clear_errors` becomes `agentboard_delete([{path: "_errors/<key>"}])` — but `_errors` is a server-managed buffer (in-memory or NDJSON in operational SQLite per ROADMAP §B). Decide: surface errors via `agentboard_read("_errors")` (returning the buffer as a virtual leaf) OR keep it as a dedicated REST endpoint without an MCP tool. Spec says expose via REST; lean toward NO MCP tool for errors in v1.

7. **Decide Issue 8 (frontmatter `order:`).** Spec is silent. Decision: `order` in user frontmatter is opaque (server stores it but doesn't use it for navigation); `_meta.order` is server-derived from page-tree position and what `<Nav>` reads. Document this in spec §3 if the decision lands. Update `internal/store/` to stop overwriting user `order`; rename the server-derived field to `_meta.order`. If keeping the current server-rewrites-order behavior, add a sentence to spec §3 saying so explicitly.

8. **Update the skill manifest.** `.claude/skills/agentboard/SKILL.md` is what every consuming agent reads first. Rewrite the "Tools" section against the new 10-tool surface. Add a "Path layout" section pulling from spec §1. Add a "Suggested shapes" section pulling from spec §8. The skill is what makes path-as-API teachable.

9. **Update the bootstrap prompt.** `frontend/src/routes/InviteRedeem.tsx` — the `claudeCodePrompt` and `otherAgentPrompt` reference `agentboard_*` tools. Make sure the example MCP calls in the prompt match the new shape (always-plural).

10. **REST surface unification (spec §5).** Today `/api/content/<path>` and `/api/data/<key>` are separate routes. Spec §5 says one namespace `/api/<path>`. Pick: (a) ship the unified namespace in this cut, or (b) keep the dual routes as backward-compat. Pre-launch — go with (a): one namespace, simpler router, less code.

11. **Update tests.** Bruno's `04-mcp/` and `10-data/` test suites both reference the old shapes. Rewrite them. Integration test (`scripts/integration-test.sh`) — assertions about `/api/components`, `tools/list` count (≥ 30) need updating: the new count is exactly 10.

12. **Update `CHANGES.md`** with a "Cut 6" entry.

## Acceptance criteria

- `tools/list` returns exactly 10 tools.
- Each `[obsolete]` issue from `ISSUES.md` is verifiably gone (the tool doesn't exist).
- Each `[cut 6]` issue passes a regression test:
  - Write `{path, frontmatter: {value: 23}}` → read back → `value` is `23` (number).
  - Patch `{path, frontmatter_patch: {label: "Open"}}` → other fields preserved, no string-coercion.
- Each `[live]` issue resolved in this cut passes a regression test.
- The skill manifest at `.claude/skills/agentboard/SKILL.md` documents the 10 tools and the suggested-shape catalog.
- The bootstrap prompt in `InviteRedeem.tsx` round-trips against the new shape.
- A test-vet rebuild from scratch (the canonical 20-write workload) takes ~2 batch calls instead of 20 singular calls.
- Dogfood instance still renders every page.
- Bruno + integration + smoke all green.

## Risks

- **Skill manifest staleness.** Every consuming agent reads it first; a stale manifest means agents call removed tools and fail. Update in the same PR as the tool changes.
- **Best-effort batching surprises.** Agents may assume all-or-nothing. Document loudly in the skill manifest and in the tool description. The response shape (`results` array + `all_succeeded`) is the contract.
- **Auth resolution on MCP.** Issue 7 has been latent; the fix has to look up the right user without breaking the OAuth (`oat_*`) path. Test both `ab_*` and `oat_*` token shapes.

---

# After both cuts

1. **Walk `ISSUES.md`.** Every entry should be either resolved or moved to a follow-up. Prune resolved entries. The file should be back to roughly empty.
2. **Update `CLAUDE.md`** if any architectural section drifts (it currently says "~40 MCP tools" — change to "10").
3. **Refresh the dogfood feature pages** at `/features/mcp`, `/features/files-first-store`, `/features/auth` to reflect the new shapes.
4. **Bump versions in CHANGES.md** with the "Top-line outcomes" table updated.
5. **Archive this file.** `git mv REWRITE_PLAN.md docs/archive/REWRITE-cuts-5-6.md` and update the archive README.

---

# Decisions locked into the spec, summarized for context

The user made these calls during planning. They're now spec — change them only by changing the spec first.

1. **"Everything is a file" applies to content only.** Auth and operational state stay in SQLite (CORE_GUIDELINES §13).
2. **MCP surface is always-plural.** Notion's pattern. 10 tools, batch by default. Single-item operations wrap in a one-element array. (spec §6)
3. **Best-effort batching, not all-or-nothing.** Per-item results, agent retries failures. All-or-nothing transactional batching is reserved for v2.
4. **No schema validation.** Per CORE_GUIDELINES §8 + §13. Server stores whatever it parses. **Speak, don't reject** — return non-blocking `warnings` when a write drifts from a suggested shape.
5. **Suggested shapes are documentation.** Spec §8 lists tasks, data singletons, skills. New types added by 2+ in-the-wild precedent. Glob → shape mapping in `internal/store/shapes.go`.
6. **No backward-compat shims.** Pre-launch; remove the old shapes, don't fork code paths.

---

# Hand-off

The plan is enough for an autonomous agent if the agent reads `spec.md` + `CORE_GUIDELINES.md` + `ISSUES.md` first. Don't touch auth or operational SQLite. Don't add schema validation. Ship Cut 5 first, then Cut 6 in a separate PR. Update `CHANGES.md` after each cut.

If you find a spec decision that *feels* open while implementing, the spec is wrong — fix the spec in the same PR as the code.

End of plan.
