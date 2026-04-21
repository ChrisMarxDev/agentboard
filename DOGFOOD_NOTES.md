# Dogfood audit — 2026-04-21

Walked the live `agentboard-dev` instance page-by-page, pruned everything that
didn't earn its keep, updated what the consolidation rendered stale. This file
captures what got deleted, what got rewritten, and the ergonomic pain points
the exercise surfaced.

## Result

| Surface    | Before | After |
|------------|-------:|------:|
| Pages      |     52 |    38 |
| Files      |      5 |     3 |
| Data keys  |    367 |   277 |

14 pages, 2 uploads, 89 data keys gone. The remaining 38 pages all describe
something that still ships.

## Deleted pages

**Tour leftovers I seeded earlier as test data** (not real project content):

- `/notes/2026-04-weekly` — fake meeting notes
- `/runbooks/deploy-fly` — fake Fly.io runbook
- `/skills/campaign-brief/SKILL` + `/examples`
- `/skills/readme-writer/SKILL` + `/examples`
- `/skills/deploy-checklist/SKILL`
- `configs/deploy.json`, `reports/q1-signups.csv`

**Stale / demo content:**

- `/inline-demo` — old "inline scalars demo"
- `/preview` — old mockup preview page
- `/features/mermaid` — duplicate of `/features/components/mermaid`

**Superseded specs** (in-repo versions are authoritative):

- `/grab` — pre-ship brainstorm; shipped feature doc is `/features/grab`
- `/specs/grab` — 16 KB spec superseded by what shipped
- `/specs/docs` — 20 KB docs-platform idea collection, mirrored in `spec-docs.md`

**Folded into other pages:**

- `/features/files` + `/features/folders` → single `/features/content` (one
  tree, one API family per CORE_GUIDELINES §9)

## Updated pages

- `/features` — overview reshuffled. Removed `/features/mermaid` card (dup),
  `/features/bruno-tests` card (never existed), added Content Model + Skills
  Hosting + Grab cards that were missing. Card count dropped but relevance up.
- `/features/skills` — was describing the pre-consolidation React route. Now
  references `content/skills/<slug>/`, mounts `<ApiList/>` live, and points to
  §9 in the intro.
- `/features/content` (new) — replaces Files + Folders. Documents the resolver
  flow (`/<path>` → page MDX → file fallback → generated folder view), the
  "every folder is a landing" rule, and the sidebar search.
- `/principles` — was "eight rules"; now nine. Content mirror of §9 added to
  `dev.principles`.

## Counters refreshed

- `dev.components.count`: 19 → 21 (ApiList + Errors joined)
- `dev.mcp.tools`: 16 → 19
- `dev.features.skills.count`: now live via ApiList; kept the numeric key in
  sync

## What felt clunky — ergonomics punch list

### 1. No lightweight tree endpoint

`GET /api/content` returns the full page source for every page — 109 KB for
52 pages in this project, and it'll only grow. When all I want is "show me
the shape of the tree" the payload is 100× what's needed. Missing:

- `GET /api/tree` — one call, returns just `{path, title, kind}[]` for
  every page AND every file in a single response. Perfect for orientation,
  sidebar rendering, CLI overview tools, or an agent saying "what's here?"
- Or: `GET /api/content?fields=path,title` — a `fields=` query param that
  drops the source column. Tiny backend change, big payload win.

### 2. No prefix filter

`GET /api/content?prefix=features/components/` would let me batch-operate on
a subtree in one call instead of 15 individual GETs. Same goes for files.
Right now there's no way to say "everything under this folder" without
pulling the whole manifest and filtering client-side.

### 3. No bulk delete

I deleted 14 pages and 89 data keys — 103 individual HTTP calls. Fine at this
scale, but a batch endpoint would be cleaner:

    POST /api/content/bulk-delete  {"paths": [...]}
    POST /api/data/bulk-delete     {"keys":  [...]}

Nice-to-have: dry-run support (`?dry_run=1`) so an agent can preview what a
prefix-delete would catch before committing.

### 4. No prefix delete

Closely related: `DELETE /api/data?prefix=dev.grab.` would have been one call
instead of 34. Same for `DELETE /api/content?prefix=skills/campaign-brief/`.

