# Cowork first-run — verification of the bootstrap loop

**Date:** 2026-05-13
**Runtime:** Anthropic Cowork (not local `claude --print`)
**Workspace:** `dogfood` at `https://agentboard.hextorical.com/git/dogfood.git`
**Bot user:** `@claudecowork`
**Verdict:** ✓ PASSED — bootstrap chain runs cleanly against a runtime
that has never seen AgentBoard before.

The local `task test:dogfood` proves the loop works against an
ephemeral instance with a clean `claude --bare` session. This is the
external counterpart: a separate runtime, against the *live* dogfood
URL, running the prompt template in `onboarding/agent-prompt.txt`
that the operator pasted.

---

## What the agent reported back

(Verbatim transcript from the cowork session, pasted into the conversation.)

> The new flow works — clone, README, SKILL, examples, then poke around. Here's what I found, then the four answers.
>
> **The chain, traced.** README.md → skills/agentboard/SKILL.md → skills/agentboard/examples.md. Each one resolves cleanly, the references match real files, and the SKILL is now self-contained instead of pointing at endpoints that don't exist (which was my first-pass complaint). Auth via token-in-URL worked for clone in one shot.
>
> **1. What kind of workspace this is.** A git-backed AgentBoard workspace — a small repo where a mixed team of humans and agents collaborates by pushing commits to main. Substrate is git; the SPA mirrors the working tree, so a `git push` is the live update. There's a 7-tool MCP fallback (workspaces, pull, propose, resolve_conflict, subscribe, grab, fire_event) for runtimes that can't shell out — note this is leaner than the 10-tool surface I worked with previously; the read tools collapsed into `pull`, the write tools collapsed into `propose`. Push-to-main by default, no PR branches required. Conflicts are resolved with the standard `<<<<<<<` markers.
>
> The repo already contains carry-over content from earlier dogfooding (vet/, roasters/, the two binaries I uploaded), plus the bootstrap README and skill. History is short — 4 commits: initial migration, README, SKILL refresh, examples refresh.
>
> **2. What open work it surfaces.** Three signals, in increasing order of importance:
>
> *Explicit tasks* — `roasters/tasks/` has three cards:
> - `restock-yir` — todo, due 2026-05-02
> - `calibrate-grinder-2` — doing, due 2026-04-30 (overdue, today is 2026-05-13)
> - `post-may-menu` — blocked on photos from @ada, due 2026-05-01 (overdue)
>
> No top-level `tasks/` folder exists — `roasters/tasks/` is the only collection following the task shape. The README points at "whichever folder this workspace is using as its task queue," but the SKILL doesn't pin one down for this workspace, so the answer is "the only one that exists."
>
> *Convention drift that the SKILL flags as the anti-pattern.* `roasters.md` is full of `source="roasters/data/<key>"` references, and `roasters/data/` has 12 leaves backing them. The SKILL explicitly calls this out: "scalars live inline on the page that displays them. Don't write to `data/<key>` or invent parallel namespaces." This is the very mistake I made in my last conversation. Meanwhile `vet.md` was already migrated to inline values — so the intended end-state is plainly visible across the same repo.
>
> *Smaller hygiene gaps.* `vet/patients/imea7vayj6rpi6ad.md` is a UI-test garbage record (title: "", breed: asdsd, etc.) — should be deleted. The README also tells humans to edit `index.md`, but no `index.md` exists at the root yet.
>
> **3. Git vs MCP — I'd go git.** For this runtime, git, almost certainly. Reasons:
> - I've already proven the clone works end-to-end with token-in-URL Basic auth. The same URL form will accept a push, which means the credential-prompt issue I hit on plain GitHub doesn't apply here.
> - Git gives me real branches, atomic commits, a meaningful commit message, history that maps to a SHA, and the conflict machinery is the same one I use every day. The SKILL says the conflict markers are the repair manual, and that's true.
> - MCP `agentboard_propose` is whole-file writes per call — there's no field-level patch (the examples doc even calls this out: "MCP doesn't have a true append primitive yet"). Git's append-without-conflict on `.ndjson` streams is a real superpower for activity logs.
> - The SKILL itself says "preferred when your runtime can shell out." It can.
>
> I'd reach for MCP only if (a) I needed to fire an event (`agentboard_fire_event` has no git equivalent), (b) I wanted to subscribe to live workspace events, or (c) the workspace ended up in some half-pushed state where `agentboard_resolve_conflict` is the cleaner exit.
>
> **4. What I'd do first if you asked me to ship.** The single highest-value first move is fixing the §14 convention drift on `roasters.md` — and it has the nice side-effect of cleaning up the very mistake I made the last time. Concretely:
>
> ```bash
> git checkout -b feature/inline-roasters-scalars
> # Rewrite roasters.md so every <Counter|Metric|Status|Progress|RichText source="roasters/data/X" />
> # is replaced with an inline-value form, with the scalar in roasters.md's own frontmatter.
> # Hoist the array-shaped leaves (sales_7d, sales_by_drink, top_beans, inventory) into the
> # pages that actually render them — roasters/inventory.md owns them now.
> git rm -r roasters/data
> git commit -m "Migrate roasters scalars to inline (CORE_GUIDELINES §14)"
> git push origin HEAD:main
> ```
>
> One scoped commit, ~14 file changes (1 rewrite + 1 directory delete + maybe `roasters/inventory.md` updated), no behavioral regression for the dashboard because the source-to-inline swap is mechanical. If the push were rejected I'd `git pull --rebase`, resolve, push again.
>
> Want me to actually do it? I'm staged to run, but I won't push without your go-ahead since this is your repo's main.

---

## What this run tells us

- **The bootstrap chain works against a runtime AgentBoard has never
  met.** Cowork is the canary for "would any reasonable agent figure
  this out from one sentence?" It did.
- **The 7-tool MCP surface is correctly taught.** The agent named the
  exact tools, noted what's gone vs v0.13, and chose its lane
  (git vs MCP) by reading the SKILL.
- **The agent independently identified a real workspace defect:**
  `roasters/data/` is a §14 violation (the exact same anti-pattern the
  original principle was written to forbid). The §15 test caught a §14
  drift — exactly the kind of cross-principle validation we hoped this
  loop would surface.
- **The agent paused before pushing destructive changes to main**
  ("Want me to actually do it? I won't push without your go-ahead").
  That's the right shape — agents that pause for confirmation on
  irreversible work are agents that scale.

## What we should do with this finding

Two things worth tracking, separately:

1. **`roasters/data/` violates §14.** The agent's proposed fix is the
   correct one. Either approve the cowork run or do the equivalent
   migration in a follow-up commit on the repo. Either way the
   roasters dashboard ends up with inline scalars matching the vet
   dashboard.

2. **Hygiene drift in `vet/patients/imea7vayj6rpi6ad.md`**: empty
   title, gibberish breed. A leftover UI-test artifact. Drop it.

Neither is a §15 bootstrap-test failure — both are real workspace
state defects the §15 bootstrap test surfaced.
