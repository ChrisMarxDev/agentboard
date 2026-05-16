# Archived design docs

Drafts, brainstorms, and superseded specs that are kept around for
historical context. None of these are load-bearing for the current
codebase. Read them as design history, not as a contract.

The live design surface lives at the repo root:

- **`spec.md`** — the design contract. Single source of truth for the project shape.
- **`CORE_GUIDELINES.md`** — the product principles.
- **`AUTH.md`** — auth design (tokens + browser sessions).
- **`HOSTING.md`**, **`SCALE.md`** — deployment + hosted infra.
- **`seams_to_watch.md`** — consciously-deferred concerns.
- **`ROADMAP.md`** — what ships next.
- **`ISSUES.md`** — known bugs (the spec wins ties: bugs in features the spec deletes are obsolete on contact).

## What's in here

| File | Why archived |
| --- | --- |
| `spec-2026-04-pre-rework.md` | Original v2 spec describing SQLite KV + the `/api/v2` namespace. Cuts 1–4 deleted it. Historical only. |
| `REWRITE-cuts-1-4.md` | Snapshot of where cuts 1–4 landed (post-files-first, pre-everything-is-a-file). Superseded by later cuts. |
| `REWRITE-cuts-5-6.md` | Implementation plan for Cut 5 (`mdx + store` merge) and Cut 6 (MCP collapse to 10 tools). Both cuts landed in the file-store era; the entire file store was retired in the git-substrate pivot. |
| `spec-plugins.md` | React-component "bricks vs. compositions" plugin architecture. The whole `.jsx` component layer was retired in the git-substrate pivot — pages are now `.html`/`.md`/JSON files rendered server-side. No bricks, no compositions versioning, no plugin runtime. Kept for the principle of "the content layer versions, the substrate doesn't" — still load-bearing in spirit. |
| `spec-desktop.md` | Tauri-shell desktop wrapper exploration. Not on the roadmap. |
| `spec-docs.md` | Mapped the docs-platform feature space (Docusaurus, Mintlify, etc.) onto AgentBoard. Useful as a "future docs surface" net. |
| `spec-files.md` | Files-feature design. Superseded by what shipped, then retired by the git pivot. |
| `spec-file-storage.md` | Phases 0–4 of files-first; the entire files-first store is gone post-pivot. |
| `spec-grab.md` | Three UX tracks for the Grab feature; track 1 shipped, then retired (no longer in the 6-tool MCP surface). |
| `spec-knowledge.md` | PRD for unified knowledge + dashboards. The shape it described shipped via the files-first single-tree refactor, then re-shipped via the git working-tree mirror. |
| `spec-sessions.md` | Optional sessions feature spec. Replaced by browser-session cookies + `oat_*` audience-scoped OAuth tokens. |

If a future change wants to revive one of these designs, copy it
back to the root and refresh the **Status** line. Don't link to
files in this folder from CLAUDE.md or any agent-facing skill —
agents should only see the live surface.
