# AgentBoard — Docs Platform Spec (idea collection)

> **Status**: Brainstorm, not a plan. Maps the docs-platform feature space (Docusaurus, Mintlify, Fumadocs, Starlight, Nextra, GitBook, Vocs) onto AgentBoard and asks: which of these are natural, which are cheap, which are the differentiator, and which should we deliberately skip? Ranking happens later; this file's job is to be the net.

---

## 1. Motivation

AgentBoard is 80% a docs platform already. The core model — MDX pages in `content/`, a component catalog, hot reload, client-side compile, files, theming — is the same shape Docusaurus/Nextra built a company on. What's missing is the set of small conveniences that make a site *feel* like a docs site: sidebar categories, per-page TOC, search, callouts, last-updated, "Copy" on code blocks, versioning.

Adding them is cheap *and* gives us a genuine second use case: one binary serves **agent dashboards** and **agent-authored product docs** from the same `content/` folder. The boundary between "live dashboard" and "static docs" blurs in the direction we want — docs that contain live data are strictly more useful than docs that don't.

---

## 2. What AgentBoard-as-docs uniquely gets right

Lead with these when we write marketing copy. These are things **other docs tools structurally can't do** without rebuilding on our stack:

1. **Live data inline.** A Metric doc shows the CURRENT production value beside the description. A Mermaid diagram of your architecture reflects the state of `dev.architecture.flow` right now. Docs are always fresh because they read the KV store.
2. **AI-authored, AI-updated.** Claude writes the page via MCP and updates it in place when the code lands. No PR workflow; no staleness.
3. **One stack, two audiences.** The same `content/` folder serves internal dashboards (agent operators) and external docs (product users). Pages opt in/out of public visibility.
4. **Component catalog *is* self-documenting.** `/api/components` returns every built-in with `meta.props` and descriptions. Build a reference page from that — zero duplication.
5. **Context handoff to other agents.** The Grab feature (see `spec-grab.md`) lets readers pick doc sections and hand them to a different agent as a single prompt. That's a unique value proposition — docs as compile-targets for agent context.
6. **Error visibility built in.** The beacon feature surfaces broken Mermaid diagrams, missing images, bad data references on the homepage. Most docs platforms discover these only via user reports.
7. **History retention.** Page edits leave a trail in `data_history`-equivalent. "Why did this API note change last week?" is answerable.

---

## 3. Non-goals

- **We don't become Docusaurus.** Feature parity is not the target; feature *taste* is. Many docs features assume a human author with a git workflow. We optimize for an AI author with an MCP tool-call.
- **No GitHub-based edit flow.** The "Edit this page on GitHub" affordance doesn't fit an agent-authored product. The equivalent is "Ask Claude to fix this page" → which is already our default.
- **No real-time collaborative editing.** Single-user local model for now. Hosted multi-user comes with auth (Phase 3).
- **No third-party analytics integration.** Page-view dashboards, feedback aggregation — build them *into* AgentBoard using the same primitives (data keys + components), don't call out to Plausible.
- **No full i18n system in Phase 1.** Adding locale routing + translated sidebar structure is a Phase 3 effort.
- **No WYSIWYG.** Claude is the editor; MDX in the file is the truth.

---

## 4. Feature ideas, grouped

Each entry is tagged:
- **🎯** — natural fit, cheap, would ship in Phase 1
- **🧪** — worth considering, needs design
- **🚫** — explicit skip (fits other tools, not us)

### 4.1 Navigation & structure

