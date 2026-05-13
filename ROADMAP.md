# Roadmap — pivot to the git substrate

> Eight cuts from where `main` is today (v0.13 file-based store) to the
> spec-defined system (git server + working-tree mirror). Each cut is
> landable independently. The dogfood instance keeps running v0.13
> throughout; the new substrate proves itself on a parallel port before
> the cutover lands.
>
> The earlier "Road to v1" plan and everything it referenced are
> superseded by this. The previous content lives on the preservation
> branch `filebase-cms-custom-substrate-13.5.26` if needed.

---

## Cut 1 — Git endpoint live

**Goal:** `git clone https://_:$TOKEN@agentboard.hextorical.com/git/probe.git`
from cowork succeeds end-to-end against the existing binary.

- Add `go-git` as a dep.
- New handler at `/git/<workspace>.git` running the smart-HTTPS protocol
  (info/refs + git-upload-pack + git-receive-pack). Behind the existing
  auth chain.
- Create one hard-coded test workspace `probe.git` with a seed commit.
- Verify clone + commit + push round-trip from cowork.

**Exit:** the cowork-probe loop we ran on 2026-05-13 succeeds against
our binary.

---

## Cut 2 — Working-tree mirror

**Goal:** a `git push` ends up rendered in the SPA.

- For each workspace: a working-tree directory at
  `<datadir>/worktrees/<name>/` that the server keeps checked out to
  the workspace's default branch.
- Post-receive hook updates the mirror and fires `page-updated` events
  through the existing SSE broadcaster.
- Crash recovery: at startup, `git reset --hard <ref>` rebuilds the
  mirror from the bare repo if anything looks off.

**Exit:** push a `.md` file into the probe workspace; it appears in the
SPA's left nav within ~1s.

---

## Cut 3 — Read API on the working tree

**Goal:** `/api/<path>` reads from the working tree, not the page
manager.

- Repoint `handlers_unified.go::handleUnifiedRead` to walk the working
  tree directly. The bundle shape is unchanged.
- The watcher continues to fire the same SSE events.
- Drop the in-memory page index gradually — keep a thin cache only if
  measurements show it matters.

**Exit:** the SPA renders against the working-tree mirror with no
visible behavior change. The page manager package is the next thing
slated for deletion.

---

## Cut 4 — Retire the v2 file store

**Goal:** delete code we no longer need.

- Remove `internal/store/{singleton,collection,stream,catalog}.go` and
  their handlers.
- Remove the dispatch logic in `handlers_unified.go` that used to pick
  between page tier and data tier (post-§14 there's only one tier; this
  is the last cleanup).
- Remove `.agentboard/activity.ndjson` writing. Activity is `git log`.
- Remove `_meta.version` stamping on writes. CAS is git.
- Remove the page-lock + page-approval tables.

**Exit:** smaller binary, smaller test surface. The `internal/store/`
package shrinks to the parts that drive search and frontmatter parsing.

---

## Cut 5 — MCP rewrite

**Goal:** the seven tools from spec §6, replacing the v0.13 ten-tool
batch CRUD.

- Drop `agentboard_write / patch / append / delete / request_file_upload`.
- Add `agentboard_workspaces / pull / propose / resolve_conflict /
  subscribe`.
- Keep `agentboard_grab` and `agentboard_fire_event` unchanged.
- Update the seeded `SKILL.md` in `internal/project/init.go` to teach
  the new surface.

**Exit:** an agent who reads `agentboard_workspaces` and the bundled
SKILL knows how to clone, propose changes, and resolve conflicts —
either by shelling out to `git` or via MCP fallback.

---

## Cut 6 — Concurrency policy + always-PR

**Goal:** the two policies from spec §5.

- Per-workspace toggle in the registry: `policy: push-to-main |
  always-pr`.
- New `proposals` SQLite table for the always-PR path.
- `agentboard_propose` accepts a branch name and bypasses any
  push-to-main rules.
- `proposal_merge` MCP call and a small UI affordance to merge a
  proposal.

**Exit:** a shared production workspace can require PRs while a
personal scratch workspace stays push-to-main. The same agent toolchain
works against both.

---

## Cut 7 — Dogfood cutover

**Goal:** `agentboard.hextorical.com` runs on the new substrate.

- Build the `agentboard migrate-from-filebase` CLI: `git init`, `git
  add`, `git commit` over the v0.13 working tree.
- Run the migration against `/root/agentboard-data/` into a fresh
  workspace.
- Switch DNS / tmux to the new binary; keep the v0.13 binary on a
  fallback port for one week.
- Verify the kanban + frontmatter metadata panel + skills + activity
  feed all render correctly against the new substrate.

**Exit:** the live dogfood instance is fully on the git substrate; the
fallback v0.13 stays available for comparison.

---

## Cut 8 — Cleanup + documentation

**Goal:** the codebase looks like the new system, not the old one with
new bits bolted on.

- Drop the v0.13 fallback binary; archive the project state from the
  preservation branch.
- Scrub the docs of v0.13-era language (the seeded SKILL still mentions
  some `.agentboard/` paths that no longer exist).
- Final pass on `ISSUES.md` — anything left from v0.13 is either fixed
  by the substrate change or carried as a real issue against the new
  shape.

**Exit:** the project description on the README, the seeded SKILL, and
the dogfood `/skills` page all describe the same product. The next
contributor reading any of them gets the same picture.

---

## What's deliberately not on this roadmap

- A built-in PR review UI. Read the diff via `git diff` or look at the
  branch via the SPA's branch picker (which arrives whenever Cut 6
  benefits from it, not before).
- A built-in issue tracker. Issues are files in `issues/`; the existing
  kanban already renders them.
- Real-time collaborative editing. The agent collaboration model is
  "push to a branch, merge fast-forward, rebase on conflict" — not
  CRDTs.
- Mobile app. Web UI only.
- Marketplace. Plugins-as-files in `components/` with URL installs is
  the model in `spec-plugins.md`; that doesn't change with the pivot.
