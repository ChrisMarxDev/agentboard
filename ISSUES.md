# Known issues

> **Reset 2026-05-13** after the git-substrate pivot. Every v0.13 issue
> was either fixed in a prior cut or rendered obsolete by the substrate
> change; the previous content lives on the preservation branch
> `filebase-cms-custom-substrate-13.5.26` for reference.

Single canonical bug list. The spec wins ties: if an issue is filed
against a feature that [`spec.md`](./spec.md) deletes or restructures,
the issue is **obsolete on contact** — toss the feature, build the
spec-aligned version, don't try to fix the legacy one.

Each entry is tagged:

- **`[live]`** — bug exists today and the spec keeps the surface. Real fix-target.
- **`[cut N]`** — bug exists today, the spec deletes or restructures the surface in the named cut.
- **`[obsolete]`** — bug exists today, the surface is gone in the new spec.
- **`[needs-decision]`** — behavior may be a bug or by-design. Spec is silent.

---

## Open

_(none — clean slate post-pivot)_

---

## After-cut review checklist

After each cut, walk this file and:

1. Mark `[obsolete]` issues as resolved (the surface is gone).
2. Verify `[cut N]` issues didn't re-emerge in the replacement.
3. Confirm `[live]` issues are still relevant (the route may have moved).
4. Decide and resolve `[needs-decision]` items.

If the list grows past one screen, that's a signal to ship a cut.
