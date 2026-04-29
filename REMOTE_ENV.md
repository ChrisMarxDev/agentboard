# Remote dev environment

> **What this is.** A replayable recipe for working with the AgentBoard dev box from
> a fresh laptop or a fresh Claude Code session. Capture the *commands*; keep the
> *secrets* (IPs, SSH keys, tokens) out of the repo and on disk only.
>
> **What this is not.** A general hosting guide — see [`HOSTING.md`](./HOSTING.md)
> for Coolify-managed boards. This doc is specifically about the single Hetzner
> box used as a remote dev + Claude Code workstation.

## Topology

| Surface | Role | Lives where |
|---|---|---|
| Laptop | Drives the session — terminal, VS Code Remote, browser | macOS |
| Hetzner box (alias: `hetzner`) | Runs Claude Code, holds the repo, runs `task dev` | Ubuntu 24.04, 4 GB |
| Coolify Cloud | Orchestrates prod board containers on the same box | Hosted |

The dev process **is the dogfood**: Claude edits source on the box, `task dev`
hot-reloads, the running AgentBoard is what the dashboard URL serves. No
separate deploy.

---

## Connecting (laptop → box)

The repo never names the box's IP or key file. Each operator's laptop carries
that locally in `~/.ssh/config`. After it's set up, the only command in daily
use is:

```bash
ssh hetzner
```

### Setting up `~/.ssh/config` on a fresh laptop

Add a single host block. Replace `<box-ip>` and `<key-filename>` with the
values you've stored elsewhere (1Password, the Hetzner console, etc.):

```ssh
Host hetzner
    HostName <box-ip>
    User root
    IdentityFile ~/.ssh/<key-filename>
    IdentitiesOnly yes
    ServerAliveInterval 60
    ServerAliveCountMax 3
```

Then:

```bash
chmod 600 ~/.ssh/config
chmod 600 ~/.ssh/<key-filename>     # SSH refuses world-readable private keys
ssh hetzner "echo connected"        # smoke test
```

`IdentitiesOnly yes` is important if you have many keys — it prevents SSH from
offering every key in `~/.ssh/` and getting throttled by the server before the
right one is tried.

### Verifying without a real session

Use these flags when you want a non-interactive probe (script-friendly):

```bash
ssh -o BatchMode=yes -o ConnectTimeout=10 hetzner "whoami && hostname"
```

`BatchMode=yes` fails fast instead of prompting for a password — the right
behaviour when key auth is the only path you accept.

---

## What's installed on the box

| Tool | Why | Install path |
|---|---|---|
| Node 20 (NodeSource) | Claude Code runtime, frontend builds | `apt` |
| `@anthropic-ai/claude-code` | The agent CLI | `npm -g` |
| `git`, `build-essential` | Cloning + Go cgo builds | `apt` |
| `tmux` | Persistent sessions across SSH disconnects | `apt` |

Reproduce on a fresh box (run as root):

```bash
curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
apt-get install -y nodejs git build-essential tmux
npm install -g @anthropic-ai/claude-code
```

First `claude` run prompts an interactive browser login. The session token
lives at `/root/.claude/` after that — survives reboots, no re-auth needed.

### GitHub access from the box

The box has its own SSH identity for pulling the repo (separate from your
laptop's GitHub key). Generate it once on the box:

```bash
ssh-keygen -t ed25519 -C "hetzner-dev" -f ~/.ssh/id_ed25519 -N ""
cat ~/.ssh/id_ed25519.pub
```

Paste the public key into github.com → Settings → SSH and GPG keys. After
that, `git clone git@github.com:...` from the box works without prompts.

---

## Repo layout on the box

```
/root/agentboard/        — the working checkout
/root/.claude/           — Claude Code's local state (never commit, never copy off-box)
```

Day-to-day on the box:

```bash
cd /root/agentboard
git pull
task build       # if testing prod build
task dev         # frontend HMR + Go reloader
```

---

## Daily workflow

### Start (or resume) a Claude Code session

The trick that makes mobile useful: run Claude inside `tmux` so the session
survives disconnects and is attachable from any device.

```bash
# First time only
ssh hetzner
tmux new -s claude
cd /root/agentboard
claude
# Detach when done: Ctrl+b, d  (the session keeps running)

# Subsequent connects (laptop, phone, anywhere)
ssh hetzner
tmux attach -t claude
```

`tmux ls` lists running sessions. If you nuke a session by mistake,
`tmux new -s claude` recreates it.

### VS Code / Cursor Remote SSH

`Cmd+Shift+P` → *Remote-SSH: Connect to Host* → `hetzner`. Editor opens
against `/root/agentboard`; the integrated terminal already has shell access
to run `claude` or `task dev`. Port-forwarding for `localhost:3000` is
automatic — visible in the *Ports* tab.

### Mobile (Termius / Blink Shell)

iOS or Android. Add a host pointing at `<box-ip>` with the same key, then:

```bash
ssh hetzner
tmux attach -t claude
```

Same Claude session, same scrollback as the laptop. Watch the dashboard
itself in mobile Safari at the dev URL (lives in your DNS, not this doc).

---

## What's deliberately not in this doc

| Secret | Where it actually lives |
|---|---|
| Hetzner box IP | Laptop `~/.ssh/config` + Hetzner Cloud console |
| SSH private key | Laptop `~/.ssh/<key-filename>` (never copied off-laptop) |
| GitHub SSH key on box | `/root/.ssh/id_ed25519` on the box only |
| Claude Code token | `/root/.claude/` on the box only |
| `AGENTBOARD_AUTH_TOKEN` (per-instance) | Coolify env vars per app + the box's `task dev` env |
| Coolify API token | Laptop env (`~/.zshrc` or `direnv`); see [`HOSTING.md`](./HOSTING.md) |

If a value would let someone else reach the box or impersonate you, it does
not belong in this file. Add it to your password manager and reference its
*name* here, not the value.

---

## Recovery scenarios

**`ssh hetzner` hangs or times out.**
The box may be off, rebooted, or the IP changed. Hetzner Cloud console →
restart server. If the public IP changed, update `HostName` in
`~/.ssh/config`.

**`Permission denied (publickey)`.**
The key in `~/.ssh/config` doesn't match what the box accepts. Check
`ssh -v hetzner` to see which key it's offering. Worst case, add the key
again via the Hetzner console's "rescue" mode.

**`tmux attach -t claude` says "no sessions".**
The box rebooted (which kills tmux). Just `tmux new -s claude` and resume.
Long-term fix: a systemd unit that auto-starts the session on boot — not
yet wired.

**Claude Code says "session expired".**
Re-run `claude` once, follow the browser prompt. State persists in
`/root/.claude/` again.

**The box ran out of disk.**
Most likely culprit: Docker images from Coolify builds. `docker system prune -a`
clears unused layers. Check `df -h /` first to confirm the disk is the issue.
