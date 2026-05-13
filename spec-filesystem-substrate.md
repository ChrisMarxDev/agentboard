---
status: planning — no implementation yet
created: 2026-05-13
follows: cuts 1–8 (git substrate)
supersedes_naming: spec-html-substrate.md (renamed — scope is broader than HTML alone)
relates_to: spec.md, CORE_GUIDELINES.md §5/§14/§15, ROADMAP.md Cut 4, seams_to_watch.md
---

# AgentBoard Substrate Pivot — Planning Doc

> **Planning doc, not committed direction.** Builds on the git-substrate pivot (Cuts 1–8, landed on `main` 2026-05-13) and explores three coupled cuts that together collapse the product's storage, type, and rendering layers into one coherent model. Implementation does not begin until the decision criteria at the bottom are met and the git substrate has had ≥4 weeks of dogfooding.

## The three coupled cuts

The pivot is one motion expressed in three places:

| Layer | Before (today) | After (proposed) |
|---|---|---|
| **Storage** | Files-first envelope with `_meta` wrappers, server-side CAS, merge semantics | Filesystem mirror — a file is a file, a folder is a folder, extension tells the type |
| **Type system** | `page` / `singleton` / `stream` / `collection` / `binary` as virtual leaf types layered over files | Files and folders, plus a handful of **typed views** (Taskboard, User, Skill, maybe Metric) |
| **Rendering** | MDX compiled client-side + 32 JSX built-in components + agent-uploadable components | HTML files served as-is via sandboxed iframes, plus server-rendered typed views |

The result: **"a git repo of files, served as-is, with typed views for a small set of structured kinds."** That's the entire product story.

## Motivation

The current system carries three pieces of custom DSL the agent ecosystem doesn't need:

1. **A storage envelope.** `{"_meta": {...}, "value": ...}` exists so the server can do CAS, merge, and shape detection. Git already does CAS via non-fast-forward checks. Shape detection was solving a problem we created by allowing multiple shapes per path.

2. **A type vocabulary.** Singletons, streams, collections, and binaries are virtual concepts on top of files. The audit (2026-05-13) found them functionally vestigial:
   - **Zero singletons or streams** in the dogfood seed.
   - **2 of 37** built-ins consume singletons (`Metric.tsx`, `Counter.tsx`).
   - **1 of 37** built-ins consumes streams (`Log.tsx`).
   - The 7-tool MCP surface (Cuts 5–8) doesn't reference any of these — it operates on file paths and git operations.
   - `internal/server/handlers_unified.go:1-22` and `ROADMAP.md` Cut 4 already mark them for deletion.

3. **A component library.** `<MetricCard>`, `<Chart>`, `<Activity>` and the other 32 built-ins are a custom DSL every consuming agent has to learn. The token cost of explaining the library is non-trivial; HTML is in every model's training data — agents speak it fluently with zero context.

Each cut individually is reasonable. Together they shed the accidental complexity and reduce AgentBoard to its essential proposition: a multi-agent git workspace with a readable view.

## Part 1 — Filesystem mirroring (storage layer)

### What changes

A leaf at path `notes/sales/q3.json` is exactly that: a JSON file at `notes/sales/q3.json` in the working tree, served with `Content-Type: application/json`. No envelope, no metadata wrapper, no virtual shape inference. The extension says the type; the content is the data.

### Design decisions

1. **Inline frontmatter, not sidecar files.** YAML frontmatter at the top of `.html` / `.md` files (Hugo / Jekyll / Astro convention). Sidecar `.meta` files double the entropy and break the "this file IS the artifact" property.
2. **Slashes, not dots, in paths.** Today's dotted paths (`sales.q3`) are a virtual-namespace artifact. After: `sales/q3.json`. Matches what git and the filesystem already understand.
3. **Reserved API prefix.** Content lives at the repo root URL space; the dashboard + typed-view API lives under a reserved prefix (probably `/_api/`). Path collisions need an explicit rule, not the current implicit content-type sniffing.
4. **`index.html` convention.** If `index.html` exists in a folder, serve it; else auto-render a GitHub-style directory listing. This is the discovery surface for folders.
5. **Operational state stays in SQLite.** Users, tokens, sessions, locks, teams, invitations, rate limits — these are server runtime state, not content. They belong with the binary, not the repo. Git history replaces the activity log for content authorship.
6. **No symlinks.** Standard hardening — symlinks escape the repo and confuse git.

