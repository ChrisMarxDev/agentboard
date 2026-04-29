# Seams to Watch

Architectural or security concerns we've **consciously deferred**. Each item is safe for today's posture (single-user, local, trusted caller on `localhost`) but will need a deliberate fix before a non-trivial change to that posture — hosted mode, multi-tenant, public exposure, or any auth layer.

**Rule of thumb:** when a seam's preconditions change ("we just added auth", "we're about to deploy publicly", "we're about to let untrusted agents write here"), re-read this file first. Don't mitigate pre-emptively — mitigate when the seam's invariant actually breaks.

Each entry follows the same shape:

- **What** — the specific concern
- **Why it's OK today** — the current invariant that makes it safe
- **Breaks when** — the triggers that invalidate that invariant
- **Mitigation options** — what we'd do when it trips, pre-researched

---

## Security

### SVG XSS via uploaded files

- **What.** `PUT /api/files/logo.svg` is served with `Content-Type: image/svg+xml`. SVG supports inline `<script>` and `on*` handlers — the file executes in the browser's origin.
- **Why it's OK today.** Only the local user can upload (no auth, localhost bound). Their own SVGs aren't attacking them.
- **Breaks when.** Hosted mode lands, auth lands, an agent running in an untrusted context can upload, or we expose the server beyond loopback.
- **Mitigation options.**
  - Reject `image/svg+xml` at upload time (`--reject-svg` flag or config).
  - Serve SVG with `Content-Security-Policy: sandbox`.
  - Rewrite via a sanitizer (DOMPurify on the server via goja is overkill; simpler to reject).
- **Logged.** `docs/archive/spec-files.md` §9.

### HTML phishing via uploaded files

- **What.** `PUT /api/files/login.html` serves an HTML page at `http://localhost:3000/api/files/login.html`. That page can imitate a real login screen and, if shared in a link, trick the user.
- **Why it's OK today.** Same as SVG: local-only, trusted uploader.
- **Breaks when.** Same as SVG. Also breaks if we ever serve behind a real domain where `*.agentboard.dev/api/files/...` would look trustworthy.
- **Mitigation options.**
  - Force `Content-Disposition: attachment` for `text/html`, `text/xml`, `application/xhtml+xml`.
  - Serve uploaded files from a separate origin (subdomain) so the main app's cookies aren't in scope.
- **Logged.** `docs/archive/spec-files.md` §9.

### User components run with full page privileges

- **What.** `components/*.jsx` files are compiled by esbuild and executed as arbitrary JS in every dashboard visitor's browser. A user component can read every other component's data, ship it to an external domain, etc.
- **Why it's OK today.** Components are authored by the project owner (or by an agent the project owner trusts). Local mode only.
- **Breaks when.** Hosted multi-tenant mode where different tenants' agents could write components another tenant's users will view. Also breaks any time `--allow-component-upload` is turned on without the operator fully trusting every caller of localhost.
- **Mitigation options.**
  - iframe each user component with `sandbox="allow-scripts"` and no shared origin.
  - Shadow DOM + `Content-Security-Policy: default-src 'self'`.
  - Separate MCP tool for "propose a component" that requires a human approval step.
- **Logged.** `spec.md` §7.6 calls this out; `CORE_GUIDELINES.md` §2.4 and §6 refer to it.

### Single-token auth gate (partial mitigation, hosted demo only)

- **What.** `--auth-token` / `AGENTBOARD_AUTH_TOKEN` enables a shared-secret gate on every route except `GET /api/health`. Accepts `Authorization: Bearer <token>`, HTTP Basic Auth with password=token, or `?token=<token>` query param. When unset, behavior is unchanged — the server is open on every interface it binds to.
- **Why it's enough for a public single-developer demo.** One developer, one token, one URL. The repo is public but the running instance isn't. Token is stored as a Coolify env-var secret, never in git.
- **What it still doesn't solve.** (1) Every authenticated caller has full admin privileges — no per-user identity, no per-tool permissions. (2) No audit log of which caller did what (the existing `X-Agent-Source` header is advisory, not authenticated). (3) Token rotation requires updating the env var and redeploying; there's no in-process invalidation. (4) Same token is shared between browser humans and MCP clients, so compromise of one compromises both. (5) All the XSS / component / file-upload seams above are still live behind the gate — a malicious authenticated caller can still plant an SVG/HTML/component payload.
- **Breaks when.** You need multi-tenant, per-tool ACLs, or an audit trail. See `spec.md` §12's "Single Token → Multi-User" progression.
- **Mitigation options.** Replace shared token with short-lived signed JWTs keyed per agent; add a signed `X-Agent-Source` claim; emit an audit log per write. Or front the whole thing with an auth proxy (Cloudflare Access, tailnet-only).
- **Implemented in.** `internal/server/middleware_auth.go`, wired in `internal/server/server.go`.

### File upload disk exhaustion

- **What.** No per-project total quota. 50 MB × unbounded uploads eventually fills the disk.
- **Why it's OK today.** Trusted caller; admins can `df -h`.
- **Breaks when.** Hosted mode (each tenant should have a byte quota), multi-agent chaos (one runaway agent loops on `PUT /api/files/x-${rand()}.bin`).
- **Mitigation options.** Add `max_project_files_mb` to config; Manager rejects writes once the total on-disk size for the project passes it.

