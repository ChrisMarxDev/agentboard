import { useEffect, useMemo, useRef, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useData } from '../../hooks/useData'
import { useDataContext } from '../../hooks/DataContext'
import { isPublicMode } from '../../lib/session'
import {
  createCollectionItem,
  deleteCollectionItem,
  patchCollectionItem,
} from '../../lib/collectionWrites'
import { beaconError, resetBeacon } from '../../lib/errorBeacon'

// Sheet renders an array-of-objects data key as an editable grid.
// Cells are inline-editable for authed users; in public/share mode
// the grid is read-only.
//
// The source determines where writes land:
//
//   <Sheet />                                — auto-attach: rows are
//     children of the rendering page's own folder. Equivalent to
//     `<Sheet source="<this-page-path>/" />`. Use this on a folder-
//     index page to bind the grid to its siblings.
//
//   <Sheet source="tasks/" />                — folder collection
//     → PATCH /api/content/tasks/<id>      (frontmatter merge)
//     → PUT   /api/content/tasks/<newid>   (create as MDX doc)
//     → DELETE /api/content/tasks/<id>
//
//   <Sheet source="store.key" />              — file-store collection
//     → PATCH /api/data/store.key/<id>
//     → POST  /api/data/store.key
//     → DELETE /api/data/store.key/<id>
//
// Sources that resolve from a page's own frontmatter (`<Sheet source="roster" />`
// where `roster` is an array literal in YAML) read fine but cannot be edited
// here — there is no per-row write endpoint for inline-array fields. To make
// such a sheet editable, hoist each row to its own .md file under the page's
// folder and reference the folder with a trailing slash, or omit the prop.
//
// Design notes:
//  - Columns are inferred from the first row's keys unless `columns`
//    is passed explicitly. `id` is always first if present and hidden
//    from edit (it's the row key server-side).
//  - Cells are text for v0. We display numbers as-is; editing a number
//    cell returns a number on save, not a string.
//  - Deletes happen per-row behind a one-button confirm (same pattern
//    as kanban cards).
//  - Rows without `id` are rendered but can't be edited/deleted —
//    same constraint as kanban. The "Add row" button creates rows
//    with a fresh uuid-style id.

interface SheetProps {
  /** Folder collection path (e.g. "tasks/"). Omit to auto-attach to
   *  the rendering page's own folder. */
  source?: string
  columns?: string[]
  title?: string
}

type RowRecord = Record<string, unknown>

