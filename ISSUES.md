# Known issues

Single canonical bug list. The spec wins ties: if an issue is filed against a feature that [`spec.md`](./spec.md) deletes or restructures, the issue is **obsolete on contact** — toss the feature, build the spec-aligned version, don't try to fix the legacy one.

Each entry is tagged:

- **`[live]`** — bug exists today and the spec keeps the surface. Real fix-target.
- **`[cut N]`** — bug exists today, the spec deletes or restructures the surface in the named cut. Don't fix the legacy code path; verify the replacement doesn't repeat the bug.
- **`[obsolete]`** — bug exists today, the surface is gone in the new spec. No fix work; remove the surface in its cut.
- **`[needs-decision]`** — behavior may be a bug or by-design. Spec is silent. Decide before fixing.

After every cut lands, walk this list and prune entries the cut resolved.

---

## Open

### MCP write tools double-stringify the `value` payload `[cut 6]`

Sending `agentboard_write({key, value: 23})` stores the literal string `"23"`. Sending `agentboard_write({key, value: {state: "running", ...}})` stores `'{"state":"running",...}'` as a string. Breaks `Metric`, `Status`, `Chart`, `Sheet`, `Table` — every component that expects a typed shape.

Workaround that worked: REST `PUT /api/data/<key>` with `{"value": <native JSON>}`. The MCP wrapper apparently `JSON.stringify`s an already-stringified value, or the schema's missing type hint coerces to string.

**Spec status:** §9 names this explicitly. Cut 6 collapses MCP to 10 tools and normalizes `value` semantics so the MCP and REST surfaces have identical type behavior. Don't patch the legacy `agentboard_write`; verify the new one round-trips JSON cleanly.

### MCP `agentboard_merge` replaces singletons with patch-as-string `[cut 6]`

`merge({key: "vet.clinic.status", patch: {label: "Open"}})` against an object singleton clobbered it down to the string `'{"label":"Open"}'` — both lost the other fields and lost the object shape. Same root cause as the write bug. REST `PATCH /api/data/<key>` with `{"value": <patch>}` works correctly.

**Spec status:** `agentboard_merge` is gone in the new spec; `agentboard_patch(path, frontmatter_patch?, body?, version?)` replaces it. The fix is "implement `agentboard_patch` correctly," not "patch `agentboard_merge`." Verify the new tool deep-merges and preserves shape.

### Initial PUT on a new key requires `If-Match: *` instead of accepting absence `[live]`

The skill manifest implies CAS is opt-in. In practice every initial PUT to a path that doesn't exist yet returns `409 Conflict` until `If-Match: *` is added. There's nothing to be stale against on a first write.

**Spec status:** §5 fixes this explicitly: "A PUT to a path with no existing leaf MUST succeed without `If-Match`." Implementation needs to match. Carry through Cut 5/6.

### PATCH error message contradicts the accepted shape `[live]`

`/api/data/<key>` PATCH error reads: *"body must be `{"value": <patch>}` (or top-level patch object)."* The "top-level patch object" branch doesn't actually work — `{"detail": "..."}` and `{"patch": {...}}` both 400 with the same error. Only `{"value": {...}}` was accepted.

**Spec status:** principle §12 (responses are repair manuals) — fix the message to match reality, OR fix the parser to accept both. The route path itself changes in Cut 5 (everything collapses under `/api/<path>`), but the error-shape contract carries forward.

### `agentboard_search_pages` returns "malformed response" `[obsolete]`

Plain query (`{q: "vet", limit: 30}`) errors out. `agentboard_search` (unified) works fine and finds the same content.

**Spec status:** §6 explicitly removes `agentboard_search_pages`. The remaining `agentboard_search` is the only search tool. No fix work; surface is deleted in Cut 6.

### `agentboard_read_page` returns body only, no frontmatter `[obsolete]`

For folder-collection cards, the frontmatter (`col`, `assignees`, `species`, …) IS the structured data. Getting only the body back makes it impossible to verify a `patch_page` landed without falling through to REST.

**Spec status:** §6 explicitly removes `agentboard_read_page`. The replacement `agentboard_read(path)` returns `{path, frontmatter, body, version}` by spec — the right shape from day one. No fix work on the legacy tool.

### MCP writes show `modified_by: "agent"` (generic) `[live]`

REST writes with the same bearer token attribute correctly to `chris`; MCP writes through the same token attribute to `"agent"`. The MCP path isn't resolving the bearer to a user.

**Spec status:** spec doesn't change attribution; this is a real bug. Fix wherever the MCP request handler decides the actor — likely missing the bearer-to-user resolution that the REST middleware already does.

### Frontmatter `order:` is ignored on write `[needs-decision]`

Set `order: 50, 51, 52, …` on each test-vet page; the server reassigned them sequentially starting at 40. Either honor the frontmatter `order:` or document that it's server-managed (and put it under `_meta`).

**Spec status:** spec §3 names `_meta.*` as server-owned but doesn't address `order:` specifically. Decide: is `order` author-controlled or server-derived from page-tree position? Pick one and document it (in spec §3 or §4) before fixing.

---

## After-cut review checklist

After each cut, walk this file and:

1. Mark `[obsolete]` issues as resolved (the surface is gone).
2. Verify `[cut N]` issues didn't re-emerge in the replacement.
3. Confirm `[live]` issues are still relevant (the route may have moved).
4. Decide and resolve `[needs-decision]` items.

If the list grows past one screen, that's a signal to ship a cut.
