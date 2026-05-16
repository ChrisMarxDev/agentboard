# AgentBoard — Core Guidelines

The product principles. Read these before changing anything. When a proposal conflicts with one, the principle wins — or the principle needs an explicit, deliberate exception.

AgentBoard is a content surface for agent teams: agents write, humans read. The content is plural — dashboards, docs, skills, runbooks, files — sharing one tree and one set of primitives. Live dashboards are prominent but not privileged; the same project may hold mostly docs, mostly dashboards, or any mix, and the product should feel coherent for every ratio.

The full design lives in `spec.md`. This file is the load-bearing summary.

---

## 1. Single binary, zero runtime dependencies

One executable. No Node, Python, Docker, or system services to install. Pure Go on the backend (no CGO), embedded frontend bundle, embedded esbuild.

If a change requires the user to install something else to run AgentBoard, it violates the product.

## 2. Local-first, hosted-possible — same binary

`agentboard` with no flags boots a working dashboard on `localhost:3000` with no config. The same binary runs hosted with auth enabled (future). No "edition" splits, no separate codepaths. Hosted features layer on top of local; they don't fork it.

## 3. Plugin architecture for everything that grows

The product expands through additive plugin surfaces, not by accumulating core features:

- **Components** are user-droppable JSX in `components/`. New visualization needs are met by writing a component file, not by extending the core.
- **Pages** are MDX files. New dashboards are new files.
- **Data sources** are anything that can POST to the REST API. Future connectors are standalone binaries that speak that API; they aren't compiled into core.

When tempted to add a feature to the core, ask: *can this be a component, a page, or an external connector instead?* Default to yes.

## 4. AI is the primary author

The expected writer of pages, components, and data is an LLM (Claude, agents, scripts). Every public surface — REST verbs, MCP tool names, HTML/Markdown conventions, JSON typed views — is optimized for what an LLM naturally produces, not for human ergonomics.

If a "more correct" design is harder for an agent to call, the agent-friendly version wins.

## 5. Humans are the primary reader, and they're not technical

The rendered output — dashboard, doc, skill reference, or runbook — is a polished, readable surface. Not a developer console.

No SQL panels, no log viewers as primary UX, no "advanced" toggles, no jargon in the default view. If a non-developer can't glance at the page and understand it, the design failed.

## 6. Rendering is one-way

Storage is flexible. Scalars can live inline in HTML or Markdown (the page is the truth). Collections, cross-page values, and agent-pushed state live in a sibling file (atomic updates matter there). Files live in `content/`. A component reads from whichever source the author picked.

What's **not** flexible is the flow direction:

- A render path **never mutates durable state.** Components read; they don't write back. No useEffect that does `POST /_api/edit`, no submit handler that writes a file.
- A write path (REST, MCP, file save) **never produces UI directly.** It mutates and emits an SSE event. The UI observes the event and re-reads. No HTTP handler ships HTML.
- **Components don't compute; they display.** Transform your data on the way in or on the way out, not during render.

Ephemeral UI state — sort order on a Table, expand/collapse on a folder, pending picks in Grab mode — is separate from durable state. localStorage is fine. Those aren't "data" in this principle's sense.

The rule, in one line: **state → render, never render → state.** It's what lets agents and humans co-author the same page without stepping on each other.

## 7. Reliable rails for an agentic world

AI is the universal adapter — but adapters have overhead, latency, and failure modes. Anything that can be built deterministically (connectors, transports, simple data pipes) **should** be, even when an AI could do it.

AgentBoard must remain connector-friendly: stable APIs, documented data shapes, no AI in the critical path of data flow. The AI sits *on top* of the rails, not inside them.

## 8. Schemas document, don't enforce

Data shapes live in file naming conventions and rendering rules — as **documentation for the author** (Claude), not as runtime validation gates. Components are liberal in what they accept (`Chart` already takes array-of-objects OR `{labels, values}` OR `{name, value}` pairs); Claude is conservative in what it sends because it has read the docs.

This is Postel's Law, inverted: the "conservative send" is the AI's job, not the server's. Don't sprinkle `ajv`, Zod, or hand-rolled validators into handlers to reject non-conforming content. If Claude produces the wrong shape, the fix is better docs (or a clearer `meta`), not a 400.