| Idea | Tag | Notes |
| --- | --- | --- |
| **Nested sidebar** (folders under `content/` become categories) | 🎯 | Already have folder routing; just render nested structure in Nav. |
| **Breadcrumbs** on nested pages | 🎯 | Derive from URL path. ~30 lines of React. |
| **Prev / Next links** at the bottom of every doc | 🎯 | Ordering from a `sidebar_position` frontmatter field or filename prefix. |
| **Auto-generated category index pages** | 🎯 | `content/api/` with no `index.md` → auto-render a cards-grid of children. |
| **Sidebar pinning / collapse state per user** | 🧪 | `localStorage`. Pairs with the existing nav-collapse. |
| **Per-page TOC** ("On this page") derived from h2/h3 | 🎯 | MDX walk at compile time → emit an array; Nav renders it as a right rail. |
| **Section anchors + hover-reveal `#` links** | 🎯 | Auto-generated id on every heading. Pairs with Grab's card-ID work. |
| **Version switcher** (v1 / v2 / latest) | 🧪 | Folder-level — `content/v1/…`, `content/v2/…` with a dropdown. |
| **Version diff view** (what changed between v1 and v2) | 🧪 | Reads `data_history`-style diff between two page versions. |

### 4.2 Reading experience

| Idea | Tag | Notes |
| --- | --- | --- |
| **Light / dark / auto theme switch** | ✅ | Already shipped. |
| **Font size controls** (reader preference) | 🧪 | `localStorage`; mostly a win for long docs. |
| **Reading-time estimate** ("5 min read") | 🎯 | Word count / 200. Frontmatter auto-computed. |
| **Last-updated timestamp** | 🎯 | File mtime on disk (or git-log if we want to get fancy). |
| **"Edited by Claude · 3 min ago"** attribution | 🎯 | Agents self-identify when writing via MCP. Pair with `updated_by` we already record. |
| **Smooth anchor scroll + scroll-spy TOC** | 🎯 | Standard docs behavior. |
| **Print-friendly CSS** (`@media print` block) | 🎯 | Small CSS file, no JS. |
| **Keyboard navigation** — `j`/`k` next/prev section, `?` help overlay | 🧪 | Pairs with existing `?` → shortcuts help. |

### 4.3 Content / MDX enhancements

These are **new built-in components**, registered like Deck/Card/Mermaid.

| Component | Tag | What it does |
| --- | --- | --- |
| **`<Note>` / `<Warning>` / `<Tip>` / `<Danger>`** (admonitions) | 🎯 | One component `<Callout type="warning">…`. Core docs convention. |
| **`<Tabs>` + `<Tab>`** | 🎯 | OS-specific instructions, multi-language code samples, API-spec/response variants. |
| **`<Details>` / `<Summary>`** (collapsible) | 🎯 | Long supplementary content without scroll tax. |
| **`<Hero>`** for landing pages (title + subtitle + CTA buttons) | 🎯 | Cheap; makes `/` or `/docs/introduction` look like a real product page. |
| **`<FeatureGrid>`** (icon + title + one-liner + link) | 🎯 | Docusaurus home-page component. |
| **`<ComparisonTable>`** (this vs that, col-aligned) | 🧪 | Could be the existing `<Table>` with opinionated styling. |
| **`<Changelog>`** | 🎯 | Reads an array data key; renders dated entries. Reuses `<Log>` shape. |
| **`<Roadmap>`** | ✅ | Already shipped — `<Kanban>` with `status` groupBy. Document the pattern. |
| **`<Glossary>`** (term → definition dict) | 🎯 | New component; reads a `{ term: def }` map. |
| **`<API>`** (renders one endpoint from OpenAPI-shaped data) | 🧪 | Could ingest an OpenAPI doc and render endpoints. Agents love OpenAPI. |
| **`<Video>`** (YouTube / Loom / MP4 embed) | 🎯 | Wraps an iframe or `<video>` with responsive CSS. |
| **`<Sandbox>`** (CodeSandbox / StackBlitz iframe) | 🚫 | Third-party iframe heavy; niche. Defer. |
| **Math rendering** (KaTeX) | 🚫 | Real cost (KaTeX is ~280 KB); narrow audience. User-component if needed. |
| **Copy-to-clipboard button on `<Code>`** | 🎯 | Small addition to existing Code component. Universally expected in 2026. |
| **Line numbers + line highlighting in `<Code>`** | 🎯 | Prism already tokenizes; just render line numbers. |
| **Image lightbox / zoom** on `<Image>` click | 🧪 | Standard docs behavior; small library or DIY. |
| **Image captions** | 🎯 | Add `caption` prop to `<Image>`. |
| **Footnotes** (MD extension) | 🧪 | MDX plugin; moderate effort. |
| **Task-list checkboxes** in plain MD (`- [x]`) | 🎯 | Already works via markdown; verify + add a checkbox style. |
| **Definition lists** (`<dl>`) | 🎯 | Style the existing MDX rendering. |

