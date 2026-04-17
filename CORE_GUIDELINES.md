# AgentBoard — Core Guidelines

The product principles. Read these before changing anything. When a proposal conflicts with one, the principle wins — or the principle needs an explicit, deliberate exception.

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

The rendered dashboard is a polished, readable document. Not a developer console.

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

---

## How to use this file

Before any non-trivial change, ask which principles it touches and whether it strengthens or weakens them. If a change violates one, either reshape it until it doesn't, or surface the trade-off explicitly in the PR/conversation.

Drift between code and these principles is the single biggest risk to the product. Catch it early.
