# Contributing to AgentBoard

Thanks for considering a contribution. AgentBoard is open source under the [MIT License](./LICENSE) and welcomes issues, discussion, and pull requests.

## Before you start

- **Read [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md).** Eight principles define what belongs in the core vs. what should be a component, page, or external connector. PRs that fight the principles usually get reshaped; save yourself the round-trip.
- **Check [`spec.md`](./spec.md)** for the full architecture.
- **For security-sensitive changes**, read [`seams_to_watch.md`](./seams_to_watch.md) — especially anything that touches auth, network binding, or the component/upload path.
- **For larger changes, open an issue first.** A short discussion up front beats a big rejected PR.

## Dev environment

**Requirements:** Go 1.25+, Node 20+, [Task](https://taskfile.dev/) (`brew install go-task`).

```bash
git clone https://github.com/christophermarx/agentboard.git
cd agentboard
task install:frontend   # npm install
task dev                # Vite HMR + Go --dev server in parallel
```

`task dev` starts Vite on `:5173` and the Go server on `:3000` in dev-proxy mode. Edit Go code → the server restarts; edit frontend code → HMR updates the browser.

For a production-shape build:

```bash
task build      # frontend build → embedded into the Go binary → ./agentboard
./agentboard
```

Run `task` (or `task -l`) to see every available task.

## Tests and checks

Run the full suite before opening a PR:

```bash
task lint                # gofmt + go vet + ESLint
task test                # Go unit tests + frontend vitest
task test:integration    # end-to-end: starts server, hits every REST + MCP endpoint
task test:bruno          # Bruno contract tests (requires `bru` CLI)
```

CI (`.github/workflows/ci.yml`) runs gofmt, `go vet`, staticcheck, `go test -race`,
the frontend typecheck/lint/test/build, the landing build, and the integration
script on every push and PR. Run `task fmt` to auto-format Go files.

**Expectations:**

- New Go code has a test in the same package (`*_test.go`).
- New REST endpoints get a case in `internal/server/handlers_test.go` **and** a line in `scripts/integration-test.sh`.
- New frontend components have a `*.test.tsx` next to them (vitest + Testing Library).
- New or changed MCP tools get a Bruno request under `bruno/mcp/`.

Don't mock the database in handler tests — the SQLite layer is fast, and mocks drift from real behavior. Use a temp directory and a real store.

## Code style

**Go:**

- `go fmt` before committing (gopls / your editor should do this automatically).
  CI runs `gofmt -l` and fails if anything is unformatted.
- `go vet` and `staticcheck` run in CI — fix warnings rather than suppressing them.
- Keep packages focused: `internal/data` owns storage, `internal/server` owns HTTP, `internal/mcp` owns MCP. Cross-package leakage is a red flag.
- Error returns, not panics. Surface errors up to the handler; the handler decides the HTTP status.
- No CGO. AgentBoard uses pure-Go SQLite (`modernc.org/sqlite`) on purpose — adding a CGO dependency breaks the static-binary promise.

**Frontend:**

- TypeScript in strict mode. `npm run typecheck` + `npm run lint` must pass.
- Components live in `frontend/src/components/` — `builtin/` for the nine built-ins, `shell/` for layout/nav, etc.
- Data access goes through the `useData` hook (`frontend/src/hooks/useData.ts`) — never `fetch` directly inside a component.
- Styling is Tailwind. No CSS-in-JS, no module-level styles except `src/index.css`.

**Commits:** small, focused, imperative mood ("Add X" not "Added X"). One logical change per commit when possible.

## Opening a PR

1. Fork, branch from `main`, do your work.
2. Run `task test` and `task test:integration` locally — both must pass.
3. Open the PR against `main` with:
   - A short description of **what** and **why**.
   - Which principles from `CORE_GUIDELINES.md` the change touches (if any).
   - Screenshots/GIFs for any UI change.
   - A note on any new dependencies (Go modules or npm packages) — prefer the standard library when the trade-off is close.
4. CI runs tests + builds the binary on push. Fix red before requesting review.

## Scope and non-goals

AgentBoard stays small on purpose. A few things are explicitly **out of scope**:

- **Hosted multi-tenant mode.** We do not operate an AgentBoard service. The product is the self-hosted binary.
- **SQL query UIs, admin consoles, workflow engines.** If it requires training to use, it doesn't belong in the rendered dashboard.
- **Heavy runtime dependencies.** Anything that requires the user to install another service breaks the "single binary" promise.
- **AI in the data path.** LLMs author pages and push data *on top of* the rails. The rails themselves stay deterministic.

If you're unsure whether an idea fits, open an issue and ask — it's cheaper than writing the PR first.

## Security issues

**Do not open public issues for security bugs.** Email `dev@christopher-marx.de` with details, reproduction steps, and a proposed timeline. You'll get an acknowledgment within a few days.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](./LICENSE).
