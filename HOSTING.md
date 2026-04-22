# Hosting AgentBoard

Three supported deployment paths, pick whichever matches your scale:

| You want | Path | Cost |
|---|---|---|
| One board, minimal setup, single VPS | [Self-host on your own VPS](#self-host-on-your-own-vps-debianubuntu) (`scripts/deploy-vps.sh`) | ~€3–4/mo |
| Multiple boards (e.g. one per friend) on one host | [Multi-board via Coolify](#multi-board-via-coolify-one-box-many-boards) | ~€3–6/mo flat |
| Hands-off managed PaaS, one board only | [Fly.io](#alternative-single-board-flyio-deploy) | ~$0–3/mo usage-based |

Below: each path in detail, followed by shared operational notes (auth token locations, related files).

## Self-host on your own VPS (Debian/Ubuntu)

One-shot installer that provisions Docker, starts AgentBoard behind Caddy,
issues a Let's Encrypt cert if you pass a domain, and prints the URL + auth
token. Re-runnable — re-running with the same `--host` updates the code and
preserves the token and data.

Prerequisites: a VPS with SSH access as `root` (or a user with passwordless
sudo), Debian 12+ / Ubuntu 22.04+. If you want TLS, point a DNS A record at
the VPS *before* running the script.

```bash
# HTTP only, reached via the VPS's IP
./scripts/deploy-vps.sh --host root@1.2.3.4

# TLS via Caddy + Let's Encrypt
./scripts/deploy-vps.sh --host root@1.2.3.4 --domain ab.example.com

# Deploy a specific ref (tag, branch, or SHA)
./scripts/deploy-vps.sh --host root@1.2.3.4 --ref v0.3.0 --domain ab.example.com

# Same but as a task
task deploy:vps -- --host root@1.2.3.4 --domain ab.example.com
```

On success the script prints the URL, auth token, and deployed git SHA. Pass
`--json` if you want machine-readable output for automation. Full flag list
in `./scripts/deploy-vps.sh --help`.

The first run takes ~3 minutes (apt install + Docker image build). Subsequent
runs only rebuild if the source changed.

Cheapest realistic target: Hetzner CX22 (~€4/mo, 4 GB RAM) or a CAX11 ARM
instance (~€3.30/mo). AWS is workable but more expensive once you add the
IPv4 tax — see `SCALE.md` for the economics breakdown.

## Multi-board via Coolify (one box, many boards)

When you want to run **several AgentBoard instances on one VPS** — e.g. one
per friend dogfooding the app — use [Coolify](https://coolify.io) as the
orchestrator. Each friend gets their own subdomain, isolated Docker container,
persistent volume, and auth token. Adding a new board is one script call;
redeploys on `git push` happen automatically.

**Why Coolify over raw `deploy-vps.sh`:** the VPS script is built for one
instance per host. Coolify adds per-app TLS, volumes, env management, and a
UI you can share with non-engineer friends who want to see their deploy logs
without SSH access. One CAX11 (€3.30/mo) comfortably runs 10–20 active boards.

### One-time host setup

**On your laptop:**

1. Pick a domain or subdomain you control, e.g. `boards.example.com`.
2. Add a wildcard DNS A record: `*.boards.example.com → <your-vps-ip>`.
   One record, works for every future board.

**On the Hetzner box** (Ubuntu 22.04+ / Debian 12+, root or passwordless sudo):

```bash
# Install Coolify (official one-liner)
curl -fsSL https://cdn.coollabs.io/coolify/install.sh | sudo bash

# Point a DNS record at the box for the Coolify UI itself too, e.g.
# coolify.example.com, then finish the installer wizard at that URL.
```

After the wizard:

1. **Create an API token** — Keys & Tokens → API tokens → New. Copy the
   bearer value.
2. **Create a project** — Projects → New. Note its UUID (visible in the URL).
3. **Verify the server UUID** — Servers → pick the default localhost server.
   Note its UUID.
4. **Note the environment name** — usually `production`.

### Local env (add to `~/.zshrc` or a direnv `.envrc`)

```bash
export COOLIFY_URL=https://coolify.example.com
export COOLIFY_TOKEN=<paste-from-step-1>
export COOLIFY_PROJECT_UUID=<paste-from-step-2>
export COOLIFY_SERVER_UUID=<paste-from-step-3>
export COOLIFY_ENVIRONMENT_NAME=production
export BOARDS_DOMAIN=boards.example.com
```

### Provision a new board

```bash
./scripts/new-board.sh alice
# → {"name":"alice","url":"https://alice.boards.example.com","token":"…","uuid":"…"}
```

The script creates a Coolify application called `agentboard-alice`, sets its
env vars (`AGENTBOARD_AUTH_TOKEN`, `AGENTBOARD_PROJECT=alice`,
`AGENTBOARD_PATH=/data`), and triggers the first deploy. DM the printed URL
+ token to Alice.

**One-time UI step per board** (~10 seconds): once the first deploy finishes,
open the board's Coolify page → **Storages** → **Add** → Persistent Storage,
destination `/data`. Redeploy. This is needed because Coolify's public API
doesn't yet cover storage mounts; after this one click, subsequent redeploys
preserve all data written to the board. Skip it only for throwaway demos
where data loss on redeploy is fine.

### List / redeploy / remove boards

```bash
./scripts/list-boards.sh
# NAME                         STATUS     URL
# ----                         ------     ---
# agentboard-alice             running    alice.boards.example.com
# agentboard-bob               stopped    bob.boards.example.com

./scripts/redeploy-boards.sh           # redeploy all
./scripts/redeploy-boards.sh alice     # just one
./scripts/redeploy-boards.sh --force   # bypass the build cache

# Remove a board: delete it from the Coolify UI (also offers to drop the volume)
```

### Auto-deploy on push to main

Two paths, pick whichever fits:

- **Per-app GitHub webhook** (recommended): in each board's Coolify page,
  enable Auto Deploy and paste the webhook URL into the repo's GitHub settings
  once. Coolify deduplicates across multiple apps sharing one webhook, so one
  URL wired once covers every existing and future board.
- **GitHub Actions fan-out** (fallback / manual): `.github/workflows/redeploy-coolify.yml`
  is a `workflow_dispatch` job that calls `redeploy-boards.sh`. Useful when
  you want to force every board to rebuild on demand without touching the UI.
  Requires GH secrets `COOLIFY_URL` and `COOLIFY_TOKEN`.

### Cost sanity check

| Boards | Compute | Storage (1 GB each) | Total |
|---|---|---|---|
| 1 | CAX11 €3.30 fixed | ~1 GB of the included 40 GB | **€3.30/mo** |
| 10 | same | 10 GB | **€3.30/mo** |
| 20 | same | 20 GB | **€3.30/mo** |
| >25 or heavy traffic | upgrade to CAX21 €6/mo | |

Break-even vs. Fly: roughly 3 boards with persistent volumes (Fly 3 × ~$2.40
≈ $7/mo) pay for a Hetzner box many times over. Coolify itself is free and
self-hosted — no SaaS fees.

### Trust-boundary caveats (read before inviting friends)

`seams_to_watch.md` flags that AgentBoard's auth model was designed for
"you, alone, on localhost." Running it multi-tenant on a shared box is fine
for friends who already trust each other and trust you, but note:

- Each board has one shared auth token — anyone with the token is an admin
  on that board. No user accounts, no per-route authz, no rate limiting.
- Board-to-board isolation comes from Docker containers + separate volumes.
  A kernel or Docker escape would cross that boundary; don't put anything
  truly sensitive on a shared box.
- Token rotation is manual: generate a new one, `PATCH` the env var via
  Coolify API or UI, redeploy, re-DM the new token.

If a friend wants to invite *their* coworker, either give them their own
board or wait for a real multi-user auth story.

---

## Alternative: single-board Fly.io deploy

**The repo is deliberately free of any specific deployment's identity.** The Fly app name and hosted URL are never committed — they come from local env vars (`FLY_APP`, `AGENTBOARD_URL`) and GitHub secrets (`FLY_APP_NAME`, `FLY_API_TOKEN`). Every command in this doc reads `$FLY_APP` from your shell.

## First-time setup

Assumes you have a Fly account and `flyctl` + `gh` installed.

```bash
# 1. Pick a unique app name and export it for this shell (add to ~/.zshrc / direnv to persist)
export FLY_APP=my-agentboard-testing
export AGENTBOARD_URL=https://$FLY_APP.fly.dev

# 2. Create the Fly app (uses fly.toml in this repo; --name overrides the missing `app` key)
fly launch --no-deploy --copy-config --name "$FLY_APP"

# 3. Generate + set the shared auth token
TOKEN=$(openssl rand -hex 32)
fly secrets set AGENTBOARD_AUTH_TOKEN="$TOKEN" --app "$FLY_APP"
echo "$TOKEN" > /tmp/agentboard-token   # local reference copy

# 4. Wire up GitHub Actions secrets for CI deploys
fly tokens create deploy -x 999999h --app "$FLY_APP" | gh secret set FLY_API_TOKEN
gh secret set FLY_APP_NAME --body "$FLY_APP"

# 5. Populate Bruno env for local API testing
cp bruno/.env.example bruno/.env
# edit bruno/.env — set AGENTBOARD_URL and AGENTBOARD_AUTH_TOKEN
```

After this, `git push` to `main` triggers a deploy. Locally, any `fly` command in this doc Just Works because it picks up `FLY_APP` from your shell.

## Current state (as of 2026-04-20)

- **Platform:** Fly.io, personal org, primary region `fra` (Frankfurt)
- **App:** `$FLY_APP` → `$AGENTBOARD_URL` (both set in your local shell; never committed)
- **Machines:** 1× `shared-cpu-1x`, 256 MB RAM (Fly auto-created a second HA machine on first deploy; scaled down with `fly scale count 1` — re-run this if it ever flips back)
- **Storage:** **ephemeral** (`AGENTBOARD_PATH=/tmp/agentboard`). Any data written via REST is lost on machine restart
- **Auto-stop:** `"stop"` — machine sleeps when idle and restarts on the next request
- **Auth:** single shared token in `AGENTBOARD_AUTH_TOKEN` Fly secret. Every route except `GET /api/health` requires Bearer / Basic / `?token=`
- **CI/CD:** `.github/workflows/deploy.yml` deploys on push to `main` via `flyctl deploy --remote-only` (app name comes from the `FLY_APP_NAME` GH secret, exposed as `FLY_APP` to flyctl)

## The known trade-off ("empty page after a while")

Auto-stop + ephemeral storage means: if nobody hits the URL for a few minutes, Fly stops the machine. When the next request wakes it, `/tmp/agentboard` is wiped and only the built-in `welcome.*` seed is re-created. Any dogfood/demo data authored via REST is gone.

We're currently leaving it this way to keep the Fly bill at near-$0.

## Cost reality

Fly.io **does not support spending caps or billing alerts** (as of 2026-04-20). Their recommendation is to bound cost architecturally by running fewer/smaller machines. Docs: https://fly.io/docs/about/cost-management/

Worst-case monthly bill for the current config:

| Config | Compute | Volume | Total |
|---|---|---|---|
| **Current** (auto-stop + ephemeral) | ~$0–0.50 | $0 | **~$0–0.50** |
| + persistent 1 GB volume | ~$0–0.50 | $0.15 | ~$0.15–0.65 |
| Disable auto-stop (always-on) | ~$2.10 | $0 | ~$2.10 |
| Both | ~$2.10 | $0.15 | ~$2.25 |

shared-cpu-1x 256 MB is $0.0000008/s of compute. Always-on = 730 h/mo × 3600 s × that rate = ~$2.10. Volume is $0.15/GB/mo flat. First 100 GB/mo outbound in EU/NA is free.

**Self-capping:** scaling up (bigger machine, more regions, more volumes) requires an explicit `fly` CLI command. The `fly.toml` committed to the repo *is* the upper bound. You can't accidentally 10× the bill from within the app.

**External belt-and-braces:**
- Set a merchant-specific alert in your bank/card app (most issuers support $10-ish alerts on a given merchant)
- Check https://fly.io/dashboard/personal/billing weekly — it shows month-to-date

## Options to revisit later

Pick one when you want to fix the "empty page" issue.

### Option A — Persistent volume (recommended when you come back)

Cheapest fix. ~$0.15/mo. State survives idle-stops. Redeploys preserve data. Only downside: cold start (~5-10s) still happens on the first request after idle.

```bash
fly volumes create agentboard_data --app "$FLY_APP" --region fra --size 1
```

Then edit `fly.toml`:

```toml
[env]
  AGENTBOARD_PATH = "/data"

[[mounts]]
  source = "agentboard_data"
  destination = "/data"
```

Redeploy:

```bash
fly deploy --remote-only --app "$FLY_APP"
```

### Option B — Disable auto-stop

~$2.10/mo. Machine stays up; no cold starts. Data still wipes on every `fly deploy` (which is your originally stated preference). Combine with Option A for true persistence (~$2.25/mo).

```toml
# fly.toml
[http_service]
  auto_stop_machines = "off"
  min_machines_running = 1
```

### Option C — Seed-on-demand script

No infra change. Add a `task seed:hosted` target that rehydrates demo data before a session. Suitable if the demo is only needed on-demand and the $0 bill matters more than the seams.

Rough shape:

```yaml
# Taskfile.yml
seed:hosted:
  desc: Populate the hosted Fly instance with demo pages + data
  cmds:
    - ./scripts/seed-hosted.sh
  env:
    AGENTBOARD_URL:
      sh: printenv AGENTBOARD_URL   # or hardcode for this machine
    AGENTBOARD_AUTH_TOKEN:
      sh: cat /tmp/agentboard-token   # or source from ~/.agentboard.env
```

### Option D — Switch platforms

If you want truly free + persistent, none of the big PaaS providers offer both in 2025. Realistic alternatives:

- **Hetzner CX11** (€3.79/mo, ~$4): persistent VPS, deploy via SSH/Docker. More ops work, more control.
- **Render free tier**: free compute but no disk — same ephemeral problem
- **Koyeb starter**: free compute, no disk
- **Cloudflare Workers + D1**: truly free, but doesn't fit the single-Go-binary model — would need a rewrite

## Operational cheatsheet

```bash
# All commands assume $FLY_APP is set in your shell (see First-time setup).

# What's running?
fly status --app "$FLY_APP"
fly machines list --app "$FLY_APP"

# Live logs
fly logs --app "$FLY_APP"

# Redeploy (uses remote builder — no Docker needed locally)
fly deploy --remote-only --app "$FLY_APP"

# Rotate the auth token
NEW=$(openssl rand -hex 32)
fly secrets set AGENTBOARD_AUTH_TOKEN=$NEW --app "$FLY_APP"
echo "$NEW" > /tmp/agentboard-token
# Also update bruno/.env

# Scale back to 1 if Fly ever recreates an HA second machine
fly scale count 1 --app "$FLY_APP"

# Kill everything (also destroys the volume if you create one)
fly apps destroy "$FLY_APP"
```

## Auth token locations (local dev)

- `/tmp/agentboard-token` — machine-local, recreated by whatever wrote it last
- `bruno/.env` — used by the Bruno `Hosted` environment; gitignored
- GitHub secret `FLY_API_TOKEN` — Fly deploy token. Set once: `fly tokens create deploy -x 999999h --app "$FLY_APP" | gh secret set FLY_API_TOKEN`
- GitHub secret `FLY_APP_NAME` — the Fly app name. Set once: `gh secret set FLY_APP_NAME --body "$FLY_APP"`

None of these are committed. Public repo is safe.

## Related files

- `fly.toml` — Fly app manifest (region, auto-stop, health check; app name intentionally omitted — supplied via `FLY_APP`)
- `Dockerfile` — 3-stage build (node → go → distroless)
- `.github/workflows/deploy.yml` — Fly CI deploy pipeline
- `.github/workflows/redeploy-coolify.yml` — manual fan-out redeploy of every Coolify board
- `scripts/new-board.sh` — provision a new Coolify-hosted AgentBoard instance
- `scripts/list-boards.sh` — list all boards running on the Coolify host
- `scripts/redeploy-boards.sh` — redeploy one or every Coolify board via API
- `scripts/deploy-vps.sh` — single-board install on a raw Debian/Ubuntu VPS (no Coolify)
- `internal/server/middleware_auth.go` — the token gate
- `bruno/hosted/` — Bruno collection that exercises the live instance
- `seams_to_watch.md` §"Single-token auth gate" — what the token does and doesn't protect against