### 4.4 Search

| Idea | Tag | Notes |
| --- | --- | --- |
| **Cmd+K palette with full-text search** across pages | 🎯 | Build an index of `page_path → (title, headings, prose)` at page-scan time. Fuzzy-match client-side. Pairs naturally with Grab's proposed palette. |
| **Search result previews** (matching snippet with highlights) | 🎯 | Straight extension of above. |
| **Recent pages dropdown** in the palette | 🎯 | `localStorage` track of last 10 visited. |
| **Tags per page** (frontmatter `tags: [a, b]`), tag-browse page | 🧪 | Non-trivial UX; worth only if the corpus grows big. |
| **Search-query analytics** ("what are users searching for?") | 🧪 | Reads into a data key; itself becomes a dashboard. |
| **Algolia / Typesense integration** | 🚫 | External dep, hosted-only. Local Lunr or custom wins for our shape. |

### 4.5 Metadata & SEO

| Idea | Tag | Notes |
| --- | --- | --- |
| **Per-page `<title>` from frontmatter** | 🎯 | Currently everything is "AgentBoard". Easy win. |
| **Per-page meta description** | 🎯 | Frontmatter `description:`; emitted in `<head>`. |
| **Open Graph / Twitter Card** (`og:title`, `og:image`, etc.) | 🎯 | Frontmatter-driven. |
| **Auto-generated `og:image`** per page | 🧪 | Render the hero card to PNG at request time. Fun but heavy. Skip. |
| **Sitemap.xml** (auto, lists all pages) | 🎯 | ~30 lines. Essential for SEO. |
| **Robots.txt** | 🎯 | Template file served from embedded assets. |
| **Canonical URLs** | 🎯 | Frontmatter `canonical:`. |
| **RSS / Atom feeds** for changelog / blog-like sections | 🧪 | Opt-in per folder (`rss: true` in a folder metadata file). |
| **Structured data (schema.org)** for docs articles | 🧪 | JSON-LD in `<head>` — nice for SEO but niche. |

### 4.6 Customization / branding

| Idea | Tag | Notes |
| --- | --- | --- |
| **Custom logo** (swap the "AgentBoard" title) | 🎯 | `agentboard.yaml: logo: files/logo.svg`. |
| **Custom colors** via CSS custom properties | ✅ | Already possible via `:root` overrides; document it. |
| **Custom fonts** | 🧪 | `fonts:` config pointing at `files/*.woff2`; emit `@font-face`. |
| **Favicon** | 🎯 | `agentboard.yaml: favicon:`. |
| **Header banner for announcements** ("v2 is out") | 🎯 | Reads from a data key; dismissible with `localStorage` flag. |
| **Footer** with links, copyright, social | 🎯 | Config-driven; opt-in. |
| **Hero section** on home page | 🎯 | See §4.3 — `<Hero>` component. |
| **Dismissible first-run tour** | 🧪 | One-shot overlay for new readers. |

### 4.7 Collaboration / feedback

| Idea | Tag | Notes |
| --- | --- | --- |
| **"Was this helpful?" thumbs up/down widget** | 🎯 | Writes to `feedback.<page>` data key. Becomes a dashboard. |
| **"Report a problem"** | 🎯 | Triggers the error beacon with a human-authored note. |
| **"Ask Claude about this page"** link | 🎯 | Deep-links to Claude Desktop with the current page's MDX as context. Pairs with the Grab spec. |
| **Comments** (Giscus / Disqus / Utterances) | 🚫 | Third-party auth dependency. Not in keeping. |
| **Edit-on-GitHub link** | 🚫 | Wrong author model for us. |
| **Suggest-a-change** writes to an agent via MCP | 🧪 | "Reader says: this section confused me → Claude rewrites → page updates." Feels very AgentBoard. |

### 4.8 Authoring / publishing

