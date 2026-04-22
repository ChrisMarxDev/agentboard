# AgentBoard — Plugin ecosystem

> **Status:** design spec. Companion to `CORE_GUIDELINES.md` §10 ("Version compositions, not components"). Integral to the product.

Bricks vs. compositions. A page is a *composition* — a layout of bricks fed by data the user wrote. A brick is a stable primitive that renders inputs. The content layer versions compositions; bricks evolve through software releases.

This document is the contract that makes "compositions vs. bricks" work in practice: what a brick is, how it's distributed, what it can do, what it can't, and how the plugin system stays safe as it opens up.

---

## 1. What is a brick

A brick is a React component, registered under a name, that:

- Takes declared props (typed, documented in `meta.props`).
- Renders output — inline markup, chart, kanban board, external data view, whatever.
- Honors a stable contract with the content layer: **old compositions keep rendering under new bricks.**

Bricks are stateless in the content sense — they don't own durable state. State lives in data keys, files, or external systems. A brick reads; it doesn't persist on its own authority.

### First-party bricks (today)

21 built-ins shipped in the binary: `Metric`, `Status`, `Progress`, `Table`, `Chart`, `TimeSeries`, `Log`, `List`, `Kanban`, `Deck`, `Card`, `Stack`, `Markdown`, `Badge`, `Counter`, `Code`, `Mermaid`, `Image`, `File`, `Errors`, `ApiList`. Import lands in the registry at boot.

### User bricks (today)

Drop a `.jsx` file into `components/`. The file watcher picks it up; Manager registers it; `getComponents()` returns it on the next render. This is **Phase 1 distribution** — covers solo self-hosters and small teams.

### Third-party bricks (Phase 2)

A URL-based install model. Users fetch a brick bundle from a git repo or a publicly hosted URL:

```bash
agentboard plugins install https://github.com/team/agentboard-github-issues
# or
agentboard plugins install https://plugins.example.com/github-issues/v1.2.0
```

Installation writes the bundle + manifest to `components/<slug>/`, runs integrity checks (signature, manifest shape), and registers. **No central marketplace.** The registry is the open web — git repos, self-hosted URLs. AgentBoard itself may maintain an official curated registry at `plugins.agentboard.dev`, but that's *one* source among many, not mandatory.

---

## 2. The contract

Every brick must satisfy:

### 2.1 Stable props

Props listed in `meta.props` are the public API. Removing or renaming an existing prop = breaking change = requires a new brick name (`Kanban` → `KanbanV2`) or semver major bump with migration guide.

Adding new *optional* props is non-breaking. Default values keep old compositions working.

### 2.2 Graceful input handling

Per Principle §8, bricks are **liberal in what they accept**. Wrong-shape data = render a placeholder or beacon an error, don't throw. The content layer tolerates a faulty input; a faulty brick takes down only its own card, never the whole page.

### 2.3 No durable side effects

A brick can read data keys (via `source=`), fetch from its allow-listed hosts (§4), and render. It can't:

- Write to the content tree
- Write to data keys
- Persist to localStorage beyond ephemeral UI state (sort order, expanded/collapsed)
- Register background workers

These are **data-plane** actions. If a brick needs them, it's not a brick — it's an agent. Use an MCP tool or a REST call from the server side.

### 2.4 Self-describing

Every brick registration includes a `meta` object:

```ts
{
  name: "GithubIssues",
  version: "1.2.0",
  description: "Lists open issues from a GitHub repo.",
  props: {
    repo: { type: "string", required: true, description: "owner/repo" },
    state: { type: "string", description: "open | closed | all", default: "open" },
    limit: { type: "number", default: 20 }
  },
  allowed_hosts: ["api.github.com"]
}
```

`/api/components` returns this metadata; it's how agents learn to compose pages with the brick.

---

## 3. Distribution — phases

### Phase 1: upload JSX (today)

- Write `components/Foo.jsx` → registered.
- `--allow-component-upload` flag gates agent-authored uploads (`PUT /api/components/Foo`).
- Trust model: caller has full privileges. Appropriate for solo + small team.

### Phase 2: URL install

- `agentboard plugins install <url>` fetches a bundle + manifest + signature.
- Manifest is YAML:
  ```yaml
  name: github-issues
  version: 1.2.0
  entry: dist/index.js
  exports: [GithubIssues]
  allowed_hosts:
    - api.github.com
  signature: sha256:abc...
  ```