---

## Architecture

### Single esbuild instance, synchronous per-request

- **What.** Every MDX page compile and every user-component compile goes through one embedded esbuild call. No pooling, no caching past in-memory maps.
- **Why it's OK today.** One user, sub-second builds, pages change rarely.
- **Breaks when.** Hundreds of pages, many concurrent MCP writes from parallel agents, or binary-size sensitivity (esbuild is ~15 MB of the binary).
- **Mitigation options.** Persistent esbuild service, content-addressed cache on disk, or lazily shelling out to a sidecar `esbuild` binary downloaded on first run.
- **Context.** `spec.md` §23 Q6 already names binary size as a concern.

### User components: catalog registered but not actually bundled

- **What.** `internal/components/manager.go` reads `components/*.jsx` and populates the catalog with their source, but there's no esbuild step that produces `/api/components.js` as a runnable bundle. The frontend's component registry only imports built-ins statically. User components appear in `GET /api/components` but won't render on a page.
- **Why it's OK today.** Phase 1 scope per `spec.md` §21 hasn't closed the loop yet; no tests or users depend on user-component rendering working end-to-end.
- **Breaks when.** A user writes a `.jsx` file and expects `<MyWidget />` to render on a page. Or `--allow-component-upload` is used seriously.
- **Mitigation options.** Wire esbuild's Build API over `components/`, expose the bundle at `/api/components.js`, have `componentRegistry.ts` import it dynamically at startup and on `components-updated` SSE.
- **Context.** Noticed during the component-upload plan. `bruno/component-upload/` passes because we only test the write + catalog paths, not render.

### Data store write serialization

- **What.** All writes go through a per-key mutex in Go. No batching.
- **Why it's OK today.** Trusted single-user workload. Writes are rare relative to reads.
- **Breaks when.** High-frequency ingest (hundreds of writes/sec), multiple processes sharing the SQLite file.
- **Mitigation options.** Switch to WAL mode (if not already), batch inserts, or move to a sharded approach. Most projects never hit this.

### Frontend bundle size

- **What.** `frontend/dist/assets/index-*.js` is ~1 MB after adding Mermaid. Vite's chunk-warning fires at 500 KB.
- **Why it's OK today.** Bundle is served locally; load time is ~instant on loopback. No CDN cost.
- **Breaks when.** Hosted mode over real network, mobile clients, users opening `/showcase` on first paint.
- **Mitigation options.** Code-split Mermaid (already dynamically imported — mostly solved), code-split Recharts (lazy-load `Chart`/`TimeSeries` when first used), ship a "lite" frontend without heavy viz libs.

---

## Contract stability

### MCP tool surface is effectively public API

- **What.** Every MCP tool name + argument shape is a promise to every Claude skill file already deployed. Renaming or reordering breaks Claude sessions silently.
- **Why it's OK today.** `CORE_GUIDELINES.md` §2.3 names the 13 (soon 16, with files) core tools as stable. Code review catches additions.
- **Breaks when.** We rename a tool, remove an arg, or split one tool into two without an alias.
- **Mitigation options.** Version the MCP server (`/mcp/v1`, `/mcp/v2`), or maintain a deprecation log. Not urgent while the surface is small.

### Built-in component data shapes

- **What.** Each of the 19 built-ins expects a specific shape on its `source` data key (see `spec.md` §8). Pages written in the wild assume those shapes forever.
- **Why it's OK today.** Shapes are documented; no change has been made since launch.
- **Breaks when.** Someone "improves" a component and accepts a subtly different shape, silently breaking existing pages.
- **Mitigation options.** Schema validation in the component (warn in console when shape is off), or publish per-component JSON Schemas alongside `meta`.

---

## Operational

### No backup / export path

- **What.** All data lives in one SQLite file under `~/.agentboard/<project>/.agentboard/data.sqlite`. History is retained for 30 days but never exported.
- **Why it's OK today.** Local single-user; the user can `cp` the folder.
- **Breaks when.** Something accidentally wipes it (user runs `rm -rf ~/.agentboard/default` mid-session), or hosted mode lands without a snapshot story.
- **Mitigation options.** `agentboard export <path>` that zips the project folder, or `agentboard backup` that runs SQLite `.backup`. Litestream for hosted.

### File watcher debounce may miss events under load

- **What.** 500ms debounce on `components/` and `pages/` fsnotify events. On macOS, rapid atomic-saves (e.g. VS Code's save pattern) can coalesce into a single event.
- **Why it's OK today.** Manual file edits in practice leave >500ms between saves.
- **Breaks when.** An agent writes many components in sequence via the filesystem directly (not via the REST API, which invokes ScanUserComponents explicitly).
- **Mitigation options.** Lower debounce to 100ms, or re-scan after each Write in the REST handler (we already do this for components; same for files).

---

## How to add an entry

When you defer a concern, add it here instead of letting it fade from memory. Template:

```md
### <short name>

- **What.**
- **Why it's OK today.**
- **Breaks when.**
- **Mitigation options.**
- **Logged.** (link to the spec section or PR where it was accepted)
```

If you mitigate a seam, don't delete the entry — move it to a `## Resolved` section at the bottom with the commit SHA, so the reasoning history stays visible.