export function Sheet({ source, columns, title }: SheetProps) {
  const ctx = useDataContext()
  // Auto-attach: if no `source` prop, treat the rendering page's own
  // folder as the implicit collection. Mirrors Kanban — the broker's
  // ref-extractor adds the same folder key to scope when it sees
  // `<Sheet>` without source, so the bundle is already populated.
  const effectiveSource = source ?? (ctx.path ? ctx.path + '/' : '')
  const { data, loading } = useData(effectiveSource)
  const readOnly = isPublicMode()

  // Optimistic overlay: a short-lived map of (rowId → patched values)
  // and (rowId → deleted) so the UI reflects a just-dispatched write
  // without waiting for the SSE round-trip. Cleared as soon as the
  // underlying data mirrors the change.
  const [pendingEdits, setPendingEdits] = useState<Record<string, RowRecord>>({})
  const [pendingDeletes, setPendingDeletes] = useState<Record<string, true>>({})

  const rowsRaw: RowRecord[] = useMemo(() => {
    if (!Array.isArray(data)) return []
    return data as RowRecord[]
  }, [data])

  // Apply optimistic overlay.
  const rows = useMemo(() => {
    return rowsRaw
      .filter(r => {
        const id = r.id != null ? String(r.id) : null
        return !(id && pendingDeletes[id])
      })
      .map(r => {
        const id = r.id != null ? String(r.id) : null
        if (id && pendingEdits[id]) {
          return { ...r, ...pendingEdits[id] }
        }
        return r
      })
  }, [rowsRaw, pendingEdits, pendingDeletes])

  // Clear overlay entries once the underlying data catches up.
  useEffect(() => {
    // Edit overlay: drop any row whose merged data already equals the
    // rowsRaw row.
    setPendingEdits(prev => {
      const next = { ...prev }
      let changed = false
      for (const r of rowsRaw) {
        const id = r.id != null ? String(r.id) : null
        if (!id || !next[id]) continue
        const merged = next[id]
        const matches = Object.keys(merged).every(k => deepEqual(merged[k], r[k]))
        if (matches) {
          delete next[id]
          changed = true
        }
      }
      return changed ? next : prev
    })
    setPendingDeletes(prev => {
      const next = { ...prev }
      let changed = false
      for (const id of Object.keys(next)) {
        if (!rowsRaw.some(r => r.id != null && String(r.id) === id)) {
          delete next[id]
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [rowsRaw])

  // Column derivation. id first if present, then every other key from
  // the first row (or the explicit `columns` prop).
  const cols = useMemo(() => {
    if (columns && columns.length > 0) return columns
    if (rows.length === 0) return []
    const first = rows[0]
    const keys = Object.keys(first)
    const hasId = keys.includes('id')
    const rest = keys.filter(k => k !== 'id')
    return hasId ? ['id', ...rest] : rest
  }, [columns, rows])

  async function patchRow(id: string, col: string, raw: string) {
    // Optimistic: apply locally first.
    const parsed = coerceValue(raw)
    setPendingEdits(prev => ({
      ...prev,
      [id]: { ...(prev[id] ?? {}), [col]: parsed },
    }))
    try {
      const res = await patchCollectionItem(effectiveSource, id, { [col]: parsed })
      if (!res.ok) throw new Error(`patch ${effectiveSource}/${id} → ${res.status}`)
      resetBeacon('Sheet', effectiveSource)
    } catch (e) {
      // Revert optimistic overlay for this cell.
      setPendingEdits(prev => {
        const row = { ...(prev[id] ?? {}) }
        delete row[col]
        const next = { ...prev }
        if (Object.keys(row).length === 0) delete next[id]
        else next[id] = row
        return next
      })
      beaconError({
        component: 'Sheet',
        source: effectiveSource,
        error: e instanceof Error ? e.message : 'row edit failed',
      })
    }
  }

  async function appendRow() {
    const newId = generateId()
    // Seed a row with empty strings in every column so the optimistic
    // overlay has something to show before the server confirms.
    const blank: RowRecord = { id: newId }
    for (const c of cols) if (c !== 'id') blank[c] = ''
    try {
      const res = await createCollectionItem(effectiveSource, newId, blank)
      if (!res.ok) throw new Error(`create ${effectiveSource}/${newId} → ${res.status}`)
      resetBeacon('Sheet', effectiveSource)
    } catch (e) {
      beaconError({
        component: 'Sheet',
        source: effectiveSource,
        error: e instanceof Error ? e.message : 'append failed',
      })
    }
  }

  async function deleteRow(id: string) {
    setPendingDeletes(prev => ({ ...prev, [id]: true }))
    try {
      const res = await deleteCollectionItem(effectiveSource, id)
      if (!res.ok) throw new Error(`delete ${effectiveSource}/${id} → ${res.status}`)
      resetBeacon('Sheet', effectiveSource)
    } catch (e) {
      setPendingDeletes(prev => {
        const next = { ...prev }
        delete next[id]
        return next
      })
      beaconError({
        component: 'Sheet',
        source: effectiveSource,
        error: e instanceof Error ? e.message : 'delete failed',
      })
    }
  }

  if (loading) {
    return (
      <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>
        Loading…
      </div>
    )
  }

  if (!Array.isArray(data)) {
    return (
      <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>
        <code>{effectiveSource || '(no source)'}</code> is not an array — <code>&lt;Sheet&gt;</code> expects an array of objects.
      </div>
    )
  }

  return (
    <div
      className="my-4 rounded-lg border overflow-hidden"
      style={{ borderColor: 'var(--border)' }}
    >
      {title && (
        <div
          className="px-3 py-2 text-xs font-semibold uppercase tracking-wide border-b"
          style={{ color: 'var(--text-secondary)', borderColor: 'var(--border)' }}
        >
          {title}
        </div>
      )}
      <div style={{ overflowX: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.875rem' }}>
          <thead>
            <tr style={{ background: 'var(--bg-secondary)' }}>
              {cols.map(c => (
                <th
                  key={c}
                  style={{
                    padding: '0.5rem 0.75rem',
                    textAlign: 'left',
                    fontSize: '0.7rem',
                    fontWeight: 600,
                    textTransform: 'uppercase',
                    letterSpacing: '0.04em',
                    color: 'var(--text-secondary)',
                    borderRight: '1px solid var(--border)',
                  }}
                >
                  {c}
                </th>
              ))}
              {!readOnly && (
                <th style={{ width: 28 }} aria-hidden />
              )}
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td
                  colSpan={cols.length + (readOnly ? 0 : 1)}
                  style={{
                    padding: '1.25rem',
                    textAlign: 'center',
                    color: 'var(--text-secondary)',
                    fontStyle: 'italic',
                  }}
                >
                  Empty.
                  {!readOnly && (
                    <>
                      {' '}
                      <button
                        type="button"
                        onClick={() => void appendRow()}
                        style={{
                          background: 'transparent',
                          border: 'none',
                          color: 'var(--accent)',
                          textDecoration: 'underline',
                          cursor: 'pointer',
                          fontStyle: 'normal',
                        }}
                      >
                        Add the first row.
                      </button>
                    </>
                  )}
                </td>
              </tr>
            ) : (
              rows.map((row, i) => {
                const id = row.id != null ? String(row.id) : null
                return (
                  <tr key={id ?? i} style={{ borderTop: '1px solid var(--border)' }}>
                    {cols.map(c => (
                      <Cell
                        key={c}
                        value={row[c]}
                        readonly={readOnly || !id || c === 'id'}
                        onSave={next => id && patchRow(id, c, next)}
                      />
                    ))}
                    {!readOnly && (
                      <td style={{ width: 28, textAlign: 'center', verticalAlign: 'middle' }}>
                        {id && (
                          <button
                            type="button"
                            aria-label="Delete row"
                            onClick={() => {
                              if (confirm(`Delete row ${id}?`)) void deleteRow(id)
                            }}
                            style={{
                              background: 'transparent',
                              border: 'none',
                              color: 'var(--text-secondary)',
                              cursor: 'pointer',
                              padding: 4,
                            }}
                          >
                            <Trash2 size={12} />
                          </button>
                        )}
                      </td>
                    )}
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>
      {!readOnly && rows.length > 0 && (
        <div
          className="px-3 py-2 border-t"
          style={{ borderColor: 'var(--border)' }}
        >
          <button
            type="button"
            onClick={() => void appendRow()}
            className="inline-flex items-center gap-1 text-xs"
            style={{
              background: 'transparent',
              border: '1px solid var(--border)',
              borderRadius: '0.375rem',
              padding: '0.25rem 0.5rem',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
            }}
          >
            <Plus size={12} />
            Add row
          </button>
        </div>
      )}
    </div>
  )
}

interface CellProps {
  value: unknown
  readonly: boolean
  onSave: (next: string) => void
}

function Cell({ value, readonly, onSave }: CellProps) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(stringifyValue(value))
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!editing) setDraft(stringifyValue(value))
  }, [value, editing])

  useEffect(() => {
    if (editing) inputRef.current?.focus()
  }, [editing])

  if (editing && !readonly) {
    const commit = () => {
      const before = stringifyValue(value)
      if (draft !== before) onSave(draft)
      setEditing(false)
    }
    const cancel = () => {
      setDraft(stringifyValue(value))
      setEditing(false)
    }
    return (
      <td
        style={{
          padding: 0,
          borderRight: '1px solid var(--border)',
        }}
      >
        <input
          ref={inputRef}
          value={draft}
          onChange={e => setDraft(e.target.value)}
          onBlur={commit}
          onKeyDown={e => {
            if (e.key === 'Enter') {
              e.preventDefault()
              commit()
            } else if (e.key === 'Escape') {
              e.preventDefault()
              cancel()
            }
          }}
          style={{
            width: '100%',
            padding: '0.5rem 0.75rem',
            fontSize: '0.875rem',
            background: 'var(--bg-secondary)',
            border: '1px solid var(--accent)',
            color: 'var(--text)',
            outline: 'none',
            boxSizing: 'border-box',
          }}
        />
      </td>
    )
  }

  return (
    <td
      onClick={() => {
        if (!readonly) setEditing(true)
      }}
      style={{
        padding: '0.5rem 0.75rem',
        borderRight: '1px solid var(--border)',
        color: 'var(--text)',
        cursor: readonly ? 'default' : 'text',
        verticalAlign: 'top',
      }}
    >
      <span style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
        {stringifyValue(value) || (readonly ? '' : <span style={{ color: 'var(--text-secondary)' }}>—</span>)}
      </span>
    </td>
  )
}

// ---------- helpers ----------

function stringifyValue(v: unknown): string {
  if (v === null || v === undefined) return ''
  if (typeof v === 'string') return v
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  try {
    return JSON.stringify(v)
  } catch {
    return String(v)
  }
}

function coerceValue(raw: string): unknown {
  if (raw === '') return ''
  // Numbers without leading zeros pack-through; everything else stays a string.
  // Booleans are not coerced — "true" stays "true" text so typed columns are
  // opt-in per schema (future).
  const trimmed = raw.trim()
  if (/^-?\d+$/.test(trimmed)) {
    const n = Number(trimmed)
    if (Number.isFinite(n)) return n
  }
  if (/^-?\d+\.\d+$/.test(trimmed)) {
    const n = Number(trimmed)
    if (Number.isFinite(n)) return n
  }
  return raw
}

function generateId(): string {
  // Short, URL-safe, collision-resistant enough for this use case
  // (dozens of rows per sheet, not millions).
  const rand = () => Math.random().toString(36).slice(2, 10)
  return rand() + rand()
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== typeof b) return false
  if (a === null || b === null || typeof a !== 'object') return false
  try {
    return JSON.stringify(a) === JSON.stringify(b)
  } catch {
    return false
  }
}