### What this kills

- `internal/store/{singleton,collection,catalog}.go` and their handlers (already scheduled in ROADMAP Cut 4).
- The envelope CAS layer — git's non-fast-forward push is now the CAS.
- The shape-inference routing in `handlers_unified.go`.
- The `/api/sales.q3`-style dotted-path URLs.

## Part 2 — Kill singletons and streams (type layer)

### Singletons → just JSON files

| | Today | After |
|---|---|---|
| On disk | `{"_meta": {"version":3,...}, "value": {"rev": 42000}}` | `{"rev": 42000}` |
| On the wire | Same envelope | Same JSON, no envelope |
| CAS | `_meta.version` on the envelope | git non-fast-forward push |
| Read | `GET /api/sales.q3` | `GET /sales/q3.json` |

**Components affected:** `Metric.tsx` and `Counter.tsx` become typed views (or just plain HTML pages) that fetch the JSON file and read the number directly. The flash-on-change animation comes from the same SSE `page-updated` events as today.

**Migration cost:** drop the wrapper, rewrite two components, delete ~250 lines from `internal/store/singleton.go` and its handlers.

### Streams → directories of timestamped files

| | Today | After |
|---|---|---|
| On disk | One `.ndjson` file per stream, rotated at 5×100MB | A folder with one file per entry: `events/2026-05-13T14-22-09Z.json` |
| Append | `POST /api/x:append` with body line → server appends under lock | Write a new file in the folder — atomic at the OS level |
| Read tail | In-memory tail buffer + segment scan | Directory listing, sorted by filename |
| Live updates | SSE `data` event with the new line | SSE `directory-changed` event |

Benefits stack:

- **Atomic append is free** (new file write is atomic; no append-lock).
- **Clean git diffs** — each append is one new tree entry; no NDJSON line-level merge conflicts.
- **Clone-mode works** — `file://` directory listings ARE the tail.
- **Rotation goes away.** If history matters, a nightly job compacts old entries into a `events/2026-05.ndjson` archive companion alongside the live folder — but that's an optimization, not the model.

**Components affected:** `Log.tsx` becomes "list the folder, read entries in filename order, subscribe to directory-changed events."

**Migration cost:** rewrite one component, delete `internal/store/stream.go` (~358 lines), simplify the watcher.

### Scaling caveat

Directory-of-files starts to suck at millions of entries per stream (inode limits, slow listings, git tree bloat). AgentBoard's threat model is hundreds to low-thousands of entries per stream — comfortably within range. If a board ever hits that cliff, reintroduce a packed format then. Not now.

## Part 3 — HTML rendering (presentation layer)

### The two-surface split

The pivot is **not** "everything is HTML." It's a deliberate split:

| Surface | Format | Renderer | Purpose |
|---|---|---|---|
| **Structured data** | Typed files (JSON / YAML / specific schemas) | Built-in typed views in the binary | Queryable, tool-callable, editable affordances |
| **Expressive content** | Raw `.html` files | Pass-through with sandboxing | Specs, reports, explainers, mockups, brainstorms |

**First-class types under consideration:** Taskboard, User/Auth, Skill. Possibly Metric, Chart. Final list driven by dogfooding which kinds of content need queryable structure vs. read-once expression.

The split **must be named explicitly** in `spec.md` once committed. Without that, taskboards will start showing up as HTML pages and lose their queryable / editable / API-callable affordances — and the model degrades into "everything is files" with no structure to operate on.

### Why HTML beats MDX for expressive content

Agents already know HTML from training data. Markdown was the right choice when humans hand-edited specs; HTML is the right choice when agents generate read-once artifacts for humans. Token cost is 2–4× higher per generation, but the prompt-tokens-not-needed-to-explain-the-component-library savings flip the equation for AgentBoard's many-short-pages-by-many-agents pattern. Bonus: HTML renders without a runtime, opens in any browser, and ships zero proprietary syntax.

