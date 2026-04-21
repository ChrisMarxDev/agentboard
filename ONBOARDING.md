# AgentBoard Onboarding

> **Status:** Baseline / design document — captures the intended user journey so
> implementation, the landing page, docs, and the installer skill all stay
> aligned. Not a spec. Update this file whenever the canonical flow changes.

## Who this is for

AgentBoard's primary user is **someone already using Claude** who wants a live
dashboard their agents can write into — without standing up a stack.

Two personas sit under that:

- **Non-technical / low-code** — uses Claude Desktop or Claude Code, is
  comfortable approving tool calls, but doesn't want to open a terminal, read
  README, or manage binaries. They want to say "set me up with AgentBoard" and
  see a dashboard in their browser two minutes later.
- **Developer** — will `brew install`, `task run`, fork the repo, or embed
  AgentBoard inside a larger system. This path already exists; onboarding for
  them is just a good README.

**This document focuses on the first persona.** The dev path is the fallback.

## The principle

> **One sentence to Claude → working dashboard in the browser.**

Everything else (binary install, config, port selection, MCP wiring, first
page, seeded data) is the skill's responsibility. The user never types a shell
command, never edits a file, never sees a port number unless they ask.

## Canonical flow

**One terminal command**, then everything else happens inside Claude.

```
┌─────────────────────────────────────────────────────────────────┐
│  1.  Landing page (agentboard.dev)                              │
│         ↓                                                        │
│  2.  Hero CTA shows the install line. User pastes into a        │
│      terminal:                                                   │
│                                                                  │
│        curl -fsSL https://agentboard.dev/install.sh | sh        │
│         ↓                                                        │
│  3.  install.sh:                                                 │
│       • detects OS + arch                                        │
│       • downloads binary from GitHub releases (checksum verify)  │
│       • writes skill to ~/.claude/skills/agentboard/SKILL.md     │
│       • patches Claude Desktop's mcpServers if present           │
│       • prints next-step instructions                            │
│         ↓                                                        │
│  4.  User opens / restarts Claude (Desktop or Code) and says:   │
│      "start AgentBoard"                                          │
│         ↓                                                        │
│  5.  Skill starts the server in background, waits for health    │
│      check, opens the browser, seeds a welcome page via MCP,    │
│      demonstrates a live update.                                 │
│         ↓                                                        │
│  6.  User is onboarded — live dashboard, Claude can write to it │
└─────────────────────────────────────────────────────────────────┘
```

**Why a script, not the plugin marketplace?**

- Works the same in **Claude Code and Claude Desktop**. The Claude Code plugin
  system is nicer, but Desktop has no `/plugin install`, and Desktop is where
  most non-technical users live today.
- One command, zero prior tooling — no Homebrew, no `bun`, no terminal
  familiarity beyond "paste this and press enter."
- Mirrors a pattern users already trust (Homebrew, Rust, Deno, oh-my-zsh,
  nvm, gstack).
- The plugin-marketplace path remains available later as a secondary
  surface for Claude Code users who prefer it (see "Alternative paths").

## Step-by-step UX

### Step 1 — Discovery (landing page)

