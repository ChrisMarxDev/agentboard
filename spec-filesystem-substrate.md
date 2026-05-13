---
status: planning — no implementation yet
created: 2026-05-13
follows: cuts 1–8 (git substrate)
relates_to: spec.md, CORE_GUIDELINES.md §5, seams_to_watch.md
---

# HTML-Substrate Pivot — Planning Doc

> **This is a planning doc, not a committed direction.** The git-substrate pivot (Cuts 1–8) just landed on `main`. This explores the *next* possible move: replace the MDX + React component surface with a "repository + typed views + free-form HTML" model. Implementation does not begin until the open questions at the bottom are resolved and the git substrate has had ~4 weeks of dogfooding.

## Motivation

The current model treats MDX + a JSX component library as the readable surface. Every consuming agent has to learn that component library — which means:

- **Prompt tokens** to explain `<MetricCard>`, `<Chart>`, `<Activity>`, etc.
- **Documentation** that drifts every time a component changes shape.
- **Skill weight** carrying conventions specific to AgentBoard.
- **Custom DSL tax** — we are essentially asking agents to learn our proprietary markup when they already know HTML natively from training data.

The pivot bets that **leaning on HTML — which agents produce fluently with zero context — beats maintaining a bespoke component vocabulary**.

The product also becomes more generic: "a git repo of files the binary serves and renders" is a smaller, clearer story than "a custom store with a custom component runtime."

## The two-surface split

The pivot is **not** "everything is HTML." It's a deliberate split:

| Surface | Format | Renderer | Purpose |
|---|---|---|---|
| **Structured data** | Typed files (JSON / YAML / specific schemas) | Built-in typed views in the binary | Queryable, tool-callable, editable affordances |
| **Expressive content** | Raw `.html` files | Pass-through with sandboxing | Specs, reports, explainers, mockups, brainstorms |

**First-class types under consideration:** Taskboard, User/Auth, Skill. Possibly Metric, Chart. Final list driven by dogfooding which kinds of content need queryable structure vs. read-once expression.

The split **must be named explicitly** in `spec.md` once committed. Without that, taskboards will start showing up as HTML pages and lose their queryable / editable / API-callable affordances — and the model degrades back into "everything is files" with no structure to operate on.

## Three operating modes

The pivot must preserve all three:

1. **Server-running.** Single binary serves the dashboard, typed views, and user HTML. Full feature set: live updates, editing, auth, API.
2. **Cloned offline.** `git clone`, open `index.html` in a browser via `file://`. Read-only static archive. No live updates, no editing — just readable content.
3. **Migration.** `git clone` to any new host. Bits are durable; the binary is interchangeable. AgentBoard the company / project disappearing does not strand user data.

**Invariant:** *Every page must be readable via `file://` from a fresh clone with no server running.*

This locks portability into the on-disk format and forces decisions that would otherwise drift (relative URLs, no server-only links in content).

## Security architecture

### Threat model

- **Trust the writer** for the workspace itself. Members and the agents they run are trusted not to be intentionally malicious.
- But the system is **multi-user** (invitations, teams, member vs. admin roles), so a compromised or sloppy agent can attack *other* viewers via stored HTML.
- Hosted deploys are **multi-tenant** at the board level; cross-board attack containment is required.
- HTML is **untrusted code** by the platform even if the writer is trusted — defense-in-depth catches accidents.

### Split-origin model

The non-negotiable mitigation. Without this, raw HTML at the dashboard origin is a stored-XSS factory.

- Dashboard at `agentboard.example.com` (the trusted origin — sessions, CSRF tokens, admin APIs live here).
- User HTML served from `usercontent.agentboard.example.com` (the untrusted origin — opaque, cookie-isolated).
- Cookies do not cross subdomains by default. Sessions on the dashboard origin cannot be read or used from the user-content origin.

Industry precedent: GitHub uses `raw.githubusercontent.com`. Google Docs uses `docs.googleusercontent.com`. Cloudflare Pages uses `*.pages.dev`. The pattern is well-understood.

### Rendering rules

User HTML embedded in the dashboard MUST be loaded via:

```html
<iframe
  src="https://usercontent.agentboard.example.com/<path>"
  sandbox="allow-scripts allow-popups"
  referrerpolicy="no-referrer">
</iframe>
```

The `sandbox` attribute MUST omit:

- `allow-same-origin` — keeps the iframe's origin opaque even if the URL is on a related domain. This is the load-bearing flag.
- `allow-forms` — prevents form-based CSRF attempts against the dashboard origin.
- `allow-top-navigation` — prevents the iframe from redirecting the parent window.

The `sandbox` attribute MAY include:

- `allow-scripts` — required for interactivity (sliders, copy-to-prompt buttons, etc.).
- `allow-popups` — for "open in new tab" UX on external links.

### CSP on the user-content origin

```
Content-Security-Policy:
  default-src 'self' 'unsafe-inline';
  script-src 'self' 'unsafe-inline' <allowlisted CDNs>;
  connect-src 'self';
  frame-ancestors https://agentboard.example.com;
```

