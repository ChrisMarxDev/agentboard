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

### Data singleton drops user-supplied `value:` field on read round-trip `[needs-decision]`

Writing `{"value": {"label": "DAU", "value": 42}}` to a singleton splats `label` + inner `value` into the frontmatter on disk. On read, `UnmarshalDoc` treats `value:` as the envelope wrapper and drops it when other keys are present (`internal/store/envelope.go::UnmarshalDoc` "Drop a stray `value:` if other keys are present — the object shape wins"). Net: writing `{label, value: 42}` round-trips as `{label}`, losing the `value: 42` field.

Pre-dates Cuts 5–7 (deliberate design call when the splat encoding landed). Hits any agent that authors a singleton with a literal `value` key. Decide:

- a) Reserved-key documentation: `value` is a reserved frontmatter key for top-level singletons — agents must rename to `quantity` / `score` / etc. Document in spec §3.
- b) On-disk encoding switches to nested: `_meta:` + `value:` always at top level, never splatted. More verbose YAML but no collision.
- c) Detect: if the user-supplied frontmatter object contains a `value` key, marshal it as `value: <object>` (not splat) so the round-trip is symmetric.

Lean (c) — least breaking, preserves agent intent. Workaround until then: pick a different field name for the literal value.



Cut 5 closed: initial PUT no-If-Match (regression test), PATCH error message contradicts shape (regression test).

Cut 6 closed: MCP value double-stringification (`agentboard_write` regression test), MCP merge object-shape clobber (`agentboard_patch` regression test), `agentboard_search_pages` malformed response (tool removed), `agentboard_read_page` body-only (tool removed), MCP writes attribute to "agent" instead of bearer's user (`Server.resolveActor` reads from auth context), frontmatter `order:` semantics (spec §3 clarified — user `order` is opaque, server-derived order travels under `_meta.order`).

---

## After-cut review checklist

After each cut, walk this file and:

1. Mark `[obsolete]` issues as resolved (the surface is gone).
2. Verify `[cut N]` issues didn't re-emerge in the replacement.
3. Confirm `[live]` issues are still relevant (the route may have moved).
4. Decide and resolve `[needs-decision]` items.

If the list grows past one screen, that's a signal to ship a cut.