**agentboard.dev** (this repo's `landing/`). The hero is structured around
the install line:

```
curl -fsSL https://agentboard.dev/install.sh | sh
```

Copy button sits next to it. Secondary line reads *"Then say 'start
AgentBoard' in Claude."* — this is the entire pitch for the non-technical
user. A tertiary "I'm a developer →" link points to the GitHub README.

### Step 2 — Run the installer

User pastes the command in Terminal / iTerm / Warp / whatever shell they
have. The script is auditable (it's just a POSIX sh file) — before piping
to `sh`, paranoid users can `curl -fsSL ... | less`.

The script prints every step it takes. A complete run looks roughly like:

```
AgentBoard installer
  repo:     anthropics/agentboard
  version:  latest
  prefix:   /Users/you/.agentboard

→ Downloading binary (darwin_arm64, latest)
✓ checksum verified
✓ binary installed: /Users/you/.agentboard/bin/agentboard
→ Installing Claude skill
✓ skill installed: /Users/you/.claude/skills/agentboard/SKILL.md
→ Wiring AgentBoard MCP into ~/Library/Application Support/Claude/claude_desktop_config.json
✓ MCP wired (backup: …claude_desktop_config.json.agentboard-backup-1713612345)
  Restart Claude Desktop for the MCP server to appear.

────────────────────────────────────────────────
  AgentBoard installed ✓
────────────────────────────────────────────────

Get started in Claude:
    Say "start AgentBoard" or "open my dashboard"
```

Under the hood the script:

1. Detects OS + architecture (`uname -sm`, canonicalized to
   `darwin_arm64` / `linux_amd64` / etc.).
2. Downloads the binary tarball from GitHub releases — uses the `latest`
   alias by default, accepts `AGENTBOARD_VERSION=vX.Y.Z` to pin.
3. Fetches `agentboard_checksums.txt`, verifies SHA-256 with `shasum` or
   `sha256sum`. Mismatch → aborts. Missing checksum file → warns and
   continues.
4. Extracts to `~/.agentboard/bin/agentboard`, `chmod +x`.
5. Downloads the Claude skill file (`install/skill/SKILL.md` in this repo)
   to `~/.claude/skills/agentboard/SKILL.md`. The skill is the same on
   Code and Desktop.
6. If `claude_desktop_config.json` exists, merges an `agentboard` entry into
   `mcpServers` using `jq`. Always writes a timestamped backup first. If
   `jq` isn't installed, prints a copy-paste snippet for the user.
7. Prints a PATH hint if `~/.agentboard/bin` isn't on `PATH` (non-fatal —
   Claude invokes the binary by full path).
8. Prints the summary card.

Idempotent: re-running is a no-op unless `FORCE=1`. Uninstall is `rm -rf
~/.agentboard ~/.claude/skills/agentboard` plus removing the `agentboard`
entry from the Desktop config.

### Step 3 — Kickoff in Claude

User opens or restarts Claude (Desktop needs a restart for the new MCP
server to appear; Code just loads the new skill at next session start).
They say something like:

- "start AgentBoard"
- "open my dashboard"
- "set me up with AgentBoard"

The skill description matches these triggers. Claude invokes it.

### Step 5 — Start server

- Pick a free port (default 3000, fall back to 3001+).
- Pick a named project (default: user's home folder name, e.g. `chris-desktop`;
  not `default`, which is reserved for first-run).
- Launch: `agentboard serve --project <name> --port <port> --no-open`.
- Wait for `/api/health` to return 200.

Server runs in the background for the lifetime of the session. The skill
remembers PID + port so it can stop, restart, or re-open.

### Step 6 — Open the browser

`open http://localhost:<port>` (macOS) / `xdg-open` (Linux) / `start` (Windows).

The user sees a live, empty-but-friendly dashboard within ~5 seconds of saying
"start AgentBoard."

### Step 7 — Seed a welcome page

Skill authors a `welcome.mdx` page in the project directory with:

- A short "you're live" message.
- A `<Metric>` bound to `welcome.events` that starts at 0.
- A `<Log>` bound to `welcome.log` showing what Claude has done so far.

This gives the user something to look at besides an empty shell, *and* it
demonstrates the three primitives: pages, components, data.

### Step 8 — MCP is already live (installer wrote the config)

The installer patched Claude Desktop's `claude_desktop_config.json` to include:

```json
{
  "mcpServers": {
    "agentboard": {
      "command": "/Users/<user>/.agentboard/bin/agentboard",
      "args": ["mcp"]
    }
  }
}
```

Because this ran at install time, the AgentBoard MCP tools are callable the
first time the user opens Claude after running the installer (Desktop needs
one restart; Code picks them up on next session). The skill confirms by
running `agentboard:list_keys` and reporting the schema it just seeded.

Claude Code users whose MCP config lives elsewhere can still add the server
via `claude mcp add` or the plugin path (see "Alternative paths" below).

### Step 9 — Teach by doing

The skill says something like:

> Your AgentBoard is live at http://localhost:3000. I've opened it in your
> browser and seeded a welcome page with a live counter and a log.
>
> Try: **"write 'hello' to the log"** — I'll update it, and you'll see your
> dashboard change in real time.

The user does that. The counter increments, the log updates live via SSE. They
now understand the mental model: **Claude writes, dashboard updates, no
infrastructure.**

## The skill's responsibilities

One skill, one set of Bash/MCP permissions. Operations it owns:

| Capability | Detail |
|---|---|
| Detect existing install | `command -v agentboard`, `agentboard --version` |
| Install binary | Homebrew / tap, or signed GH release download with checksum verify |
| Start/stop server | Background process management, PID tracking |
| Open browser | Platform-aware `open` / `xdg-open` / `start` |
| Seed first page | Write MDX into the project dir |
| Call MCP tools | Issue data writes to demonstrate realtime |
| Diagnose | If port taken, binary missing, MCP disconnected → explain + repair |
| Tear down | Stop server, clean project dir on request |

Explicitly **not** the skill's job:

- Long-term agent orchestration (that's the user's existing Claude workflow).
- Cloud hosting (separate path — see `HOSTING.md`).
- Editing the user's code or dotfiles.

## Success criteria

A user is "onboarded" when **all four** are true:

1. `agentboard --version` returns a real version.
2. A background `agentboard serve` is reachable at `http://localhost:<port>/api/health`.
3. The browser tab is open at the dashboard and shows the welcome page.
4. At least one MCP tool call has successfully written data that changed the
   visible UI (proves the end-to-end loop).

The skill reports "you're onboarded" only after all four pass. Failure in any
one becomes a diagnostic branch, not a silent half-state.

## Alternative paths

The `curl | sh` installer is **the** path for 99% of users. These three
alternatives exist for the edges.

| Audience | Path | How |
|---|---|---|
| Default (Desktop + Code) | `install.sh` | `curl -fsSL https://agentboard.dev/install.sh \| sh` |
| Claude Code power users | Plugin marketplace | `/plugin install agentboard@anthropic` — same binary, bundled via `.claude-plugin/plugin.json` + `.mcp.json` |
| Developers | Homebrew + source | `brew install agentboard/tap/agentboard`, clone the repo, `task run` |

### When to add the plugin-marketplace path

Later, as a second rail for Claude Code users. Upsides:
- No shell step at all — `/plugin install` from inside Claude.
- Version pinning per plugin release.
- MCP auto-wired via `.mcp.json` without touching a config file.
- `/plugin update agentboard` is the update mechanism.

We don't need it on day one. The install script works everywhere; the plugin
is a nice-to-have for the Claude-Code-native crowd.

### When to add the developer path

Also later, but trivially — it's just documentation pointing at the repo's
README. Some users distrust magic install flows; `brew install` + manual
`agentboard serve` + hand-wired MCP stays supported as the "I read your
shell script and I want to do this differently" option.

### Why Desktop doesn't get its own path

It doesn't need one — the install script covers it. Claude Desktop has no
plugin system (as of Apr 2026) but it *does* auto-load skills from
`~/.claude/skills/` and MCP servers from `claude_desktop_config.json`. The
installer writes both. One script, two surfaces.

## Ongoing use (what happens after day 1)

The skill isn't just an installer. Post-onboarding, the user invokes it (or
adjacent sub-skills in the same plugin) for:

