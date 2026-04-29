# Archived design docs

Drafts, brainstorms, and superseded specs that are kept around for
historical context. None of these are load-bearing for the current
codebase. Read them as design history, not as a contract.

The live design surface lives at the repo root:

- **`spec.md`** — the full design, single source of truth.
- **`spec-rework.md`** — the locked rewrite contract that drove
  files-first.
- **`spec-plugins.md`** — companion to `CORE_GUIDELINES.md` §10
  ("Version compositions, not components"); still load-bearing.
- **`CORE_GUIDELINES.md`** — product invariants.
- **`AUTH.md`** — auth design (tokens + browser sessions).
- **`HOSTING.md`** — Hetzner / Coolify deploy guide.
- **`seams_to_watch.md`** — consciously-deferred concerns.

## What's in here

| File | Why archived |
| --- | --- |
| `spec-desktop.md` | Brainstorm. Tauri-shell desktop wrapper exploration. Not on the roadmap; revisit when the hosted offering is more mature. |
| `spec-docs.md` | Brainstorm. Mapped the docs-platform feature space (Docusaurus, Mintlify, etc.) onto AgentBoard. Useful as a "future docs surface" net. |
| `spec-files.md` | Draft. Files-feature design. Superseded by what actually shipped under `/api/files/*` + the files-first store. |
| `spec-file-storage.md` | Draft. Phases 0–4 of files-first; Phase 5 ("remove SQLite KV") landed via the rewrite contract in `spec-rework.md`. |
| `spec-grab.md` | Draft. Three UX tracks for the Grab feature; track 1 shipped at `agentboard_grab` + the `/grab` UI. |
| `spec-knowledge.md` | Draft. PRD for unified knowledge + dashboards. The shape it described shipped via the files-first single-tree refactor. |
| `spec-sessions.md` | Draft. Optional sessions feature spec. Replaced by the simpler view-broker / share-cookie shape currently shipping. |

If a future change wants to revive one of these designs, copy it
back to the root and refresh the **Status** line. Don't link to
files in this folder from CLAUDE.md or any agent-facing skill —
agents should only see the live surface.