- AgentBoard verifies signature, stores under `components/<slug>/`, registers on next watcher tick.
- Update: `agentboard plugins update github-issues` or `agentboard plugins install <url>@v1.3.0`.
- Pinning: `agentboard.yaml` lists installed plugins with pinned versions; CI/provisioning reproduces the brick set.

### Phase 3: curated registry (if we need it)

- `plugins.agentboard.dev` — an optional central index mirroring a curated set of plugins.
- Published via git PR against a manifest repo. No marketplace dynamics, no revenue share in scope yet.
- `agentboard plugins install github-issues` (no URL) → resolves via the registry.
- Users can configure alternate registries in `agentboard.yaml`: personal git repo, team-internal URL, etc.

**The registry is metadata, not hosting.** Bundles still live at whatever URL the author picked (git raw, GitHub releases, their own CDN). The registry just maps `name → url`.

---

## 4. Trust model

Two classes, two policies.

### First-party (bundled, built-in)

- Full page privileges.
- Reviewed in code review; shipped via release.
- Same trust level as AgentBoard itself.

### Third-party (user / URL-installed)

**Sandboxed iframe per brick instance.**

- Each brick renders in an iframe with `sandbox="allow-scripts"`, no parent-origin access.
- Parent (AgentBoard) passes props via `postMessage` on mount.
- Brick can request data reads via a narrow message protocol:
  ```
  → parent: { op: "read", key: "tasks" }
  ← brick:  { op: "read-result", key: "tasks", value: {...} }
  ```
- Parent enforces: brick can only read keys that were passed as `source=` props. No arbitrary KV access.

This is the default for everything that arrives via URL install. Opt-out exists but requires `plugins.trust_elevated: [name]` in `agentboard.yaml` — a conscious operator choice, per-plugin, logged.

### Outbound requests

Bricks declare `allowed_hosts` in their manifest. The parent iframe enforces via Content-Security-Policy:

```
default-src 'none';
script-src 'self';
connect-src https://api.github.com;
style-src 'self' 'unsafe-inline';
img-src 'self' data: https://api.github.com;
```

CSP is computed per-brick from its manifest. A brick trying to fetch outside its declared hosts → browser blocks → brick sees a network error → beacons as a render error.

### Revocation

`agentboard plugins uninstall <name>` removes the component and registers a tombstone so existing compositions that reference it render the graceful placeholder (§6).

---

## 5. Data access

What a brick can read, three layers:

| Source | Access |
|---|---|
| Its declared props | Direct (from the MDX composition) |
| Data keys referenced by `source=` props | Via parent message protocol, scoped to those keys |
| Anything else | Not allowed (sandbox blocks it) |

What a brick CAN'T do:

- Read arbitrary data keys (no bulk enumeration)
- List or read other files / pages
- Call AgentBoard's admin endpoints
- Read other bricks' iframes or state

This maps directly to Principle §6 (data and UI separated) and §10 (version compositions, not components). A brick is pure UI over inputs. Persistence and retrieval are the core's job.

---

## 6. Graceful degradation

When a composition references a brick that isn't registered:

```mdx
<UnknownBrick source="x" />
```

Renders as:

```
┌─────────────────────────────────┐
│  Brick "UnknownBrick" not       │
│  installed.                     │
│  Install via:                   │
│  agentboard plugins install X   │
└─────────────────────────────────┘
```

Not a compile error, not a broken page. The rest of the composition renders; the missing brick is a dotted-border placeholder with an install hint. Same treatment when:

- A brick is installed but its manifest is invalid → placeholder with "manifest error"
- A brick version mismatches the composition's frontmatter pin → placeholder with "requires v2.0, found v1.1"

This is non-negotiable. Compositions must never hard-fail because of a missing brick — otherwise a lost plugin would take down all the pages that reference it.

---

## 7. Versioning

### Brick versions

Semver. The manifest declares `version: "1.2.0"`. A composition can pin:

```mdx
---
requires:
  GithubIssues: ^1.2.0
---
```

- No pin → latest installed version, tolerate any.
- Pin with caret → tolerate compatible updates.
- Pin exact → fail render (graceful placeholder) if version doesn't match.

Most compositions should have no pin. Pins are for brittle bricks or compliance scenarios.

### AgentBoard versions

Built-in bricks are versioned with the binary. Upgrading AgentBoard can bump built-in brick versions. Those upgrades must pass §10's contract (old compositions still render).

### Uninstalling vs. downgrading