Notes:

- `connect-src 'self'` blocks data exfiltration to attacker-controlled domains.
- `frame-ancestors` restricts who can iframe user content, mitigating clickjacking.
- CDN allowlist enables common interactive libs (mermaid, d3, chartjs, fonts) — decide the list during implementation, not now.
- `'unsafe-inline'` is accepted because the whole origin is untrusted-by-design; the sandbox + cookie-isolation is the real boundary.

### Single binary, two origins

The binary listens on one port and routes by HTTP `Host` header:

- `agentboard.example.com` → dashboard handlers, typed views, API, MCP.
- `usercontent.agentboard.example.com` → raw `.html` file serving with `Content-Type: text/html` and CSP headers.

For **local development**: `localhost:3000` (dashboard) and `usercontent.localhost:3000` (user content). Modern browsers resolve `*.localhost` to `127.0.0.1` natively — no `/etc/hosts` edits needed.

For **hosted deploys**: Coolify / Traefik / Caddy terminates TLS on both hostnames pointing at the same upstream port. One DNS record per board, or a wildcard like `*.agentboard.example.com`. `HOSTING.md` to be updated with the topology when the pivot lands.

### Defense-in-depth: agent sloppiness does not break security

If an agent ignores the "use relative URLs" rule and embeds absolute links to the dashboard origin in user HTML, the sandbox + CORS + REST hygiene stack catches each attempt:

| Attempt | Why it fails |
|---|---|
| `<a href="https://agentboard.com/admin/...">` (in-iframe click) | Navigates iframe with opaque origin — no cookie access, broken UI but not exploitable |
| `<script src="https://agentboard.com/...">` | CSP blocks; even if allowed, JSON responses don't execute as JS, cross-origin script reading is blocked |
| `<form action="https://agentboard.com/...">` | `allow-forms` omitted from sandbox; even if allowed, CSRF middleware rejects missing `X-CSRF-Token` header |
| `<img src="https://agentboard.com/...">` | Fires authenticated GET — harmless because all mutating routes are POST/PUT/PATCH/DELETE (REST hygiene already in place) |
| `fetch('/api/...')` from iframe script | Opaque origin → cross-origin to dashboard → CORS rejects; cookies don't auto-attach without `credentials: 'include'` + CORS approval |

**Conclusion:** the relative-URL rule is a **portability requirement** (offline clone breaks without it), not a **security requirement**. The two failure modes are decoupled by the architecture.

### Residual risk: external link phishing

`<a href="https://attacker.com/phish" target="_blank">` can lead users to phishing sites. This is structurally unsolvable — every platform that allows authored links has this risk (email, Slack, Notion, GitHub READMEs).

Mitigations:

- Auto-apply `rel="noopener noreferrer"` to all external links at render time.
- Optional interstitial: "You are leaving AgentBoard. Continue?".
- Content moderation policy — same as Slack / Notion / email.

Acceptable residual risk for the threat model.

## Agent-facing rules

To be reflected in the AgentBoard skill once the pivot lands:

1. **Use relative URLs only.** No absolute paths to the host. Required for clone portability; not load-bearing for security but load-bearing for the §15 cloneable-substrate property.
2. **External scripts come from the allowlist.** Pulling random CDNs will be blocked by CSP; fight that and you'll just produce broken pages.
3. **HTML is for read-once expressive content.** For taskboards, users, skills, metrics — use the typed APIs. HTML is not the place to store structured workspace state.
4. **Style with embedded CSS or by referencing `design-system.css`.** No shared component vocabulary — each page styles itself, but a single design-system reference file gives agents an idiom to imitate.

## What survives the pivot

- **Git substrate** (Cuts 1–8). HTML files commit / push / clone exactly like MDX files do today. No change to storage.
- **SSE broadcaster.** Page-changed events fire from the git post-receive hook as before; the dashboard handles them as "this page was updated — reload?" toasts or auto-refresh.
- **Auth** (tokens + sessions). Unchanged. The user-content origin uses no auth on read (the bits are in git anyway, and any access control is enforced upstream of serving).
- **MCP tool surface** (the 7-tool set from Cuts 5–8). HTML files flow through the same propose path.
- **Live page-level updates.** The user pointed out that listening for changes on the current page and reloading on change is fine — we don't expect changes every second.

## What's lost vs. MDX/React

