---
source: Thariq Shihipar, "Why I prefer HTML over Markdown for agent output"
read_date: 2026-05-13
status: under consideration — should AgentBoard's MDX-based spec/plan workflow shift to HTML?
---

# HTML vs. Markdown for Agent Output — Notes

The pitch: as agents do more of the writing, the value of "human-editable plaintext" drops and the value of "human-readable rich document" rises. HTML wins on every axis except token cost and diff noise.

## The case for HTML

| Axis | Markdown | HTML |
|---|---|---|
| Information density | Headers, lists, tables, fenced code | + CSS, SVG, `<script>`, canvas, images, absolute positioning, interactive widgets |
| Reading >100 lines | "I won't" | Tabs, columns, collapsibles, illustrations make long docs navigable |
| Sharing | Attach a file; recipient needs a renderer | Upload anywhere, send a link, opens in any browser |
| Two-way interaction | None | Sliders, knobs, drag-and-drop, "copy as prompt" buttons |
| Token cost | Cheap | 2–4× more expensive to generate |
| Diff in git | Clean | Noisy |
| Visual taste | Renderer's default | Whatever you make it (use a design-system reference file) |

## Why this matters specifically when an agent is writing

The original argument for Markdown was "humans should be able to edit this in any text editor." That argument decays the moment the human stops editing the doc directly and starts prompting the agent to edit it. At that point the only remaining job of the format is to be **read** — and HTML reads better.

Corollary: the argument *for* Markdown is strongest when the doc is short, frequently hand-edited, version-controlled, and reviewed in diff form. Argument for HTML is strongest when the doc is long, agent-generated, read once or twice, and shared with people who won't clone the repo.

## Use cases the author calls out

1. **Specs / planning / exploration** — instead of a single `PLAN.md`, generate a web of HTML files: brainstorm grid, mockups, code snippets, then a final implementation plan. Pass them all into a fresh session for implementation.
2. **Code review** — render the diff with inline annotations, color-coded severity, flowcharts. Attach to every PR. Beats GitHub's default diff view.
3. **Design & prototypes** — HTML is the lingua franca for design even when the target is React/Swift/etc. Add sliders so you can tune without re-prompting.
4. **Reports / research / learning** — synthesis across Slack + codebase + git + web → one readable HTML explainer with SVG diagrams.
5. **Throwaway editors** — purpose-built single-HTML-file UI for one piece of data (drag tickets between columns, edit a feature-flag config, tune a system prompt with live preview). Always ends with a "copy as JSON" / "copy as prompt" button.

## How to start

Don't build a `/html` skill. Just prompt: *"make an HTML file"* or *"make an HTML artifact"*. The skill comes later if at all — first learn what you actually want the artifact to do.

For taste/styling: have Claude read your codebase and generate one `design-system.html` reference file, then point future generations at it.

## How to view / share

- Local: ask Claude to `open` the file in your default browser.
- Shared: upload to S3 (or any static host), send the link.

## Caveats the author admits

- Tokens: 2–4× generation cost. With Opus 4.7's 1MM context, not noticeable in-window — but it is noticeable on the bill.
- Diffs: HTML diffs are awful. If the doc lives in git and gets reviewed in PRs, this hurts.
- Generation time: 2–4× slower than Markdown.

## Implications for AgentBoard

AgentBoard currently treats MDX as the doc format — pages are MDX files, components are JSX, the watcher feeds the SPA from a working-tree mirror. The pivot isn't a wholesale swap to HTML, because:

- **The dashboard already renders MDX with React components.** The viewer-side richness Thariq wants from HTML, we get from MDX + components. A `<MetricCard>` is richer than a hand-rolled `<div style>`.
- **Diffs still matter.** Spec docs (`spec.md`, `ROADMAP.md`, `CORE_GUIDELINES.md`) live in git and get reviewed in PRs. HTML would wreck the review surface.
- **But:** for one-shot artifacts the agent produces *for the human reader and never again* — incident reports, "explain this PR", design explorations, feature explainers — generating HTML next to the MDX page makes sense. The MDX page is the canonical state; the HTML is the readable companion.

Concretely worth trying:
- When an agent writes a long-form explainer (>200 lines), generate both the MDX page (for the dashboard) and a standalone `.html` file (for sharing externally / reading on mobile / sending to non-technical stakeholders — see the §5 audience the project optimizes for).
- Keep specs and roadmap as `.md` — they're reviewed in diffs and edited often.
- Skip the "throwaway editor" pattern for now; the dashboard's built-in components cover most of those use cases.

## Verdict (provisional)

Don't switch the dashboard's storage format. Do start producing companion HTML artifacts for long-form, read-once content aimed at humans outside the repo. Revisit after a month of usage.