### Security architecture

**Threat model.**
- Trust the writer for the workspace itself.
- The system is **multi-user** (invitations, teams, member vs. admin) — a compromised or careless agent can attack other viewers via stored HTML.
- Hosted deploys are **multi-tenant** at the board level.
- HTML is **untrusted code** by the platform even if the writer is trusted — defense-in-depth catches accidents.

**Split-origin model (non-negotiable).** Without this, raw HTML at the dashboard origin is a stored-XSS factory.

- Dashboard at `agentboard.example.com` — the trusted origin (sessions, CSRF tokens, admin APIs).
- User HTML at `usercontent.agentboard.example.com` — the untrusted origin (opaque, cookie-isolated).
- Cookies do not cross subdomains. Dashboard sessions cannot be read or used from the user-content origin.

Industry precedent: GitHub uses `raw.githubusercontent.com`, Google Docs uses `docs.googleusercontent.com`, Cloudflare Pages uses `*.pages.dev`.

**Rendering rules.** User HTML embedded in the dashboard MUST be loaded via:

```html
<iframe
  src="https://usercontent.agentboard.example.com/<path>"
  sandbox="allow-scripts allow-popups"
  referrerpolicy="no-referrer">
</iframe>
```

The `sandbox` attribute MUST omit:
- `allow-same-origin` — keeps the iframe's origin opaque even on a related domain. Load-bearing flag.
- `allow-forms` — prevents form-based CSRF against the dashboard.
- `allow-top-navigation` — prevents the iframe from redirecting the parent.

MAY include `allow-scripts` (for interactivity) and `allow-popups` (for "open in new tab").

**CSP on the user-content origin:**

```
Content-Security-Policy:
  default-src 'self' 'unsafe-inline';
  script-src 'self' 'unsafe-inline' <CDN allowlist>;
  connect-src 'self';
  frame-ancestors https://agentboard.example.com;
```

- `connect-src 'self'` blocks exfiltration to attacker-controlled domains.
- `frame-ancestors` mitigates clickjacking from outside the dashboard.
- CDN allowlist (mermaid, d3, chartjs, fonts) decided during implementation.
- `'unsafe-inline'` is accepted — the whole origin is untrusted-by-design; sandbox + cookie isolation is the real boundary.

**Single binary, two origins.** The binary listens on one port and routes by HTTP `Host` header:
- `agentboard.example.com` → dashboard handlers, typed views, `/_api/`, MCP.
- `usercontent.agentboard.example.com` → raw HTML serving with appropriate `Content-Type` and CSP headers.

Local dev uses `localhost:3000` and `usercontent.localhost:3000` (browsers resolve `*.localhost` to `127.0.0.1` natively — no `/etc/hosts` hacks). Hosted deploys: Coolify / Traefik / Caddy terminates TLS on both hostnames pointing at the same upstream. One DNS record per board, or a wildcard.

### Defense-in-depth: agent sloppiness doesn't break security

If an agent ignores the "use relative URLs" rule and embeds absolute links to the dashboard origin in user HTML, the sandbox + CORS + REST-hygiene stack catches each attempt:

| Attempt | Why it fails |
|---|---|
| `<a href="https://agentboard.com/admin/...">` (in-iframe click) | Navigates iframe with opaque origin — no cookie access, broken UI but not exploitable |
| `<script src="https://agentboard.com/...">` | CSP blocks; even if allowed, JSON responses don't execute as JS, cross-origin script reading is blocked |
| `<form action="https://agentboard.com/...">` | `allow-forms` omitted from sandbox; even if allowed, CSRF middleware rejects missing `X-CSRF-Token` header |
| `<img src="https://agentboard.com/...">` | Fires authenticated GET — harmless because all mutating routes are POST/PUT/PATCH/DELETE (REST hygiene already in place) |
| `fetch('/api/...')` from iframe script | Opaque origin → cross-origin to dashboard → CORS rejects; cookies don't auto-attach without `credentials: 'include'` + CORS approval |