| Idea | Tag | Notes |
| --- | --- | --- |
| **Dead-link detector** (crawl internal links, report broken ones) | 🎯 | Periodic background job; results go to `_system.errors.dead_links`. Surfaces on home page via the existing `<Errors>`. |
| **Missing-image detector** | 🎯 | Same shape; checks `<Image src>` resolves. |
| **Linting: heading hierarchy** (no h2 without h1, no skipping h2→h4) | 🧪 | Compile-time check; warnings land in the error beacon buffer. |
| **Alt-text lint** for images | 🧪 | Same. |
| **Spell check** | 🚫 | Human-author concern. Claude rarely misspells. |
| **Draft state** (`draft: true` in frontmatter hides from public) | 🎯 | Pairs with the public/private split below. |
| **Scheduled publish** (`publish_at: 2026-05-01`) | 🧪 | Page visible after the date. Needs a time trigger. |
| **Public vs private pages** | 🎯 | Frontmatter `public: true`. Anonymous GET serves only public pages; authed serves everything. Pairs with the auth-token work already shipped. |

### 4.9 Analytics (self-hosted, via data keys)

These all become dashboards on top of the same primitive: page-view events written to a data key.

| Idea | Tag | Notes |
| --- | --- | --- |
| **Page view counter** | 🎯 | Client beacon on page load writes to `analytics.views.<path>`. |
| **Scroll depth** (did readers reach the bottom?) | 🧪 | Useful for long docs; one metric per page. |
| **Search-query log** | 🧪 | See §4.4. |
| **Most-viewed pages** dashboard | 🎯 | `<Chart>` over `analytics.views.*`. |
| **Broken-link report** dashboard | 🎯 | `<Table>` over `_system.errors.dead_links`. |
| **Feedback-rollup** dashboard | 🎯 | `<Chart>` over `feedback.*`. |

### 4.10 Publishing / distribution

| Idea | Tag | Notes |
| --- | --- | --- |
| **Static export** (`agentboard export ./dist/`) | 🎯 | Snapshot every page to HTML + assets → deploy anywhere. Data-bound components get baked-in values at export time (plus a "data is stale" banner if we want). |
| **Incremental static rebuild** on page write | 🧪 | Only rebuild changed pages. Nice for large corpora. |
| **Edge-ready output** (clean URLs, no server) | 🎯 | Static export produces `page.html` + directory-style URLs. |
| **Password-protected sections** | 🚫 | Auth-gated; that's a hosted-mode concern, not a docs concern. |

---

## 5. Things we explicitly skip (and why)

- **WYSIWYG editor** — Claude writes MDX; the file is the source. A visual editor would introduce a second authoring path with divergent behavior.
- **Git-based edit workflow** — pull requests are the wrong primitive when an agent can just land the change. "Ask Claude to edit" replaces "Edit on GitHub".
- **Third-party auth (Auth0, Clerk)** — we have a simple shared-token story; hosted multi-user goes through our own design, not a vendor's.
- **Algolia DocSearch** — local full-text search with Cmd+K covers ~everything at zero runtime cost.
- **Multi-language i18n** — defer until the product has users in multiple languages. Adds surface area and translation workflows we'd otherwise own.
- **Comments sections** — invite spam; wrong UX primitive for "ask a question about this doc". The AgentBoard answer is "ask Claude about this page".
- **Team / author pages** — non-technical audience, no multi-user model, doesn't compose with our principles.
- **Real-time collaborative editing** — one author at a time (Claude) by design.

---

## 6. The 🎯 Phase-1 shortlist

If we ship a docs-mode release, the minimum that makes AgentBoard feel like a first-class docs platform:

