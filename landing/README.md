# AgentBoard — Landing Page

Marketing/selling site for AgentBoard. Separate from the product app (`frontend/` + Go binary) — deploys independently to a CDN (Cloudflare Pages or Vercel).

Built with **Astro + Tailwind 4** + optional React islands. Static-first for best Lighthouse/SEO.

## Dev

From the repo root (preferred, via Taskfile):

```bash
task landing:install    # npm install
task landing:dev        # Astro dev server on http://localhost:4321
task landing:build      # Build to landing/dist
task landing:preview    # Build, then serve the production output
task landing:clean      # Wipe dist/ and .astro/
task landing            # List every landing task
```

Or run tasks from inside this directory (`cd landing && task`) — the same
tasks live in `landing/Taskfile.yml` and are included into the root Taskfile
under the `landing:` namespace.

Plain npm also works:

```bash
npm install
npm run dev
npm run build
```

## Structure

```
src/
  pages/        routes (file-based)
  layouts/      <html> shells
  components/   .astro + React islands
  styles/       global.css (Tailwind entry)
public/         static assets served as-is
```

Design work lives in `src/components/` and `src/pages/`. Tailwind utilities are available everywhere via the `@import "tailwindcss"` in `src/styles/global.css`, which is imported by `layouts/Base.astro`.