### 5. Renaming pages is awkward

`POST /api/content/move {from, to}` exists but it's hidden — not obvious from
the tree view that a page can be moved. The UI doesn't expose it. For this
audit I deleted-and-recreated instead of moving.

### 6. File listing is flat

`/api/files` returns a flat list with slash-separated names — fine for the
frontend tree builder, but awkward for humans grepping through curl output.
A hierarchical view (same shape as the tree endpoint above) would be nicer.

### 7. No "list all data keys with size" view

`GET /api/data` gives me keys + values, but a lightweight "key, byte-size,
updated-at, inferred-type" summary would make stale-key triage much faster.
I had 367 keys and had to eyeball prefixes to find the 89 to delete.

### 8. No "what references this data key" reverse index

Deleting 89 keys safely required grepping through page sources for `source=
"dev.xxx"` patterns to check if anything still referenced them. A reverse
index — "which pages and components read this key?" — would let an author
delete keys fearlessly or see immediately what breaks when a key goes away.
Schema endpoint partially covers this but only for written values.

### 9. The skill/page boundary still leaks in the manifest

`/api/content` returns `/skills/agentboard/SKILL` alongside `/features/grab`
with no hint that one is a skill manifest and the other is a docs page. The
frontend uses the sidebar tree to render both uniformly — good — but an
agent fetching the manifest has no way to say "give me only skills" without
knowing the `content/skills/` prefix convention. `GET /api/skills` handles
that one case; generalising to `/api/search?type=skill` or frontmatter
`type:` field (per spec-knowledge §2) would scale.

### 10. No visual "orphan" flag on data keys

The sidebar now shows content files perfectly, but orphan data keys (KV keys
with no current reader) are invisible. A "Data" page listing keys with a
"referenced by N pages" badge would make cleanup a regular habit instead of
a periodic archaeology dig.

### 11. Page move / rename in the nav

Folders are now clickable (good). But there's no drag-and-drop or right-click
rename in the sidebar. For a dashboard that admits human readers and
occasional human editors, a lightweight "Rename folder" on the folder row
would close an obvious gap. Backend already supports it (`POST
/api/content/move`); frontend just needs to wire it.

### 12. The `find files for a term` hole

Sidebar search filters titles + paths. Good. But it doesn't search
*content* — if I type "Fly.io", I don't find the deleted runbook via its
body. A tiny server-side `GET /api/search?q=...` that full-text-matches
page sources would let the sidebar escalate beyond title match when needed.
SQLite FTS5 was already called out in `spec-knowledge.md` §8 as the
v1 approach. Small add.

## What to implement next (ranked by leverage)

Cheap, high-leverage:

1. **`GET /api/tree`** — strip out source bodies, return one flat tree for
   pages + files. 20 LOC. Unblocks faster agent orientation and a future CLI
   tool.
2. **`?prefix=` filter on `GET /api/content`, `/api/files`, `/api/data`** —
   tiny server-side filter, saves the round-trip tax.
3. **`?fields=path,title` filter** — same pattern, just projection.

Medium-lift, big payoff:

4. **Bulk + prefix delete** — `POST /api/content/bulk-delete` and
   `DELETE /api/data?prefix=...`. Critical for cleanup jobs like this one.
5. **Full-text search** — `GET /api/search?q=...`. SQLite FTS5 over page
   sources, returns `{path, score, snippet}`. Frontend escalation path for
   the existing title filter.

Larger bets:

6. **Data key reverse index** — a table from KV keys to referring pages,
   updated on page write. Exposes orphans, warns on broken deletes, and
   lets the schema surface show "used by N pages" per key.
7. **In-UI move/rename** — right-click "Rename" on sidebar folders and
   pages, backed by the existing move endpoint.

## What NOT to build

- A specialised "notes" or "runbooks" API family. Dogfooding confirmed
  CORE_GUIDELINES §9: the two pages I tried were indistinguishable from
  regular content, and the `content/runbooks/` convention handled them
  with zero backend change. Don't specialise.
- An in-browser WYSIWYG editor. Agents write; humans read. Today the
  file path + curl pattern was awkward a handful of times, but every time
  the friction came from the read/audit side, not the write side.
