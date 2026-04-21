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

## 6. Data and UI are separated, always

Agents push data to a key-value store via REST. Pages are MDX. Data changes constantly; pages change rarely. The two never bleed into each other:

- A render path never writes data.
- A write path never produces UI.
- Components don't compute; they display.

This separation is what lets agents and humans co-author the same dashboard without stepping on each other.

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

---

## How to use this file

Before any non-trivial change, ask which principles it touches and whether it strengthens or weakens them. If a change violates one, either reshape it until it doesn't, or surface the trade-off explicitly in the PR/conversation.

Drift between code and these principles is the single biggest risk to the product. Catch it early.
