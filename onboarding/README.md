# Onboarding an agent to an AgentBoard workspace

The §15 implementation. The human writes one sentence; the workspace
teaches the agent everything else.

## What's in this directory

- [`agent-prompt.txt`](./agent-prompt.txt) — the canonical bootstrap
  prompt. Fill in `<CLONE_URL>`, `<TOKEN>`, `<HOST>`, `<WORKSPACE>` and
  paste the whole thing into the agent runtime (Claude Code, cowork,
  Anthropic Workbench, any agent that can shell out to `git`).

## How an operator uses it

The whole flow from "I have a fresh agent runtime open" to "the
agent is productive in our workspace":

1. **Mint a token.** From the AgentBoard host:

       agentboard admin invite --path <project-path> --role bot --label <agent-name>

   Open the printed `/invite/<id>` URL in a browser, claim a username
   for the bot (e.g. `cowork`, `qa-bot`, `releases`), capture the
   bearer token it shows you.

2. **Fill the template.** Substitute four placeholders in
   `agent-prompt.txt`:

   | Placeholder      | Example                                       |
   |------------------|-----------------------------------------------|
   | `<CLONE_URL>`    | `https://agentboard.hextorical.com/git/dogfood.git` |
   | `<HOST>`         | `agentboard.hextorical.com`                   |
   | `<WORKSPACE>`    | `dogfood`                                     |
   | `<TOKEN>`        | `ab_…43-chars…`                               |

3. **Paste into the agent.** The whole filled-in text becomes the
   first user message. Nothing else. No "you are an AI assistant…",
   no MCP registration steps, no per-tool permission notes. The agent
   already knows how to be an AI assistant; the workspace teaches it
   how to be useful here.

4. **Watch it work.** The agent runs the four-line wire-up, reads
   `README.md`, follows the chain to `skills/agentboard/SKILL.md`,
   gets oriented. From that point on it's collaborating against the
   workspace like a human would.

## Why the prompt looks like that

The shape is load-bearing. Each clause is doing a job:

- *"AgentBoard is a workspace at `<CLONE_URL>`."* — one sentence.
  Sets the URL once. Everything else in the prompt could be deleted
  and the agent could still find its way; the URL is the irreducible
  bootstrap.

- *"Your current directory IS the workspace — don't clone it somewhere
  else."* — the "your coworker is here" rule. Without this an agent
  will reflexively `git clone /tmp/agentboard` and the human loses
  cwd-locality. (We caught this in the first cowork test.)

- *The four-line `git init && remote add && fetch && checkout -B`* —
  handles empty AND non-empty cwd. `git clone <url> .` would be one
  line but only works in an empty dir; this works in either.

- *"Now README.md is right here in your cwd. Read it…"* — the
  chain-starter. The README points at the SKILL, the SKILL teaches the
  protocol. We don't enumerate any of that here.

- *"AgentBoard is the source of truth; my instructions stop here."* —
  prevents the agent from treating this prompt as the *full* contract.
  Updates to conventions ship to the workspace; the prompt never
  changes.

- *Four numbered questions* — give the agent a structured handoff so
  the human gets back a useful summary instead of "ok, I'm ready."

## When to change the prompt

Almost never. Edit the workspace's `README.md` or
`skills/agentboard/SKILL.md` instead — that's where every convention,
tool list, and recipe lives. Per **CORE_GUIDELINES §15**: push
knowledge into the workspace, not the prompt.

The prompt template should change when the *substrate itself* changes:

- The wire-up commands change (e.g. if we add a non-git transport).
- The four questions stop being useful for catching workspace drift.
- We add a runtime-detection step (`git` vs MCP fallback) the agent
  shouldn't have to figure out.

## See also

- [`spec.md` §1.5](../spec.md) — the bootstrap contract.
- [`CORE_GUIDELINES.md` §15](../CORE_GUIDELINES.md) — the principle.
- [`test/dogfood/`](../test/dogfood/) — the automated §15 test that
  validates a fresh agent can run this prompt successfully.
- Verified runs are captured in `test/dogfood/results/`.
