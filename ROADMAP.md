# Roadmap

> What ships next on the post-pivot AgentBoard (git substrate + HTML dashboard). Items are roughly ordered by leverage, not by commitment — the live work is whatever earns its way to the top.

---

## Workspace UX

- **Branch picker** — toggle the working-tree mirror to a non-default branch for read-only browsing. The "always-PR" workspace policy (see spec §5) becomes useful the moment this lands.
- **Per-page lock affordance** — a workspace-author can mark a page "human approval only"; agent commits to that path are rejected with a clear "needs review" error pointing at a merge tool.
- **Diff visualization on history rows** — current `?history=1` lists commits; clicking through to `?diff=<sha>` works but is plain. Side-by-side view, syntax highlighting per file extension.
- **Inline mentions** — `@username` in any rendered file resolves to the dashboard's user table; clicking opens that user's recent activity.

## Discovery

- **Workspace-level search facets** — current FTS is full-text only. Add filters by extension, path prefix, last-edited-by, last-edited-since.
- **Recent activity feed at `/`** — bare list of the N most recent commits across workspaces this user can read.
- **Saved searches** — bookmark `/?q=foo&type=md&owner=alice` as a named view.

## Agent quality-of-life

- **Long-poll `agentboard_subscribe`** — the tool exists; the SSE shape works through the dashboard. Wire it through MCP so agents can wait on `conflict.push_rejected` instead of polling.
- **`agentboard_propose` bundle responses** — return the full working-tree bundle on conflict, not just the file list, so the agent can resolve in one round-trip.
- **Skill registry** — a workspace can ship `SKILL.md` files in well-known paths; the dashboard surfaces them in a Skills tab so new agents can browse them.

## Hosting

- **Persistent-volume Coolify path** — the dogfood instance currently runs on ephemeral storage (`/tmp`); a small volume + the right Coolify env keep state across auto-stops. See `HOSTING.md` for the open work.
- **`scripts/new-board.sh` polish** — provisioning a per-friend board takes one command today; the rough edges are around DNS + Cloudflare token plumbing.

## Substrate hardening

- **`go-git` swap-in** — the smart-HTTP endpoint currently shells out to `git http-backend`. The CGI dependency is fine but a pure-Go single-binary story is cleaner. Mostly a drop-in swap once `go-git`'s push semantics are verified against the current test suite.
- **Always-PR concurrency policy** — push-to-main works for personal scratch; shared production wants every change to land on a feature branch with explicit merge. The proposal table + `proposal_merge` MCP call are sketched in spec §5; needs implementation.
- **Workspace-level rate limits** — per-token rate limit on push exists; add a per-workspace cap so one runaway agent can't fill the disk in a tight loop.

## Documentation + outreach

- **Landing page refresh** — `landing/` Astro site is from the file-store era; the value prop needs to match "git workspace humans + agents share".
- **One-page install guide for non-developers** — Homebrew formula + an install script that handles the systemd / launchd plumbing.
- **Recorded demo of the full flow** — clone → commit → push → see it in the browser → SSE toast. Under 90 seconds. Used in the landing page and the README.

---

## Deliberately not on this roadmap

- A hosted SaaS plan. AgentBoard is self-host-first. We don't operate boards for paying customers; we ship the software they run.
- A general-purpose data store. Files are the artifacts. There is no `/api/data` plane; data shapes are file conventions (taskboard JSON, CSV, frontmatter on Markdown), and the renderers handle them.
- A native desktop app. The web UI is the only UI. Agents use git or MCP; humans use the browser.
- Real-time collaborative editing of prose. Two agents disagreeing about a file is git-merge territory, not CRDT territory.
