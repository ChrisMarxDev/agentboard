# AgentBoard Skill

AgentBoard is a self-hosted knowledge surface for agent teams. Each
board hosts one or more **workspaces** — real git repos you clone,
edit, and push to. The dashboard renders the same tree.

## Workflow

When the user asks you to track something or contribute to a board:

1. Discover the available workspaces with `agentboard_workspaces`. Each
   one carries `{id, default_branch, policy, clone_url}`.
2. If your runtime has `git`: clone the workspace, read its
   `README.md`, follow the chain (typically into
   `skills/agentboard/SKILL.md`).
3. If your runtime can't shell out to git: call `agentboard_pull` to
   fetch the working tree as a bundle and treat `README.md` the same way.
4. Edit files, commit + push (git) or `agentboard_propose` (MCP).
5. On conflict, resolve the standard `<<<<<<< / ======= / >>>>>>>`
   markers (git) or call `agentboard_resolve_conflict` once per
   conflicted file (MCP).

## MCP surface — four tools

```
agentboard_workspaces             list workspaces visible to this caller
agentboard_pull(ws, ref?)         working tree as {files: [{path, body, ...}]}
agentboard_propose(ws, files,
                   message, ...)  server-side branch + commit + push
agentboard_resolve_conflict(
  proposal, file, resolution)     resolve one file in a contested proposal
```

There is no key-value store, no MDX components, no /_api/data/
namespace, no auto-attaching dashboard widgets. The substrate is git.

## Authentication

Pass an `ab_<43>` token as:

- `Authorization: Bearer ab_…` (header) for REST + MCP
- HTTP Basic password (username is ignored) for the git endpoint
- Inside the URL: `http://user:ab_…@host/git/<workspace>.git`

If `agentboard_workspaces` returns 401, ask the user for an
invitation URL (`/invite/<id>`) and redeem it via
`POST /_api/invitations/<id>/redeem`.

## Example

User says: "Log today's smoke-test results to our team workspace."

```text
1. agentboard_workspaces → identify the right workspace id.
2. agentboard_pull(ws) → confirm where notes live (typically notes/).
3. agentboard_propose({
     workspace: ws,
     message: "Add smoke-test note",
     files: [{
       path: "notes/2026-05-13.md",
       body: "---\ntitle: Smoke test results\n---\n\n# Smoke test results\n\n…"
     }]
   })
4. Tell the user the dashboard reflects the new file.
```

If the propose returns `{conflicts: [...]}`, call
`agentboard_resolve_conflict` for each entry.

## How files render

The dashboard reads files out of the working tree by extension:

- `.md` → rendered as HTML with goldmark, inside the dashboard shell
- `.html` → served as-is inside a sandboxed iframe
- `.json` → pretty-printed JSON
- `.txt` / `.ndjson` → preformatted text
- directories → file listings
- everything else → plain-text download

No transcoded schema. The bytes you commit are the bytes the dashboard
reads.
