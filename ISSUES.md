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

_(empty after Cuts 5–9)_

Cut 5 closed: initial PUT no-If-Match (regression test), PATCH error message contradicts shape (regression test).

Cut 6 closed: MCP value double-stringification (`agentboard_write` regression test), MCP merge object-shape clobber (`agentboard_patch` regression test), `agentboard_search_pages` malformed response (tool removed), `agentboard_read_page` body-only (tool removed), MCP writes attribute to "agent" instead of bearer's user (`Server.resolveActor` reads from auth context), frontmatter `order:` semantics (spec §3 clarified — user `order` is opaque, server-derived order travels under `_meta.order`).

Cut 7 + Cut 8 closed: REST namespace unification (spec §5) — `/api/<path>` ships, legacy `/api/content/*` and `/api/data/<key>[/<id>]` retired, `/api/<path>:append` for streams, `/api/<path>/history` for per-doc audit.

Cut 9 closed: `value:` field collision in data singletons. `MarshalDoc` now nests user objects under `value:` when the object itself contains a `value` key; the splat path remains the default for objects without a collision. Round-trip is symmetric for every shape.

---

## After-cut review checklist

After each cut, walk this file and:

1. Mark `[obsolete]` issues as resolved (the surface is gone).
2. Verify `[cut N]` issues didn't re-emerge in the replacement.
3. Confirm `[live]` issues are still relevant (the route may have moved).
4. Decide and resolve `[needs-decision]` items.

If the list grows past one screen, that's a signal to ship a cut.
