# Hosting AgentBoard

Notes on the current public deploy and the open decisions to revisit later.

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
- `.github/workflows/deploy.yml` — CI deploy pipeline
- `internal/server/middleware_auth.go` — the token gate
- `bruno/hosted/` — Bruno collection that exercises the live instance
- `seams_to_watch.md` §"Single-token auth gate" — what the token does and doesn't protect against
