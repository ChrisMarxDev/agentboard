// Collection writers — the editing path for components that mutate
// rows (Sheet, Kanban). The view broker resolves a `source=` to one of
// three places (frontmatter splat, folder of pages, file-store
// collection); the matching write surface depends on the source shape.
//
// Trailing-slash sources (`tasks/`) are folder-of-pages collections.
// Each row is an .md file under `content/<source>/<id>.md`; mutations
// flow through `/api/<source>/<id>` (the unified namespace; spec §5).
//
// Bare sources (`store.key`) are file-store collections. Mutations go
// through `/api/<source>/<id>` against the files-first store. The
// dispatcher routes by lookup so the same URL covers both cases.
//
// Frontmatter-splat sources (a key that the broker pulled from the
// owning page's YAML) cannot be mutated per-row from a component —
// the write would have to PATCH the owning page's frontmatter, and
// the component doesn't know which page it lives on. Hoist each row
// to its own .md and switch to a folder source if you need editing.

import { apiFetch } from './session'

export function isFolderSource(source: string): boolean {
  return source.endsWith('/')
}

function folderPath(source: string): string {
  // Drop trailing slash; keep any leading segments. Encode each path
  // segment so embedded special chars don't collide with chi's wildcard.
  return source.replace(/\/+$/, '').split('/').map(encodeURIComponent).join('/')
}

export async function patchCollectionItem(
  source: string,
  id: string,
  patch: Record<string, unknown>,
): Promise<Response> {
  if (isFolderSource(source)) {
    const path = folderPath(source) + '/' + encodeURIComponent(id)
    return apiFetch(`/api/${path}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ frontmatter_patch: patch }),
    })
  }
  return apiFetch(
    `/api/${encodeURIComponent(source)}/${encodeURIComponent(id)}`,
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    },
  )
}

// patchPageFrontmatter targets a single page (not a row in a folder
// collection) — used by the Kanban to persist column-config edits
// to the page's own frontmatter.columns array. Takes the page path,
// not a source folder path.
export async function patchPageFrontmatter(
  pagePath: string,
  patch: Record<string, unknown>,
): Promise<Response> {
  const cleaned = pagePath.replace(/^\/+/, '').replace(/\.md$/, '')
  const encoded = cleaned.split('/').map(encodeURIComponent).join('/')
  return apiFetch(`/api/${encoded}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ frontmatter_patch: patch }),
  })
}

// patchCollectionItemBody replaces the prose body of a folder-collection
// row's .md file without touching its frontmatter. Used by the Kanban
// card detail's description editor — descriptions are the actual MDX
// body, not a frontmatter `body` field, so a free-form rewrite is the
// natural shape.
export async function patchCollectionItemBody(
  source: string,
  id: string,
  body: string,
): Promise<Response> {
  if (!isFolderSource(source)) {
    throw new Error('patchCollectionItemBody only works on folder sources')
  }
  const path = folderPath(source) + '/' + encodeURIComponent(id)
  return apiFetch(`/api/${path}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ body }),
  })
}

// readCollectionItemBody fetches the prose body of a folder-collection
// row. The Kanban detail pane lazy-loads this on card open — the kanban
// row itself only carries frontmatter (per folderChildren), so the body
// is a separate round-trip.
export async function readCollectionItemBody(
  source: string,
  id: string,
): Promise<string> {
  if (!isFolderSource(source)) return ''
  const path = folderPath(source) + '/' + encodeURIComponent(id)
  const res = await apiFetch(`/api/${path}`, {
    headers: { Accept: 'application/json' },
  })
  if (!res.ok) return ''
  const j = (await res.json()) as { source?: string }
  return extractBody(j.source ?? '')
}

// extractBody returns everything after the YAML frontmatter block of an
// MDX source. The frontmatter is delimited by `---` lines per the
// project's content convention; if the source has no frontmatter we
// return it verbatim.
export function extractBody(source: string): string {
  if (!source.startsWith('---')) return source
  // Find the closing fence. The first `---` line after a newline ends
  // the frontmatter block; everything after that line is body.
  const m = source.match(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/)
  if (!m) return source
  return source.slice(m[0].length).replace(/^\s+/, '')
}

export async function createCollectionItem(
  source: string,
  id: string,
  row: Record<string, unknown>,
): Promise<Response> {
  if (isFolderSource(source)) {
    const path = folderPath(source) + '/' + encodeURIComponent(id)
    // Assemble minimal MDX: YAML frontmatter, no body. Values are
    // JSON-stringified — that's a valid YAML scalar form for any
    // primitive and avoids escaping headaches with arbitrary strings.
    const fm = Object.entries(row)
      .map(([k, v]) => `${k}: ${JSON.stringify(v ?? '')}`)
      .join('\n')
    const md = `---\n${fm}\n---\n`
    return apiFetch(`/api/${path}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'text/markdown' },
      body: md,
    })
  }
  return apiFetch(`/api/${encodeURIComponent(source)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(row),
  })
}

export async function deleteCollectionItem(
  source: string,
  id: string,
): Promise<Response> {
  if (isFolderSource(source)) {
    const path = folderPath(source) + '/' + encodeURIComponent(id)
    return apiFetch(`/api/${path}`, { method: 'DELETE' })
  }
  return apiFetch(
    `/api/${encodeURIComponent(source)}/${encodeURIComponent(id)}`,
    { method: 'DELETE' },
  )
}