**The relative-URL rule is a portability requirement** (offline clone breaks without it), not a security requirement. The two failure modes are decoupled by the architecture.

### Residual risk: external link phishing

`<a href="https://attacker.com/phish" target="_blank">` can lead users to phishing sites. Structurally unsolvable — every platform with authored links has this risk (email, Slack, Notion, GitHub READMEs). Mitigations: auto-apply `rel="noopener noreferrer"`, optional "you're leaving AgentBoard" interstitial, content moderation policy. Acceptable residual risk.

## Three operating modes

The pivot must preserve all three:

1. **Server-running.** Single binary serves the dashboard, typed views, and user HTML. Full feature set: live updates, editing, auth, API.
2. **Cloned offline.** `git clone`, open `index.html` in a browser via `file://`. Read-only static archive. No live updates, no editing — just readable content.
3. **Migration.** `git clone` to any new host. Bits are durable; the binary is interchangeable. AgentBoard the company / project disappearing does not strand user data.

**Invariant:** *Every page must be readable via `file://` from a fresh clone with no server running.*

**Implication for typed views:** at write-time the binary generates a **static HTML companion** alongside the structured data file. `taskboards/q3.json` ships with `taskboards/q3.html` (rendered snapshot). The HTML companion regenerates on every structured-data write.

| Capability | Server-running | Cloned offline | Migration target |
|---|---|---|---|
| Read HTML pages | ✓ | ✓ | ✓ (after running binary) |
| Read typed views | ✓ (live) | ✓ (static companion) | ✓ |
| Edit typed data | ✓ | ✗ (read-only) | ✓ |
| Live page updates | ✓ | ✗ | ✓ |
| Auth-gated content | ✓ | n/a (you have all bits) | ✓ |
| Search | ✓ | optional (client-side index) | ✓ |
| MCP / API | ✓ | ✗ | ✓ |

## Agent-facing rules

To reflect in the AgentBoard skill once the pivot lands:

1. **Use relative URLs only.** No absolute paths to the host. Required for clone portability.
2. **External scripts come from the CSP allowlist.** Pulling random CDNs gets blocked; don't fight the policy.
3. **HTML is for read-once expressive content.** For taskboards, users, skills, metrics — use the typed APIs.
4. **Style with embedded CSS or by referencing `/assets/design-system.css`.** No shared component vocabulary; the design-system file gives agents an idiom to imitate.
5. **Folders auto-index.** Put `index.html` in a folder to control its landing page; otherwise a GitHub-style listing renders.

## What survives the pivot

- **Git substrate (Cuts 1–8).** HTML files commit / push / clone exactly like MDX files do today.
- **SSE broadcaster.** Page-changed events fire from the git post-receive hook; the dashboard renders them as "this changed — reload?" toasts or auto-refresh.
- **Auth** (tokens + sessions). Unchanged. The user-content origin uses no auth on read.
- **MCP tool surface (7 tools, Cuts 5–8).** Already operates on file paths and git operations — no virtual-type baggage to update.
- **Operational SQLite** for users, tokens, sessions, locks, teams, invitations, rate limits.

## What's lost vs. MDX/React

- **Shared component library.** No more `<MetricCard>`, `<Chart>`, `<Activity>`. Each page styles itself. Mitigation: `design-system.css` as a reference idiom. Drift will happen — accepted, because each writer gets the upside of styling per page.
- **Live data binding inside content.** MDX `<MetricCard value={data.foo}>` updates without reload. HTML pages get page-level reload-on-change. Typed views handle their own live updates server-side.
- **Rich MDX editor.** Goes away. Users edit HTML directly or via agent prompts. Acceptable per principle §15.
- **React SPA.** Replaced by server-rendered HTML (Go templates + htmx-style fragment swaps, or full page reloads on navigation). Smaller surface, fewer build steps, no npm at runtime.

## Migration plan (sketch)

A one-shot Go program reads the v0.13 substrate and writes the new layout:

