# Scaling AgentBoard to a hosted offering

> **Status:** speculative design doc, not current work. The shipped product is self-host only
> (see [`CLAUDE.md`](./CLAUDE.md) and the product-vision memory). This file captures the thinking
> so future-us can act on it without re-deriving the whole argument.

Sibling docs: [`HOSTING.md`](./HOSTING.md) (today's Hetzner/Coolify deploy), [`CORE_GUIDELINES.md`](./CORE_GUIDELINES.md)
(what the product is/isn't), [`seams_to_watch.md`](./seams_to_watch.md) (security concerns we deferred).

## The model under consideration

**One AgentBoard binary per customer, on its own container, managed by a control plane.**

- No multi-tenancy inside the binary. Each tenant = separate process, separate SQLite file,
  separate auth token, separate subdomain.
- Tenants are cattle. Provisioning and updates go through a programmable control plane, not SSH.
- The install script is the primitive — it's what the control plane runs on a fresh box.

## Why this shape fits AgentBoard

Most products are a bad fit for single-tenant-per-VM because their state lives in a shared
Postgres. AgentBoard's state lives in one directory (`AGENTBOARD_PATH`) with a single SQLite
file. That makes several normally-hard things trivial:

- **Backups** = `sqlite3 .backup` + tar of `AGENTBOARD_PATH` → S3/R2. Nothing else.
- **Restore** = extract tar into the volume, start the binary. No migration dance.
- **Version skew across tenants** = a non-issue. Tenant A can run v0.4 while tenant B runs v0.5.
- **Data isolation** = physical, not logical. Cross-tenant bugs are structurally impossible.
- **Upgrades** = replace one binary, restart one process.

The single-binary/SQLite design we already committed to for self-host is the same design
that makes hosted-per-tenant cheap to build.

## Orchestrator: Coolify

For the foreseeable range (1–500 tenants), **Coolify is the right control plane.**

Why it fits:

- Architecture is identical to what we'd build: one control-plane VM that SSHes into N
  destination servers and runs Docker on them. Each tenant = one "Service" pinned to a destination.
- Built-in Traefik + Let's Encrypt handles per-tenant TLS. No hand-rolled cert renewal loop.
- Has an HTTP API — sign-up flow can `POST /services` without a human in the loop.
- Volume/env management per-service is native. Rotating a tenant's token = update env + redeploy.
- Preview-environment pattern maps 1:1 to "spin up a trial instance."

Where it breaks down:

- **~1000+ tenants** or cross-region placement logic — migrate to Nomad/k3s/ECS.
- **Fine-grained placement** (pin tenant X to dedicated hardware for compliance) is awkward.
- **Their API is less polished** than the cloud-native orchestrators. Occasional rough edges.

The migration from Coolify → Nomad later is straightforward because our unit of deployment is
already a Docker container with a volume. Nothing Coolify-specific leaks into the binary.

## Self-update mechanism

Worth shipping for self-hosters regardless of whether hosted happens; doubles as a primitive
for hosted.

Three levels, ship the first, add others behind flags later:

### 1. Check-only (ship first)

Binary polls `api.github.com/repos/.../releases/latest` on a daily timer, caches the result,
surfaces a banner in the UI: "v0.5 available — changelog." User updates however they want
(re-run install script, Coolify redeploy, `docker pull`). ~50 lines of Go, no footguns.

Also exposes it via `GET /api/version`:

```json
{"current": "0.4.1", "latest": "0.5.0", "update_available": true, "changelog_url": "..."}
```

Control plane can scrape this across the fleet to get "who's out of date" for free.

### 2. One-click in-band update

Banner gets an "Update now" button → `POST /api/admin/update` →
- download release asset to `<binary>.new`
- verify checksum (published as a sibling asset on each release)
- `rename(2)` swap
- exit with code 0; systemd `Restart=always` brings it back on the new binary
- health-check loop from a small companion script; if `/api/health` isn't 200 within 30s,
  swap back to `<binary>.old` and restart

Requires: systemd unit with `Restart=always`, write access to the binary dir, a small
supervisor wrapper for rollback.

### 3. Auto-update with channel pinning

`--update-channel=stable|beta|pinned` in the config file.
- `stable`: auto-applies after a 48h cooldown from release.
- `beta`: auto-applies immediately.
- `pinned`: never auto-updates. **This is the hosted default** — the control plane rolls
  customers forward via Coolify redeploys with new image tags, bypassing the in-band path.

## Install script requirements to be scale-ready

The single-instance `deploy-vps.sh` we're about to build *is* the primitive a control plane
will call. If we get these 12 properties right from day one, the hosted offering is ~60%
built by the time one customer runs it.

1. **Fully non-interactive.** No prompts, no tty detection. Every choice is a flag or env var.
2. **Idempotent.** Running twice = no-op. Running with `--version v0.5` on a v0.4 host = upgrade.
   One script, not install-vs-upgrade branches.
3. **Version-pinned inputs.** Takes `--version v0.5.2` (resolves to a GitHub release asset),
   never `:latest`. Control plane rolls customers forward deterministically.
4. **Structured output on stdout, narrative on stderr.** Prints JSON on success:
   ```json
   {"url":"https://abc.agentboard.app","version":"0.5.0","token":"...","instance_id":"abc"}
   ```
   Control plane parses stdout; humans can still `| jq`.
5. **Explicit exit codes.** Distinct codes for DNS-not-resolvable, disk-full, port-busy,
   cert-issuance-failed, healthcheck-timeout. Control plane decides retry vs. human per code.
6. **Cloud-init compatible.** Works as EC2 user-data / Hetzner cloud-init too, not just over
   SSH. Lets the control plane skip SSH entirely on provision.
7. **Config-as-file.** Writes `/etc/agentboard/env`; systemd (`EnvironmentFile=`) reads it.
   Rotating a token = rewrite file + `systemctl reload`. No redeploy for config.
8. **Backups baked in.** Installs a systemd timer that `sqlite3 .backup` + tars
   `AGENTBOARD_PATH` + uploads to S3/R2 on a configurable schedule.
9. **Health check before declaring success.** Polls `/api/health` with timeout. Doesn't exit 0
   until the app is actually serving 200s.
10. **Observability hook.** Optional `--callback-url` hit on phase transitions (installed,
    started, healthy, failed). Or structured stdout the host agent tails. Either way: control
    plane never needs to SSH in to know instance state.
11. **Supervision.** systemd unit with `Restart=always`, `RestartSec=5s`, journal-based logs.
    The OS handles crashes; the control plane only gets paged when systemd gives up.
12. **Unattended OS upgrades.** Configures `unattended-upgrades` (Debian/Ubuntu). Don't want
    to manually patch 200 boxes for the next OpenSSL CVE.

Get 1–7 right = control-plane-callable. Add 8–12 = survivable in production for the first
50 customers without a 3am pager.

## Economics (rough)

| Provider | Smallest instance | +IPv4 | +Storage | Realistic total |
|---|---|---|---|---|
| **Hetzner Cloud** (CX22) | €3.79/mo | included | included | **~$5/mo** |
| **Hetzner Cloud** (CAX11, ARM) | €3.29/mo | included | included | **~$4/mo** |
| DigitalOcean droplet | $4/mo | included | included | ~$5/mo |
| AWS `t4g.nano` | ~$3/mo | $3.60/mo | ~$1/mo | ~$8/mo |

**Hetzner is the right target** for the initial hosted offering. Clean API, same €/month
whether the instance is busy or idle, generous free egress. AWS is the wrong cloud for this
shape — the IPv4 tax alone doubles the floor.

Priced at $20/mo with a CX22 underneath: ~$15/mo gross margin before ops overhead. At 100
tenants that's ~$1.5k/mo to cover the control-plane VM, backups bucket, domain, and time.

## Known unknowns to resolve before committing

Anything below changes the architecture enough that we shouldn't commit to "single-tenant
VPS" until we have an answer:

- **SLA.** "Best-effort, restore-from-backup on incident" = this model works. "99.9% with
  automatic failover" = you need standby replicas, and SQLite doesn't replicate cleanly. That
  would push toward a shared Postgres layer (which kills half the thesis) or Litestream-style
  streaming backups (which solves backups but not instant failover).
- **Cross-tenant data.** Does hosted ever need cross-tenant features? Org-level dashboards,
  shared skill marketplace, cross-workspace search? If yes, single-tenant breaks — you need a
  shared service above the tenants. If no, the model is cleanest.
- **Custom domains.** If every customer wants `{their}.domain.com` pointing to us, Coolify
  can handle it but the TLS issuance path gets more fragile (ACME over HTTP-01 per custom
  domain, and customers forget DNS).
- **Region pinning / data residency.** If EU customers must stay in EU, we need multi-region
  Hetzner (they have EU + US) + a routing layer that knows where each tenant lives. Doable
  but adds a placement concern to the control plane.

## Alternative we're consciously not taking

**Shared-container fleet** (Nomad/k3s/ECS bin-packing 20 tenants per VM):

- 3–5× better unit economics.
- Centralized logs/metrics/tracing free.
- Much faster provision (seconds, not minutes).
- But: noisy-neighbor at CPU level, less clean "your data is on a dedicated box" pitch,
  harder per-tenant compliance/SLA story, higher blast radius on orchestrator bugs.

Revisit this when: (a) margin pressure from cheap pricing tier, or (b) tenant count pushes
Coolify's ceiling, or (c) we want instant provisioning for trials.

## What lands from the single-instance work

The VPS install script we're building for self-host naturally becomes the scale primitive if
we design it to the 12-point list above. Nothing about the single-instance work is throwaway —
Coolify calls the exact same Dockerfile, the binary runs in the exact same mode, the volume
layout is the same. Hosted is this plus a control plane plus a billing loop.

**Translation: we are not shooting ourselves in the foot by shipping the minimal version first.**
We're front-loading the only part that's load-bearing for either path.
