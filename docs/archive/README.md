# Archived design docs

Drafts, brainstorms, and superseded specs that are kept around for
historical context. None of these are load-bearing for the current
codebase. Read them as design history, not as a contract.

The live design surface lives at the repo root:

- **`spec.md`** — the locked design contract. Single source of truth for the project shape.
- **`CORE_GUIDELINES.md`** — the 13 product principles.
- **`spec-plugins.md`** — companion to principle §10 ("Version compositions, not components"); still load-bearing.
- **`AUTH.md`** — auth design (tokens + browser sessions).
- **`HOSTING.md`**, **`SCALE.md`** — deployment + hosted infra.
- **`seams_to_watch.md`** — consciously-deferred concerns.
- **`ROADMAP.md`** — what ships next.
- **`ISSUES.md`** — known bugs (the spec wins ties: bugs in features the spec deletes are obsolete on contact).

## What's in here

| File | Why archived |
| --- | --- |
| `spec-2026-04-pre-rework.md` | The original v2 spec. Marked superseded on 2026-04-28; finally moved out of the root in the everything-is-a-file pass. Describes SQLite KV + the parallel `/api/v2` namespace that cuts 1–4 deleted. Historical context only. |
| `REWRITE-cuts-1-4.md` | Snapshot of where cuts 1–4 landed (post-files-first, pre-everything-is-a-file). Internally inconsistent — calls 8 tools "the data-plane set" but lists domain-specific names. The next rewrite (cuts 5–8 in `spec.md §11`) supersedes it. |
| `spec-desktop.md` | Brainstorm. Tauri-shell desktop wrapper exploration. Not on the roadmap; revisit when the hosted offering is more mature. |
| `spec-docs.md` | Brainstorm. Mapped the docs-platform feature space (Docusaurus, Mintlify, etc.) onto AgentBoard. Useful as a "future docs surface" net. |
| `spec-files.md` | Draft. Files-feature design. Superseded by what actually shipped under `/api/files/*` + the files-first store. |
| `spec-file-storage.md` | Draft. Phases 0–4 of files-first; Phase 5 ("remove SQLite KV") landed via cuts 1–4. The next rewrite (cuts 5–8) goes further: SQLite gone everywhere. |
| `spec-grab.md` | Draft. Three UX tracks for the Grab feature; track 1 shipped at `agentboard_grab` + the `/grab` UI. |
| `spec-knowledge.md` | Draft. PRD for unified knowledge + dashboards. The shape it described shipped via the files-first single-tree refactor. |
| `spec-sessions.md` | Draft. Optional sessions feature spec. Replaced by the simpler view-broker / share-cookie shape currently shipping. |

If a future change wants to revive one of these designs, copy it
back to the root and refresh the **Status** line. Don't link to
files in this folder from CLAUDE.md or any agent-facing skill —
agents should only see the live surface.