- **"open my dashboard"** — opens the browser tab if closed, starts server if
  stopped.
- **"add a page for <X>"** — scaffolds an MDX page with relevant components.
- **"stop AgentBoard"** — clean shutdown.
- **"reset"** — nuke the project, reseed.
- **"update AgentBoard"** — upgrade the binary to the latest release.

The plugin is the surface; the binary is the engine; the browser is the
output. The user never has to know which is which.

## What's in the repo today

The onboarding artifacts ship from this repo:

| Path | Role |
|---|---|
| [`install.sh`](./install.sh) | The singular installer users run. POSIX sh, idempotent, auditable. |
| [`install/skill/SKILL.md`](./install/skill/) | The user-facing Claude skill the installer drops into `~/.claude/skills/agentboard/`. |
| [`landing/`](./landing/) | The marketing site whose hero CTA is this installer. |
| `.claude/skills/agentboard/` | The **in-repo dogfood** skill — **not** shipped to users. |

Keep `install.sh` and `install/skill/SKILL.md` in lockstep — if the skill
changes the expected binary path, config structure, or CLI flags, the
installer has to change too. Release tagging should cover both (and the
binary itself).

## How this shapes nearby work

- **Landing page (`landing/`)** — hero CTA is the install one-liner. Secondary
  CTA: *"Then say 'start AgentBoard' in Claude."* Tertiary "I'm a developer →"
  links to the GitHub README.