- **Shared component library.** No more `<MetricCard>`, `<Chart>`, `<Activity>`. Each page styles itself. Mitigation: a single `design-system.html` reference file agents imitate. Drift will happen — accepted, because the writer (humans pointed out) gets the upside of styling per page.
- **Live data binding inside content.** An MDX page with a live `<MetricCard value={data.foo}>` can update without page reload. HTML pages get page-level reload-on-change instead. Acceptable for AgentBoard's update cadence; typed views handle their own live updates server-side.
- **Rich MDX editor.** Goes away. Users edit HTML directly or via agent prompts. Acceptable per principle §15 (workspace teaches the agent — humans don't hand-edit much).
- **React SPA.** Replaced by server-rendered HTML (Go templates + htmx-style fragment swaps, or just full page reloads on navigation). Smaller surface, fewer build steps, no npm at runtime.

## Three modes — what each mode loses

| Capability | Server-running | Cloned offline | Migration target |
|---|---|---|---|
| Read HTML pages | ✓ | ✓ | ✓ (after running binary) |
| Read typed views | ✓ (live) | ✓ (static companion) | ✓ |
| Edit typed data | ✓ | ✗ (read-only) | ✓ (after running binary) |
| Live page updates | ✓ | ✗ | ✓ |
| Auth-gated content | ✓ | n/a (you have all bits) | ✓ |
| Search | ✓ | optional (client-side index) | ✓ |
| MCP / API | ✓ | ✗ | ✓ |

**Implication for typed views:** at write-time the binary should generate a **static HTML companion** alongside the structured data file, so the typed view is readable in cloned-offline mode. E.g. `taskboard.json` + `taskboard.html` (rendered snapshot). The HTML companion regenerates on every structured-data write.

## Open questions

These must be resolved before the pivot is committed to:

1. **First-class types list.** Definitely Taskboard. Probably User/Auth. Maybe Skill, Metric, Chart. Decide by which types' use-frequency justifies a typed renderer vs. an HTML one-off.
2. **Migration path for existing MDX content.** Convert all MDX → HTML on a flag day? Render MDX as a legacy surface alongside HTML? Mass-convert via an agent run? The dogfood instance has real MDX pages today; the answer affects everyone.
3. **Discovery surface for the dashboard.** With no React SPA and no MDX home page, what does a user see when they open `agentboard.example.com`? Options: file tree like GitHub (PMs use Notion/Drive — file trees are fine), curated landing page (still HTML, regenerated by an agent), tag/search index. Probably some combination.
4. **CDN allowlist for CSP.** Which interactive libraries (mermaid, d3, chartjs, recharts-equivalent, fonts) get included in `script-src`? Smaller list = tighter security but more "this doesn't work in AgentBoard" papercuts.
5. **Search.** Server-running mode can index files. Cloned-offline mode needs either no search, a static client-side index (lunr.js), or "ctrl-F is enough." Decide.
6. **Static-companion regeneration cost.** If every typed-view write triggers a re-render of an HTML companion, does that scale? Probably yes (cheap template render), but measure.

## Decision criteria

Before this pivot is committed:

1. The git-substrate pivot (Cuts 1–8) has had **at least 4 weeks** of dogfooding on the hosted instance.
2. The **token-cost-vs-richness tradeoff** has been measured on real agent runs — how many tokens does the MDX component library currently consume in agent context, and what does HTML actually save in practice?
3. The **two-surface split** has been consciously named in `spec.md` — not introduced as a side-effect of "remove MDX."
4. The **split-origin infrastructure** has a concrete deployment plan in `HOSTING.md`, including the local-dev `usercontent.localhost` setup.
5. A **prototype typed view** (Taskboard is the obvious candidate) has been built alongside the existing MDX surface and dogfooded for two weeks to validate the model before deprecating anything.

## Relationship to other docs

- **`spec.md`** — current source of truth, describes the git-substrate model. This doc is a *possible future* layered on top; the spec is not yet updated.
- **`CORE_GUIDELINES.md` §5** — non-technical-team framing. The repo metaphor was challenged here and corrected: Notion / Drive are file-tree UIs that work for non-technical users. What matters is the chrome around the files, not the underlying metaphor. The dashboard's *discovery surface* is what carries §5, not the storage model.
- **`CORE_GUIDELINES.md` §15** — workspace teaches the agent. This pivot *strengthens* §15: HTML is closer to what agents already know than MDX-with-custom-components is. The skill shrinks; the agent's prior knowledge does more work.
- **`seams_to_watch.md`** — split-origin user-content rendering and the "trust the writer" model belong here as conscious deferrals if the pivot moves forward without all mitigations on day one.
- **`spec-plugins.md`** — the component contract. This pivot effectively retires it (no more JSX components). When the pivot is committed, that doc moves to `docs/archive/` with a redirect note.

## Provisional verdict

The motivation is real (custom-DSL tax, token bloat, skill weight). The architecture is workable (split-origin + sandbox + CSP is well-trodden). The clone-portability story is *strengthened*, not weakened. The §5 audience concern is answerable via the dashboard's discovery surface, not the storage format.

The two things that would kill this pivot if hand-waved:

1. **The two-surface split staying ambiguous.** If everything becomes HTML, structured affordances vanish and the product gets worse, not more generic.
2. **The split-origin infrastructure being treated as "we'll add it later."** Day one needs the subdomain in place; otherwise the dashboard becomes a stored-XSS factory the moment the first agent writes an HTML file.

If both are nailed down, this is a real direction. If either is loose, defer.
