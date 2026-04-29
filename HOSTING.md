# Hosting AgentBoard

Two supported deployment paths, pick whichever matches your scale:

| You want | Path | Cost |
|---|---|---|
| One board, minimal setup, single VPS | [Self-host on your own VPS](#self-host-on-your-own-vps-debianubuntu) (`scripts/deploy-vps.sh`) | ~€3–4/mo |
| Multiple boards (e.g. one per friend) on one host | [Multi-board via Coolify](#multi-board-via-coolify-one-box-many-boards) | ~€3–6/mo flat |

Production today (`agentboard.hextorical.com`) runs the multi-board
Coolify path on a Hetzner box. Below: each path in detail, followed by
shared operational notes (auth token locations, related files).

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
env vars (`AGENTBOARD_PROJECT=alice`, `AGENTBOARD_PATH=/data`), and triggers
the first deploy. The first boot prints a `/invite/<id>` URL — DM the
printed URL to Alice; she opens it, picks a username and password, and
becomes the first admin on her board.

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

Hetzner is the cheapest serious option: a single CAX11 carries 10–20
boards for one flat fee. Coolify itself is free and self-hosted —
no SaaS fees on top.

### Trust-boundary caveats (read before inviting friends)

`seams_to_watch.md` flags concerns AgentBoard hasn't fully closed for
multi-tenant deployments. Running boards for friends-who-trust-friends is
fine; running on a shared box for an organisation that doesn't already
trust each other isn't yet the right shape. Specifically:

- Within a board, AgentBoard has real per-user accounts (admin / member /
  bot kinds, individually rotatable tokens, browser sessions with CSRF,
  audience-scoped OAuth tokens for MCP). Lockout recovery is filesystem-
  level via `agentboard admin set-password`, `admin rotate`, or
  `admin invite`. See `AUTH.md` for the full design.
- Board-to-board isolation comes from Docker containers + separate
  volumes. A kernel or Docker escape would cross that boundary; don't put
  anything truly sensitive on a shared box.
- Component upload (`--allow-component-upload`) is OFF by default. If you
  turn it on, every authenticated caller can plant arbitrary JS that runs
  in every visitor's browser — keep it off unless you fully trust every
  caller. `seams_to_watch.md §"User components run with full page
  privileges"` covers the deferred sandboxing options.

If a friend wants to invite *their* coworker on the same board, the auth
model already supports that (admin invites them via `/admin → New
invitation`); board-level isolation is what they'd lose by sharing.

---

## Auth token locations (local dev)

- `/tmp/agentboard-token` — machine-local, recreated by whatever wrote it last
- `bruno/.env` — used by the Bruno `Hosted` environment; gitignored
- GitHub secrets `COOLIFY_URL` + `COOLIFY_TOKEN` — used by
  `redeploy-coolify.yml` to fan out manual redeploys

None of these are committed. Public repo is safe.

## Related files

- `Dockerfile` — 3-stage build (node → go → distroless), used by every
  deploy path (Coolify pulls it on each redeploy; `deploy-vps.sh`
  builds it on the VPS)
- `.github/workflows/redeploy-coolify.yml` — manual fan-out redeploy of every Coolify board
- `scripts/new-board.sh` — provision a new Coolify-hosted AgentBoard instance
- `scripts/list-boards.sh` — list all boards running on the Coolify host
- `scripts/redeploy-boards.sh` — redeploy one or every Coolify board via API
- `scripts/deploy-vps.sh` — single-board install on a raw Debian/Ubuntu VPS (no Coolify)
- `internal/auth/` — the per-user token + password + session + OAuth surface (see `AUTH.md`)
- `bruno/tests/` — contract test suite (run `task test:bruno`)
- `seams_to_watch.md` — what the auth model does and doesn't protect against