- **README** — stays developer-first. Linked from the landing page's developer
  CTA. Should also quote the install line at the top for people arriving from
  GitHub search.
- **`HOSTING.md`** — separate concern (multi-user / persistent deploys). Not
  part of personal onboarding.
- **Release pipeline** — builds the binary for `darwin_{amd64,arm64}`,
  `linux_{amd64,arm64}`, tarballs each, uploads to GitHub releases, generates
  `agentboard_checksums.txt`. Without this, `install.sh` has nothing to
  download. This is the single biggest prerequisite to shipping onboarding.
- **Dogfood skill (`.claude/skills/agentboard/`)** — the in-repo skill is for
  us as AgentBoard developers. It is **not** the user-facing install skill.
  Don't conflate them.
- **Domain `agentboard.dev`** — needs to resolve and redirect
  `/install.sh` to the current `install.sh` in the repo (CNAME to GitHub Pages
  serving the file, or a tiny redirect service). Until DNS is set up, the
  install URL is the raw GitHub URL.

## Open questions

Decisions still to make. Items marked **[resolved]** reflect the singular-
installer direction.

1. **[resolved] Distribution** — one POSIX `install.sh` served from
   `agentboard.dev/install.sh`. Same artifact covers Claude Desktop and Claude
   Code. Plugin marketplace is a later, optional second rail.
2. **[resolved] MCP wiring** — `install.sh` patches
   `claude_desktop_config.json` (macOS + Linux paths) to add an `agentboard`
   entry under `mcpServers`. Uses `jq` for safe JSON merge; falls back to a
   copy-paste snippet if `jq` isn't installed.
3. **[resolved] Binary install mechanism** — `install.sh` downloads
   `agentboard_<os>_<arch>.tar.gz` from GitHub releases, verifies SHA-256 from
   `agentboard_checksums.txt`, unpacks to `~/.agentboard/bin/`.
4. **[resolved] Skill distribution** — `install.sh` downloads
   `install/skill/SKILL.md` from this repo at the pinned `AGENTBOARD_REF`
   (defaults to `main`) and writes it to `~/.claude/skills/agentboard/`.
5. **Release pipeline** — needs to exist. GitHub Actions on tag push: cross-
   compile the Go binary for 4 platforms, tarball, generate checksums, upload
   as a GitHub release. Without this the installer has nothing to fetch.
6. **Code signing** — macOS Gatekeeper flags unsigned binaries on first run;
   Windows SmartScreen same. Apple Developer cert ($99/yr) + notarization
   pipeline is cost + effort for real non-technical UX. Can ship v0.1
   unsigned with a short "you'll see a security prompt, click Open" note.
7. **Port + project defaults** — hardcode 3000 and `default` for v0.1.
   Remember across sessions via `~/.agentboard/config.json` once per-user
   customization matters.
8. **Welcome content** — counter + log is the minimum; the skill seeds it on
   first run. Anything richer needs real agent integration to be meaningful.
9. **Updates** — `curl -fsSL agentboard.dev/install.sh | FORCE=1 sh`
   re-runs the installer. Skill can invoke this for the user on "update
   AgentBoard". Could later add an `agentboard self-update` subcommand.
10. **DNS / CDN for `agentboard.dev/install.sh`** — the URL has to resolve
    before the landing page can ship. Options: GitHub Pages on a custom
    domain, Cloudflare Worker redirect to raw GitHub, or a static Fly app
    serving the file.
11. **Telemetry / success measurement** — do we count installs? Lightweight
    anonymous ping at the end of `install.sh` (opt-out) is the cheapest
    signal. Defer — not a day-one need, and adds trust overhead to the
    install line.

## Non-goals

To keep the flow sharp, onboarding explicitly is **not**:

- A config wizard. No questions before the dashboard appears. Defaults get you
  live; customization comes later and is optional.
- A tutorial on dashboards, MDX, or MCP. We show, don't tell. The user learns
  by watching their own actions show up in the UI.
- A multi-user / team setup. Single user, single laptop, single binary.
- A cloud signup. Nothing leaves the user's machine during onboarding.
