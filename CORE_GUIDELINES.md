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

The expected writer of pages, components, and data is an LLM (Claude, agents, scripts). Every public surface — REST verbs, MCP tool names, MDX syntax, component prop shapes — is optimized for what an LLM naturally produces, not for human ergonomics.

If a "more correct" design is harder for an agent to call, the agent-friendly version wins.

## 5. Humans are the primary reader, and they're not technical

The rendered output — dashboard, doc, skill reference, or runbook — is a polished, readable surface. Not a developer console.

No SQL panels, no log viewers as primary UX, no "advanced" toggles, no jargon in the default view. If a non-developer can't glance at the page and understand it, the design failed.

## 6. Rendering is one-way

Storage is flexible. Scalars can live inline in MDX (the page is the truth — `<Status state="running" label="Deploy" />`). Collections, cross-page values, and agent-pushed state live in the KV store (atomic updates matter there). Files live in `content/`. A component reads from whichever source the author picked.

What's **not** flexible is the flow direction:

- A render path **never mutates durable state.** Components read; they don't write back. No useEffect that does `PUT /api/data`, no submit handler that writes a file.
- A write path (REST, MCP, file save) **never produces UI directly.** It mutates and emits an SSE event. The UI observes the event and re-reads. No HTTP handler ships HTML.
- **Components don't compute; they display.** Transform your data on the way in or on the way out, not during render.

Ephemeral UI state — sort order on a Table, expand/collapse on a folder, pending picks in Grab mode — is separate from durable state. localStorage and React state are fine. Those aren't "data" in this principle's sense.

The rule, in one line: **state → render, never render → state.** It's what lets agents and humans co-author the same page without stepping on each other.

## 7. Reliable rails for an agentic world

AI is the universal adapter — but adapters have overhead, latency, and failure modes. Anything that can be built deterministically (connectors, transports, simple data pipes) **should** be, even when an AI could do it.

AgentBoard must remain connector-friendly: stable APIs, documented data shapes, no AI in the critical path of data flow. The AI sits *on top* of the rails, not inside them.

## 8. Schemas document, don't enforce

Data shapes live in `meta.props`, in component source, in `/api/data/schema` — as **documentation for the author** (Claude), not as runtime validation gates. Components are liberal in what they accept (`Chart` already takes array-of-objects OR `{labels, values}` OR `{name, value}` pairs); Claude is conservative in what it sends because it has read the docs.

This is Postel's Law, inverted: the "conservative send" is the AI's job, not the server's. Don't sprinkle `ajv`, Zod, or hand-rolled validators into handlers to reject non-conforming content. If Claude produces the wrong shape, the fix is better docs (or a clearer `meta`), not a 400.

**Carve-out — keep strict for trust boundaries and resource limits:**
- Filename and key validation (path traversal, reserved prefixes, length caps)
- Body size caps
- Auth tokens, reserved keys (`_system.*`), authorization checks
- JSON well-formedness (can't parse it → 400)

These aren't format enforcement; they're safety. Keep rejecting malformed paths, oversized uploads, and missing credentials. The rule applies to *content format*, not *safety invariants*.

## 9. Generic primitives, steer usage through docs

When a new product concept appears (skills, runbooks, prompts, whatever shows up next), ship the thinnest generic mechanism that could support it — an endpoint, a file convention, a component prop — and push the specialization into skills, `CLAUDE.md`, component `meta.props`, and authored MDX pages. Resist adding typed routes, type-specific React components, or dedicated UI chrome for a single concept: those accumulate linearly and foreclose future variants.

The test: *could this concept be discovered through an existing list endpoint + an authored page with generic components?* If yes, that's the shape. Code is for what can't be expressed as a file + a doc.

A thin backend discovery layer (e.g. walk `files/foo/*`, parse a manifest) is fine — it's a convention, not a specialization. What's not fine is mirroring that convention all the way up into hardcoded React routes, specialized hooks, and sidebar icons. Those should always be authored content.

**Concrete heuristic:** if the PR adds more than ~50 frontend LOC for a new content type, stop and ask whether a generic component + an authored page would do the job instead.

## 10. Version compositions, not components

What a user creates is a **composition** — a page that arranges bricks and feeds them data. That composition is versioned, rolled back, audited. The bricks themselves (built-in components, user-authored `.jsx`, future plugins) are stable primitives with backwards-compatible contracts. A brick's implementation evolves through software releases; a composition evolves through user writes. Don't conflate the two histories.

The test: *can a page written today still render against a brick updated tomorrow?* If yes, the brick's contract held. If no, it's a bug in the plugin system, not something the content layer should paper over.

Concretely:

- **`content_history` covers pages, files, data keys** — everything the user composed. Not components.
- **Component updates are software releases**, rolled forward/back through the update channel, not through the content timeline.
- **Missing or failed bricks render a graceful placeholder** ("GithubIssues not installed"). They don't take the page down.

The plugin ecosystem (`spec-plugins.md`) is the contract side of this principle: it's what keeps "the brick honors its contract" from being wishful thinking.

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

---

## How to use this file

Before any non-trivial change, ask which principles it touches and whether it strengthens or weakens them. If a change violates one, either reshape it until it doesn't, or surface the trade-off explicitly in the PR/conversation.

Drift between code and these principles is the single biggest risk to the product. Catch it early.