### Navigation
- Nested sidebar from `content/` folder structure
- Breadcrumbs + Prev/Next + per-page TOC (right rail)
- Auto-generated category index pages
- Section anchors with hover-reveal `#` (pairs with Grab's card IDs)

### Content components (new built-ins)
- `<Callout type="note|warning|tip|danger">`
- `<Tabs>` / `<Tab>`
- `<Details>` / `<Summary>`
- `<Hero>`, `<FeatureGrid>`, `<Glossary>`, `<Changelog>`
- Copy button + line numbers on `<Code>`
- `caption` prop on `<Image>`

### Metadata
- `<title>`, meta description, OG/Twitter cards (frontmatter-driven)
- Sitemap.xml
- Last-updated timestamp per page
- Reading-time estimate

### Search
- Cmd+K palette, local full-text index, snippet previews, recent pages

### Authoring hygiene (piggybacks on the existing error beacon)
- Dead-link detector → writes to the errors buffer → shows in `<Errors />`
- Missing-image detector → same
- Heading-hierarchy lint → same

### Publishing
- Static export (`agentboard export`)

### Customization
- Config-driven logo, favicon, header banner, footer

Everything above stays consistent with CORE_GUIDELINES: pure-Go backend (no new server deps), zero new auth surface, AI-first authoring stays the default. Several items reuse the error-beacon primitive (dead-link, missing-image, heading-hierarchy) — same sink, more producers.

---

## 7. Big-swing ideas worth a longer look

### 7.1 **Docs pages that ARE the dashboard**

Collapse the boundary. An API reference page shows the endpoint description AND a live request log for that endpoint AND the current value of a test metric — all side by side. Readers don't context-switch between "docs" and "dashboard"; they're the same page.

Realized via: normal components on a normal page. Already possible. Becomes more valuable with docs-shaped conventions layered on top.

### 7.2 **"Ask Claude about this page" button**

Every page has a button. Click it → Claude Desktop (or the agent's CLI) opens with the current page's MDX source + referenced data values as the first context message. Human types a question; Claude answers with that grounding.

Goes beyond any existing docs tool because most tools lose the data context on the handoff.

Pairs directly with Grab (see `spec-grab.md` §8 E2).

### 7.3 **Auto-maintained API reference**

`agentboard_generate_api_docs({openapi_url})` — MCP tool that takes an OpenAPI spec, emits a set of pages under `content/api/`, with an endpoint-per-page using a consistent `<API>` component. When the spec updates, Claude re-runs and the pages re-render.

Docs that track the API without humans in the loop.

### 7.4 **Docs versioning from the data-store history**

`data_history` already records every value a data key has ever held. If pages are data too (stored in SQLite alongside the KV, which they currently *aren't* — they're on disk), we'd inherit a full audit trail for free. Consider moving pages into the data store (or adding a parallel `page_history`). Lets the version-switcher, diff-view, and "show what changed" all be queries, not file-system archaeology.

Large architectural decision — worth a separate spec if we pursue it.

### 7.5 **Reader-driven editing via agent suggestions**

Thumbs-down on a paragraph → prompt for "what's wrong?" → POSTs to a data key → `bundles.history`-style log of reader feedback → Claude watches that log and proposes page edits. Feedback becomes training data for the doc maintainer agent.

---

## 8. Open questions

### Q1 — content mode
Do docs pages live in the same `content/` folder as dashboards, or a separate `docs/`? I'd say same folder, with frontmatter distinguishing purpose. Composing the two is the whole point.

### Q2 — public vs private default
Docs want anonymous reads. Dashboards assume trusted readers. What's the default for a new page? Probably `public: false` until explicitly opted-in — fail safe, fail closed.

### Q3 — do we need a "docs mode" project type?
An `agentboard.yaml: mode: docs` that flips defaults (anonymous reads on, comment/feedback primitives on, announcement bar visible, sitemap generated). Or is that over-design — can everything be a per-feature toggle?

### Q4 — versioning
Folder-based (`content/v1/`, `content/v2/`) is the simplest. Data-store-based history is the most powerful. Frontmatter-based (`version: v2`) is the lightest. Which wins is a separate spec.

### Q5 — how much to lean into docs as the primary use case
The current product positioning is "dashboard server for agent work". If docs becomes the more popular use case, the marketing story shifts: "docs + dashboards, same tool, AI writes both". Worth discussing before investing heavily.

---

**End of idea collector.** When we're ready to move on any of these, we cut a `spec-docs-<feature>.md` and commit to it.
