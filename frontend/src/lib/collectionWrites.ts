// Collection writers — the editing path for components that mutate
// rows (Sheet, Kanban). The view broker resolves a `source=` to one of
// three places (frontmatter splat, folder of pages, file-store
// collection); the matching write surface depends on the source shape.
//
// Trailing-slash sources (`tasks/`) are folder-of-pages collections.
// Each row is an .md file under `content/<source>/<id>.md`; mutations
// flow through `/api/content/*`.
//
// Bare sources (`store.key`) are file-store collections. Mutations go
// through `/api/data/<source>/<id>` against the files-first store.
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
    return apiFetch(`/api/content/${path}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ frontmatter_patch: patch }),
    })
  }
  return apiFetch(
    `/api/data/${encodeURIComponent(source)}/${encodeURIComponent(id)}`,
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    },
  )
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
    return apiFetch(`/api/content/${path}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'text/markdown' },
      body: md,
    })
  }
  return apiFetch(`/api/data/${encodeURIComponent(source)}`, {
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
    return apiFetch(`/api/content/${path}`, { method: 'DELETE' })
  }
  return apiFetch(
    `/api/data/${encodeURIComponent(source)}/${encodeURIComponent(id)}`,
    { method: 'DELETE' },
  )
}