- **Pages** (MDX) → `.html` files with YAML frontmatter intact, body translated. Most MDX is already valid HTML once custom components are rewritten to embedded CSS + simple HTML or to typed-view iframes.
- **Singletons** → unwrapped `.json` files at the path the singleton occupied. Drop the `_meta` and `value` keys.
- **Streams** → folder of timestamped files, one per existing NDJSON line.
- **Collections** → folder of `.json` files, one per item.
- **Binaries** → relocated to their new path, no transformation.

The dogfood instance (zero singletons/streams in the seed) is the easy case. Hosted boards with real data run the script against their own working trees.

## Open questions

1. **First-class types list.** Definitely Taskboard. Probably User/Auth, Skill. Maybe Metric, Chart. Final list by dogfooding.
2. **Discovery surface for the dashboard.** File tree (Notion / Drive-style)? Curated landing page (regenerated by an agent)? Tag / search index? Probably a combination.
3. **CDN allowlist for CSP.** Mermaid, d3, chartjs, fonts — which ones? Tighter = more secure, more papercuts.
4. **Reserved API prefix.** `/_api/`? `/.api/`? Bikeshed early so all routes settle on the same answer.
5. **Search.** Server-running indexes files; cloned-offline needs static client-side index (lunr.js), "ctrl-F is enough," or nothing. Decide.
6. **Static-companion regeneration cost.** Every typed-view write regenerates an HTML companion. Probably cheap (template render), but measure.
7. **What does the home page look like in a fresh repo?** Auto-generated landing page from the binary, or first-write content overwrites it?

## Decision criteria

Before this pivot is committed:

1. The git-substrate pivot (Cuts 1–8) has had **≥4 weeks** of dogfooding on the hosted instance.
2. The **token-cost-vs-richness tradeoff** has been measured on real agent runs.
3. The **two-surface split** has been named explicitly in `spec.md`.
4. The **split-origin infrastructure** has a concrete deployment plan in `HOSTING.md` (including local-dev `usercontent.localhost`).
5. A **prototype typed view** (Taskboard) has been built alongside the existing MDX surface and dogfooded for ≥2 weeks to validate the model.

## Relationship to other docs

- **`spec.md`** — current source of truth (git-substrate model). This doc layers on top; the spec is not yet updated.
- **`CORE_GUIDELINES.md` §5** — non-technical-team framing. Honored by the dashboard's discovery surface (curated landing page + readable HTML rendered in iframes), not by raw file-tree exposure.
- **`CORE_GUIDELINES.md` §14** — write dispatch collapses, pages are the only authoring shape. This pivot completes §14: pages become files, files are the only authoring shape.
- **`CORE_GUIDELINES.md` §15** — workspace teaches the agent. The pivot *strengthens* §15: HTML + filesystem semantics are what agents already know; the skill shrinks, prior knowledge does more work.
- **`ROADMAP.md` Cut 4** — already schedules singleton / collection / stream removal. This pivot promotes Cut 4 from "later" to load-bearing.
- **`spec-plugins.md`** — the JSX component contract. The pivot retires it; on commit, the doc moves to `docs/archive/`.
- **`HOSTING.md`** — must be updated with the split-origin topology before the pivot lands.
- **`seams_to_watch.md`** — split-origin user-content rendering and "trust the writer" belong here as conscious deferrals if the pivot moves forward.

## Provisional verdict

The three cuts are coherent because they're really one motion: collapse the storage envelope, collapse the type vocabulary, collapse the rendering DSL. The result is a simpler product (a git server with a readable view), a simpler agent surface (filesystem semantics, HTML, a few typed APIs), and a stronger portability story (clone-mode is parity with server-mode for everything except live editing).

The pivot lands cleanly if:

1. The **two-surface split** is named and enforced. Without it, structure rots into free-form HTML.
2. The **split-origin infrastructure** is in place on day one. Without it, the dashboard becomes a stored-XSS factory the moment the first agent writes an HTML file.
3. The **directory-as-stream** pattern is good enough at AgentBoard's scale. It is, within current bounds.
4. The **migration script** is solid. The dogfood instance is near-empty of legacy data; hosted boards run the script on their own working trees.

If those four hold, this is a real direction. If any wobble, defer.