`uninstall` removes the brick; compositions degrade to the placeholder. A downgrade (install older version over newer) is explicit and logged. No silent rollbacks.

---

## 8. Authoring workflow

What a plugin author does:

1. Scaffold: `agentboard plugins new github-issues` → creates a directory with sample component, manifest, test page.
2. Develop: `agentboard plugins dev` runs a local instance where the brick loads from disk with hot reload.
3. Build: `agentboard plugins build` produces a signed bundle + manifest + README.
4. Publish: push to git, tag a release. That tagged URL is the install URL.
5. (Optional) Submit to `plugins.agentboard.dev` registry.

Plugin source is whatever the author wants — a single `.jsx`, a TypeScript project, a full monorepo. AgentBoard only cares about the final bundle + manifest.

---

## 9. What's integrated with history/backup

Per §10:

- **Content references bricks by name + version (optional) in MDX.** That reference IS versioned.
- **Bricks themselves aren't in `content_history`.** A bugged brick update is rolled back via the plugin system (`agentboard plugins install <name>@<prev-version>`), not via content rollback.
- **The `components/` directory IS in the backup snapshot** — so if a team installs a brick today, gets backed up, restores tomorrow, the brick comes back with it. But per-brick versioning is the plugin manager's concern, not the content layer's.

This separation is why the content layer is simple and the plugin layer can evolve independently.

---

## 10. Non-goals

- **A marketplace with transactions.** We're not building plugin commerce. Third parties host their own bundles.
- **Plugin-to-plugin IPC.** Bricks render independently; cross-brick communication happens through shared data keys, not direct messaging.
- **Server-side plugins in Phase 1.** Everything here is about UI-layer bricks. Server-side extension (custom MCP tools, REST handlers) is a separate concern and not in scope.
- **Automatic update of unpinned plugins.** Even for unpinned compositions, plugin updates are explicit (`agentboard plugins update`). No silent pull-to-latest.
- **WebAssembly plugins.** Nice-to-have later for performance-critical bricks, not now.

---

## 11. Roadmap

**Phase 1 (today → near-term).** Upload-JSX model continues to work. Add:

- `meta.allowed_hosts` field to component manifests (currently props-only).
- `requires:` frontmatter support in pages.
- Graceful placeholder component (§6).
- Principle §10 enforced via tests: every built-in brick has a smoke test proving old composition source still renders under the current version.

**Phase 2 (when plugins are worth the build).**

- `agentboard plugins install <url>` CLI.
- Bundle format + signature verification.
- Sandboxed iframe rendering path, selected via manifest flag.
- CSP enforcement per brick.
- Plugin uninstall + tombstone.

**Phase 3 (if we see adoption).**

- `plugins.agentboard.dev` registry (curated metadata only, no bundle hosting).
- `agentboard plugins new` scaffold tool.
- Plugin dev-loop improvements: hot reload, local test harness.

**Phase 4+ (speculative).**

- Server-side plugin extension point (custom REST handlers, MCP tools).
- WASM brick option for CPU-heavy rendering.
- Plugin revenue mechanics (if the ecosystem grows enough to warrant).

---

## 12. Open questions

- **Bundle format.** ESM module + manifest JSON, or a single tarball? ESM + JSON is more transparent; tarball is more cohesive.
- **Signature mechanism.** Simple hash + author public key, or reuse a package-signing format (Sigstore, minisign)? Reuse is safer, custom is simpler.
- **How bricks express data read permissions.** Today we propose "only keys referenced as `source=` props." Does that cover all realistic use cases, or do we need a manifest declaration like `reads: ["tasks.*"]`?
- **First-party vs. third-party styling.** Should third-party bricks be able to use AgentBoard's design tokens (CSS variables), or do they style themselves in isolation? Tokens = cohesive look, tight coupling. Isolation = plugin-author freedom, visual inconsistency.
- **Dev-loop:** does `agentboard plugins dev` need a dedicated mode in the main binary, or can it just be a file-watch trick on top of Phase 1?

These don't block Phase 1 shipping. They shape Phase 2.

---

## 13. Where this lives

- `CORE_GUIDELINES.md §10` — the principle.
- This file — the contract.
- `/features/plugins` on the dashboard — the live, linkable mirror of this spec.
- `seams_to_watch.md "User components run with full page privileges"` — the deferred concern §4 addresses.
- Future: `PLUGIN_AUTHOR_GUIDE.md` for people actually building bricks (out of scope today).