**Carve-out — keep strict for trust boundaries and resource limits:**
- Filename and key validation (path traversal, reserved prefixes, length caps)
- Body size caps
- Auth tokens, reserved keys (`_system.*`), authorization checks
- JSON well-formedness (can't parse it → 400)

These aren't format enforcement; they're safety. Keep rejecting malformed paths, oversized uploads, and missing credentials. The rule applies to *content format*, not *safety invariants*.

## 9. Generic primitives, steer usage through docs

When a new product concept appears (skills, runbooks, prompts, whatever shows up next), ship the thinnest generic mechanism that could support it — an endpoint, a file convention, a component prop — and push the specialization into skills, `CLAUDE.md`, file conventions, and authored HTML/Markdown pages. Resist adding typed routes, type-specific server-rendered handlers, or dedicated UI chrome for a single concept: those accumulate linearly and foreclose future variants.

The test: *could this concept be discovered through an existing list endpoint + an authored page with generic components?* If yes, that's the shape. Code is for what can't be expressed as a file + a doc.

A thin backend discovery layer (e.g. walk `files/foo/*`, parse a manifest) is fine — it's a convention, not a specialization. What's not fine is mirroring that convention all the way up into hardcoded routes, specialized server handlers, and sidebar icons. Those should always be authored content.

**Concrete heuristic:** if the PR adds more than ~50 frontend LOC for a new content type, stop and ask whether a generic component + an authored page would do the job instead.

## 10. Version compositions, not components

What a user creates is a **composition** — a page that arranges bricks and feeds them data. That composition is versioned, rolled back, audited. The bricks themselves (built-in components, user-authored `.jsx`, future plugins) are stable primitives with backwards-compatible contracts. A brick's implementation evolves through software releases; a composition evolves through user writes. Don't conflate the two histories.

The test: *can a page written today still render against a brick updated tomorrow?* If yes, the brick's contract held. If no, it's a bug in the plugin system, not something the content layer should paper over.

Concretely:

- **`content_history` covers pages, files, data keys** — everything the user composed. Not components.
- **Component updates are software releases**, rolled forward/back through the update channel, not through the content timeline.
- **Missing or failed bricks render a graceful placeholder** ("GithubIssues not installed"). They don't take the page down.

(The git-substrate pivot dropped the JSX brick + composition layer; this principle now lives on as a discipline for whatever future stable-primitive layer rebuilds. Earlier framing in `docs/archive/spec-plugins.md`.)

## 11. Leverage agents; stay dependency-free

The backend never takes on work that an agent could do. No backend LLM calls, no external model APIs, no language-processing services baked into the server. If a capability needs semantic judgment (summarizing, tagging, classifying, disambiguating, extracting keywords), the writing agent produces the artifact at write time and the backend stores it.

This is what keeps #1 honest as the product grows. Every feature that looks like "we need AI in the server" is actually a prompt we haven't written yet — steer the authoring agent through tool descriptions, schemas, and response hints, and the artifact shows up in the write payload. The agents using AgentBoard are already LLMs; the backend doesn't need its own.

The test: *does this feature require the backend to call an LLM or speak to an external semantic service?* If yes, rework it as an instruction to the authoring agent, delivered through tool schema and response hints.

## 12. Responses are repair manuals (poka-yoke)

Every public response — successful *and* errored — is written for the agent that will act on it. Errors say exactly what went wrong, what the expected shape was, and where possible include a corrected example. Success responses include hints when the call was valid but suboptimal (missing summary, stale cache, deprecated param, unindexed field).

The product enforces correct usage through response design, not through docs the agent may or may not have read. An agent should be able to call a tool incorrectly, receive the 4xx, and self-correct on the next call without a human intervening. Same for a valid-but-thin call: the 2xx tells it how to be a better citizen.

Responses follow a stable shape across tools and versions. Error codes are snake_case, documented, and don't change silently. Agents learn the shape once and generalize.

This is the positive counterpart to #8: #8 says *don't reject content for format*, this says *when you do reject — for safety invariants, missing required params, bad paths — the response is a repair manual, not a stack trace.*

The test: *if an agent calls this wrong, does the response tell it how to succeed?* If the answer is "read the docs," the response is wrong.

## 13. Content is files; operational state stays in the database

The surface that humans and agents directly compose — pages, dashboards, taskboards, briefs, decks, skills, binary uploads — lives as files in a git repo. Files come in shapes the renderer recognizes: `.html` for expressive pages, `.md` for prose, `.json` for typed views like kanban boards, `.ndjson` for streams, binaries for everything else. Folders are folders.

Backup is `tar` the project root. Migration is `mv`. Audit is `grep`. A new content type is a path convention, not a new product feature.

**Operational state stays in SQLite.** Users, tokens, sessions, invitations, webhook subscriptions, OAuth clients, the rate-limit bucket — none of these are composed by hand. They're machine-managed indexes that an admin reads through dedicated UIs, never as raw text. Putting them in files would buy nothing and cost concurrent-write safety, indexed lookups, and the atomicity guarantees SQLite gives us. (Per-doc history and the activity log live in git, not SQLite — `git log` is the audit trail.)

The line: *do agents and humans compose this directly?* If yes, it's a file. If no, it's a row.

This is the unblocker for #4 and #9 on the content side: agents author, read, and reason about everything they author through one CRUD primitive over one tree, and new content types arrive as path conventions instead of new product features. It's also what enables the small, principled MCP surface in `spec.md §6`.

**Carve-outs.**

- **Commit metadata is the server's source of truth** for "who edited this and when". Files don't carry their own `modified_by` / `created_at` blocks — those live in `git log -- <path>`.
- **Operational rows are not "missing files."** Don't relocate users, tokens, etc. into `_system/` paths to satisfy this principle — the principle has already opted them out.
- **Ephemeral state** — open SSE connections, request-scoped caches, the in-flight rate-limit bucket — does not need to land anywhere durable. Anything you'd lose on a process restart and not miss is fair game for memory.

The test: *can I tar the project root, drop the SQLite operational database, restore both, and have the dashboard come back identical?* If a piece of *content* lives only in a row, that's a smell — move it to the tree. If a piece of *operational state* lives only in a file, that's also a smell — move it to a row.

---

## 14. Content lives inside its file

A leaf in the tree owns its content. A page's title, status, metric values, kanban columns — they live in *that page's* body (or its frontmatter, for `.md` files), not in a sibling file referenced by key. There is no "data tier", no `data/<key>` parallel namespace, no scalar-by-id lookup. One container per content unit.

**Carve-outs the substrate honors:**

- **Folder layouts** — a folder of `.md` files (e.g. `tasks/*.md`) is a perfectly fine way to model many-of-the-same-thing. The renderer doesn't have to know it's a "collection"; it just lists the folder.
- **Typed views via `.json`** — a `.json` file with `{columns, cards}` renders as a kanban. The shape lives in the file; the renderer reads it.
- **Streams** — `.ndjson` files are append-only logs.
- **Binaries** — images, PDFs, fonts referenced from pages via plain `<img src>` or `<a href>`. Treat them like any other static asset.

What's forbidden is "store this number somewhere, then reference it by key from another page". If two pages need the same value, denormalize, or factor both into the same file.

This is the unblocker for #5 and #4 on the agent side: a non-technical reader can answer *"where does this value come from?"* by looking at the page in front of them, and an agent that wants to bump a number commits one change to one file. It also removes the trap that previously led agents to invent parallel directory structures.

The test: *delete every other file in the repo and only keep the page you're reading. Does it still render its own content?* If the answer is "no, the value lived in `dev.users` somewhere else", that's a violation.

---

## 15. The workspace teaches the agent. The human writes one sentence.

A user should never have to maintain a long-running prompt that explains what AgentBoard is or how to interact with it. The bootstrap is *one sentence*, pasted once: "AgentBoard is at `<url>` — clone it, read `README.md`, follow what it says. AgentBoard tells you the rest." From the second the agent reads that, it is in conversation with the workspace, not with the human.

This is what makes the multi-agent collaboration fantasy actually liveable. The alternative — every new agent session, every new agent on the team, every project-drift event requires the human to remember and re-paste the right prompt — is the same antipattern that makes "AI coworker" tools brittle in practice. The knowledge of how to be useful in *this* workspace has to live in *this* workspace, not in the human's head, not in a CLAUDE.md they edit per project, not in a prompt template anyone has to version.

The substrate enables this for free. The workspace is a git repo. A `README.md` at the root is a file the agent already knows how to find and read. The README links to a `SKILL.md` that teaches the protocol; to a `CONVENTIONS.md` that teaches this team's rules; to `tasks/` (or wherever the queue is). The reference chain is durable — it survives the human, the agent, the conversation, the model upgrade. The bootstrap sentence is the one stable thing the human ever has to paste; everything else is a link they can follow.

This is why the substrate change to git is load-bearing for the project, not just an implementation detail. The "agent walks in cold, reads its way to usefulness" loop only works against a substrate the agent already speaks. Git is that. Custom REST surfaces are not.

**The tests:**

- *Could a fresh Claude session, given the single bootstrap sentence and nothing else, do useful work in this workspace within five turns?* If no, the workspace's README is incomplete, the SKILL is outdated, or the protocol is too opaque — fix the file the agent reads, not the prompt the human types.
- *Could the human delete their CLAUDE.md, paste the bootstrap sentence, and continue?* Same answer. CLAUDE.md is a fallback for repo maintainers, not a load-bearing input.
- *Does adding a new convention or skill require the human to update the bootstrap sentence?* If yes, the change went into the wrong place — push it into the workspace, not the prompt.

When this principle conflicts with anything else, the rule is: **push knowledge into the workspace, not the prompt.**

---

## How to use this file

Before any non-trivial change, ask which principles it touches and whether it strengthens or weakens them. If a change violates one, either reshape it until it doesn't, or surface the trade-off explicitly in the PR/conversation.

Drift between code and these principles is the single biggest risk to the product. Catch it early.

---

## Pivot audit (2026-05-13)

The git-substrate pivot was checked against every principle below. Result: each one either holds at par or strengthens; none weakens. The custom file-based store was hiding work git already does for free, and replacing it makes several of these principles load-bearing instead of aspirational.

| # | Principle | Pivot impact |
|---|---|---|
| 1 | Single binary, zero runtime deps | **Par.** `go-git` is pure-Go and embeds; no external `git` binary required, no system services to install. |
| 2 | Local-first, hosted-possible — same binary | **Par.** Same binary; the only state on disk is `<datadir>/repos/*.git` + `<datadir>/worktrees/*` + SQLite. Tar still backs the whole thing up. |
| 3 | Plugin architecture for everything that grows | **Stronger.** Components are `.jsx` blobs in the repo. They now version with the rest of the workspace — branches, history, and merges apply to components for free. |
| 4 | AI is the primary author | **Stronger.** Branches let multiple agents work in parallel without stepping on each other. The whole-file CAS that previously serialized agent edits is replaced by git's merge model, which agents already speak. |
| 5 | Humans are the primary reader, and they're not technical | **Par.** The UI never exposes git unless we want it to. Default view is HEAD of main; readers see live `.md` and `.html` files rendered server-side, same as today. |
| 6 | Rendering is one-way | **Par.** The SPA reads the working tree; nothing on the read path can mutate state. Writes go through git push (or MCP `propose`), never through a render component. |
| 7 | Reliable rails for an agentic world | **Stronger.** Optimistic locking via `_meta.version` was a half-implementation of what git's fast-forward semantics give us in full. Agents trying to push concurrently get a real, well-defined retry loop instead of a CAS race. |
| 8 | Schemas document, don't enforce | **Par.** Frontmatter shape stays freeform; the server still doesn't validate. `git diff` is a better authoring aid for the agent than any schema check would be. |
| 9 | Generic primitives, steer usage through docs | **Stronger.** Five primitives (clone, pull, push, branch, merge) replace the entire `agentboard_*` write surface. The remaining MCP tools exist for git-less fallback and notifications, not for routine writes. |
| 10 | Version compositions, not components | **Stronger.** Compositions are now *literally* versioned — every page edit is a commit; rollback is `git revert`. The principle stops being a slogan. |
| 11 | Leverage agents; stay dependency-free | **Par.** One new go module (`go-git`). Pure Go, well-maintained, no cgo. Same operational shape. |
| 12 | Responses are repair manuals (poka-yoke) | **Stronger.** Merge conflict markers (`<<<<<<<` / `=======` / `>>>>>>>`) are the canonical repair manual; Claude resolves them already without us inventing a custom format. The server's job is to surface conflicts cleanly via MCP; the resolution is the standard one. |
| 13 | Content is files; operational state stays in the database | **Stronger.** Content was *technically* in files in v0.13 but with three competing on-disk shapes (`content/`, `data/`, `.agentboard/content_history/`). Git collapses them to one tree. SQLite carve-out is unchanged. |
| 14 | Content lives inside its file | **Stronger.** Git enforces this naturally — there's no parallel namespace an agent could write into without committing it to the tree. The "no cross-page singletons" rule from spec §7 has structural support now, not just doc support. |
| 15 | Workspace teaches the agent | **Made possible.** This principle requires a substrate the agent already speaks. Git is that. The bootstrap loop — read the URL, clone, read README, follow links — only works because the agent doesn't need to learn a custom protocol first. On the v0.13 file substrate the agent had to be taught the API shape before it could even start; that's the antipattern this principle exists to forbid. |

**Net:** every principle the pivot touches gets sturdier ground under it. The pivot is consonant with the document, not against it.
