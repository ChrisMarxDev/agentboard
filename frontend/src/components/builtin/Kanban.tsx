import { useEffect, useMemo, useRef, useState } from 'react'
import { Trash2, Users } from 'lucide-react'
import { useData } from '../../hooks/useData'
import { useDataContext } from '../../hooks/DataContext'
import { beaconError, resetBeacon } from '../../lib/errorBeacon'
import {
  deleteCollectionItem,
  patchCollectionItem,
} from '../../lib/collectionWrites'
import { findUser, useUsers, type PublicUser } from '../../hooks/useUsers'
import { findTeam, useTeams, type Team } from '../../hooks/useTeams'
import { RichText } from './RichText'

interface KanbanProps {
  /** Folder collection path (e.g. "tasks/"). Omit to auto-attach to
   *  the rendering page's own folder. */
  source?: string
  groupBy: string
  columns?: string[]
  titleField?: string
}

// orderOf returns a comparable numeric key for sorting within a column.
// Cards without an explicit `order` field are placed after ordered cards,
// and the original array index is kept as a stable tiebreaker.
function orderOf(card: Record<string, unknown>, arrayIndex: number): [number, number] {
  const raw = card.order
  if (typeof raw === 'number' && Number.isFinite(raw)) return [raw, arrayIndex]
  if (typeof raw === 'string' && raw.trim() !== '') {
    const n = Number(raw)
    if (Number.isFinite(n)) return [n, arrayIndex]
  }
  return [Number.POSITIVE_INFINITY, arrayIndex]
}

export function Kanban({ source, groupBy, columns, titleField = 'title' }: KanbanProps) {
  const ctx = useDataContext()
  // Auto-attach: if no `source` prop, treat the rendering page's own
  // folder as the implicit collection. The backend ref-extractor adds
  // the same key to scope when it sees `<Kanban>` without source, so
  // the bundle is already populated.
  const effectiveSource = source ?? (ctx.path ? ctx.path + '/' : '')
  const { data, loading } = useData(effectiveSource)
  const users = useUsers()
  const teams = useTeams()
  const [openCard, setOpenCard] = useState<Record<string, unknown> | null>(null)
  const [dragOverCol, setDragOverCol] = useState<string | null>(null)
  const [dropBeforeId, setDropBeforeId] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<Record<string, unknown> | null>(null)
  const draggingIdRef = useRef<string | null>(null)

  if (loading) {
    return <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>Loading...</div>
  }

  if (!data || !Array.isArray(data)) return null

  const items = data as Record<string, unknown>[]

  // Group cards by column, preserving the array index on each card so we
  // can both render a stable order AND compute new `order` values on drop.
  const groups = new Map<string, { card: Record<string, unknown>; idx: number }[]>()
  items.forEach((card, idx) => {
    const group = String(card[groupBy] ?? 'other')
    if (!groups.has(group)) groups.set(group, [])
    groups.get(group)!.push({ card, idx })
  })
  for (const colItems of groups.values()) {
    colItems.sort((a, b) => {
      const [oa, ia] = orderOf(a.card, a.idx)
      const [ob, ib] = orderOf(b.card, b.idx)
      if (oa !== ob) return oa - ob
      return ia - ib
    })
  }

  const colOrder = columns ?? Array.from(groups.keys())

  const colLabels: Record<string, string> = {
    todo: 'To Do',
    in_progress: 'In Progress',
    done: 'Done',
    backlog: 'Backlog',
    review: 'Review',
    blocked: 'Blocked',
  }

  // computeInsertOrder returns an `order` value that places the dragged
  // card either before `beforeId` (if provided) or at the end of `colItems`.
  // Uses floating-point midpoints so we never have to renumber the column.
  function computeInsertOrder(
    colItems: { card: Record<string, unknown>; idx: number }[],
    beforeId: string | null,
    draggedId: string,
  ): number {
    const excl = colItems.filter(ci => String(ci.card.id ?? '') !== draggedId)
    if (excl.length === 0) return 1000
    const orderAt = (i: number): number => orderOf(excl[i].card, excl[i].idx)[0]
    if (beforeId) {
      const beforeIndex = excl.findIndex(ci => String(ci.card.id ?? '') === beforeId)
      if (beforeIndex >= 0) {
        const target = orderAt(beforeIndex)
        const prev = beforeIndex > 0 ? orderAt(beforeIndex - 1) : null
        if (prev == null || !Number.isFinite(prev)) {
          // Insert at the top of the column. Make sure we produce a finite value
          // even when the beforeItem itself has no explicit order yet.
          return Number.isFinite(target) ? target - 1 : 0
        }
        if (!Number.isFinite(target)) return prev + 1
        return (prev + target) / 2
      }
    }
    // Append at the end.
    const last = orderAt(excl.length - 1)
    return Number.isFinite(last) ? last + 1 : excl.length + 1
  }

  async function handleDrop(targetCol: string) {
    const id = draggingIdRef.current
    const beforeId = dropBeforeId
    draggingIdRef.current = null
    setDragOverCol(null)
    setDropBeforeId(null)
    if (!id) return
    const card = items.find(it => it.id != null && String(it.id) === id)
    if (!card) return
    const currentCol = String(card[groupBy] ?? 'other')
    const targetColItems = groups.get(targetCol) ?? []
    const patch: Record<string, unknown> = {}
    if (currentCol !== targetCol) patch[groupBy] = targetCol
    // Only emit an `order` field when the user hovered a specific card —
    // dropping on a column's empty space keeps the existing order so the
    // no-op path stays truly no-op.
    if (beforeId) {
      const newOrder = computeInsertOrder(targetColItems, beforeId, id)
      const currentOrder = orderOf(card, -1)[0]
      if (currentCol !== targetCol || !Number.isFinite(currentOrder) || currentOrder !== newOrder) {
        patch.order = newOrder
      }
    }
    if (Object.keys(patch).length === 0) return

    try {
      const res = await patchCollectionItem(effectiveSource, id, patch)
      if (!res.ok) throw new Error(`patch ${effectiveSource}/${id} → ${res.status}`)
      resetBeacon('Kanban', effectiveSource)
    } catch (e) {
      beaconError({
        component: 'Kanban',
        source,
        error: e instanceof Error ? e.message : 'card move failed',
      })
    }
  }

  async function handleDelete(card: Record<string, unknown>) {
    const id = card.id != null ? String(card.id) : null
    if (!id) return
    try {
      const res = await deleteCollectionItem(effectiveSource, id)
      if (!res.ok) throw new Error(`delete ${effectiveSource}/${id} → ${res.status}`)
      resetBeacon('Kanban', effectiveSource)
      setConfirmDelete(null)
    } catch (e) {
      beaconError({
        component: 'Kanban',
        source,
        error: e instanceof Error ? e.message : 'card delete failed',
      })
    }
  }

  return (
    <>
      <div className="flex gap-4 overflow-x-auto my-4 pb-2">
        {colOrder.map(col => {
          const colItems = groups.get(col) ?? []
          const isOver = dragOverCol === col
          return (
            <div
              key={col}
              onDragOver={e => {
                e.preventDefault()
                if (dragOverCol !== col) setDragOverCol(col)
              }}
              onDragLeave={() => setDragOverCol(prev => (prev === col ? null : prev))}
              onDrop={e => {
                e.preventDefault()
                void handleDrop(col)
              }}
              className="min-w-[11rem] flex-1 rounded-lg p-3"
              style={{
                background: 'var(--bg-secondary)',
                border: '1px solid var(--border)',
                outline: isOver ? '2px solid var(--accent)' : undefined,
                outlineOffset: isOver ? '-2px' : undefined,
                transition: 'outline-color 120ms ease-out',
              }}
            >
              <div
                className="text-sm font-medium mb-3 flex items-center justify-between"
                style={{ color: 'var(--text-secondary)' }}
              >
                <span>{colLabels[col] ?? col.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())}</span>
                <span className="text-xs px-1.5 py-0.5 rounded" style={{ background: 'var(--border)' }}>
                  {colItems.length}
                </span>
              </div>
              <div className="space-y-2">
                {colItems.map(({ card: item }, i) => {
                  const id = item.id != null ? String(item.id) : null
                  const draggable = id !== null
                  const isInsertTarget = id !== null && dropBeforeId === id && dragOverCol === col
                  return (
                    <div
                      key={id ?? i}
                      draggable={draggable}
                      onDragStart={e => {
                        if (!id) return
                        draggingIdRef.current = id
                        if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
                      }}
                      onDragEnd={() => {
                        draggingIdRef.current = null
                        setDragOverCol(null)
                        setDropBeforeId(null)
                      }}
                      onDragOver={e => {
                        // Only insert-before tracking here — the column-level
                        // onDragOver still fires (events bubble) and handles
                        // setting dragOverCol.
                        if (!id || !draggingIdRef.current || draggingIdRef.current === id) return
                        const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
                        const midpoint = rect.top + rect.height / 2
                        const before = e.clientY < midpoint ? id : null
                        if (before !== dropBeforeId) setDropBeforeId(before)
                      }}
                      onClick={e => {
                        // Don't open the modal when the trash icon was clicked.
                        if ((e.target as HTMLElement).closest('[data-card-action]')) return
                        setOpenCard(item)
                      }}
                      role="button"
                      tabIndex={0}
                      onKeyDown={e => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          setOpenCard(item)
                        }
                      }}
                      className="p-3 rounded-md text-sm select-none group relative"
                      style={{
                        background: 'var(--bg)',
                        border: '1px solid var(--border)',
                        borderTop: isInsertTarget
                          ? '2px solid var(--accent)'
                          : '1px solid var(--border)',
                        color: 'var(--text)',
                        cursor: draggable ? 'grab' : 'pointer',
                        display: 'flex',
                        flexDirection: 'column',
                        gap: '0.5rem',
                      }}
                    >
                      <RichText text={String(item[titleField] ?? item.name ?? item.id ?? '')} />
                      <AssigneeStrip assignees={item.assignees} users={users} teams={teams} />
                      {draggable && (
                        <button
                          type="button"
                          data-card-action="delete"
                          aria-label="Delete card"
                          onClick={e => {
                            e.stopPropagation()
                            setConfirmDelete(item)
                          }}
                          className="opacity-0 group-hover:opacity-100 focus:opacity-100 transition-opacity"
                          style={{
                            position: 'absolute',
                            top: '0.375rem',
                            right: '0.375rem',
                            width: '22px',
                            height: '22px',
                            display: 'inline-flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            borderRadius: '6px',
                            background: 'var(--bg-secondary)',
                            border: '1px solid var(--border)',
                            color: 'var(--text-secondary)',
                            cursor: 'pointer',
                            padding: 0,
                          }}
                        >
                          <Trash2 size={12} />
                        </button>
                      )}
                    </div>
                  )
                })}
                {colItems.length === 0 && (
                  <div
                    className="text-xs py-4 text-center"
                    style={{ color: 'var(--text-secondary)' }}
                  >
                    Empty
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>
      {openCard && (
        <KanbanCardModal
          card={openCard}
          source={effectiveSource}
          groupBy={groupBy}
          columns={colOrder}
          titleField={titleField}
          users={users}
          teams={teams}
          onClose={() => setOpenCard(null)}
          onDelete={() => {
            setOpenCard(null)
            setConfirmDelete(openCard)
          }}
        />
      )}
      {confirmDelete && (
        <ConfirmDeleteCardDialog
          card={confirmDelete}
          titleField={titleField}
          onCancel={() => setConfirmDelete(null)}
          onConfirm={() => void handleDelete(confirmDelete)}
        />
      )}
    </>
  )
}

interface ConfirmDeleteCardDialogProps {
  card: Record<string, unknown>
  titleField: string
  onCancel: () => void
  onConfirm: () => void
}

function ConfirmDeleteCardDialog({
  card,
  titleField,
  onCancel,
  onConfirm,
}: ConfirmDeleteCardDialogProps) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onCancel])

  const title = String(card[titleField] ?? card.name ?? card.id ?? '')
  return (
    <div
      onClick={onCancel}
      className="fixed inset-0 z-[110] flex items-center justify-center p-4"
      style={{ background: 'rgba(0, 0, 0, 0.4)' }}
      role="dialog"
      aria-modal="true"
      aria-label="Confirm delete card"
    >
      <div
        onClick={e => e.stopPropagation()}
        className="rounded-lg border w-full max-w-md"
        style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border)' }}
      >
        <div
          className="px-5 py-3 border-b"
          style={{ borderColor: 'var(--border)' }}
        >
          <div className="font-semibold text-sm" style={{ color: 'var(--text)' }}>
            Delete card?
          </div>
        </div>
        <div className="px-5 py-4 text-sm" style={{ color: 'var(--text)' }}>
          <p>
            <span style={{ color: 'var(--text-secondary)' }}>Title:</span>{' '}
            <strong>{title || '(untitled)'}</strong>
          </p>
          <p className="mt-3" style={{ color: 'var(--text-secondary)' }}>
            This cannot be undone — history for data rows isn't kept yet.
          </p>
        </div>
        <div
          className="px-5 py-3 border-t flex items-center justify-end gap-2"
          style={{ borderColor: 'var(--border)' }}
        >
          <button
            onClick={onCancel}
            className="text-sm px-3 py-1.5 rounded-md"
            style={{
              background: 'transparent',
              border: '1px solid var(--border)',
              color: 'var(--text)',
              cursor: 'pointer',
            }}
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className="text-sm px-3 py-1.5 rounded-md font-medium"
            style={{
              background: 'var(--error)',
              border: '1px solid var(--error)',
              color: 'white',
              cursor: 'pointer',
            }}
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  )
}

interface KanbanCardModalProps {
  card: Record<string, unknown>
  source: string
  groupBy: string
  columns: string[]
  titleField: string
  users: PublicUser[]
  teams: Team[]
  onClose: () => void
  onDelete: () => void
}

// KanbanCardModal is the editable task detail view. Every field on
// the card maps to an interactive element:
//   - title      → click-to-edit text input
//   - status     → dropdown seeded from the Kanban's `columns` list
//   - assignees  → chips with × to remove + "+ add" picker
//   - body       → multiline textarea (auto-persisted, shown via <RichText>)
//   - id         → read-only (the row key server-side)
//
// Writes go through PATCH /api/data/<source>/<id>. Every saved edit
// fires the existing data.merge.<key> event, which fans out on the
// webhook bus. Mention-in-title and assignee-add both produce inbox
// items server-side (see internal/server/inbox_dispatch.go).
//
// The modal is intentionally opinionated about field names: `status`,
// `assignees`, and `body` get typed editors; anything else falls
// back to a plain readonly line so legacy cards don't blow up.
function KanbanCardModal({
  card,
  source,
  groupBy,
  columns,
  titleField,
  users,
  teams,
  onClose,
  onDelete,
}: KanbanCardModalProps) {
  const id = card.id != null ? String(card.id) : null

  // Pull live state off the card each render. Optimistic edits are
  // carried in a local `patched` map so the UI reflects changes
  // immediately; SSE + useData bring the authoritative value back in
  // due course.
  const [patched, setPatched] = useState<Record<string, unknown>>({})
  const merged: Record<string, unknown> = { ...card, ...patched }

  const title = String(merged[titleField] ?? merged.name ?? merged.id ?? '')
  const status = String(merged[groupBy] ?? '')
  const assignees = Array.isArray(merged.assignees)
    ? (merged.assignees as string[]).filter(a => typeof a === 'string')
    : []
  const body = typeof merged.body === 'string' ? merged.body : ''

  const readonly = !id

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  async function patchField(field: string, value: unknown) {
    if (!id) return
    setPatched(prev => ({ ...prev, [field]: value }))
    try {
      const res = await patchCollectionItem(source, id, { [field]: value })
      if (!res.ok) throw new Error(`patch ${source}/${id} → ${res.status}`)
      resetBeacon('Kanban', source)
    } catch (e) {
      setPatched(prev => {
        const next = { ...prev }
        delete next[field]
        return next
      })
      beaconError({
        component: 'Kanban',
        source,
        error: e instanceof Error ? e.message : 'card edit failed',
      })
    }
  }

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-[100] flex items-center justify-center p-4"
      style={{ background: 'rgba(0, 0, 0, 0.4)' }}
      role="dialog"
      aria-modal="true"
      aria-label={title || 'Card details'}
    >
      <div
        onClick={e => e.stopPropagation()}
        className="rounded-lg border w-full max-w-xl flex flex-col"
        style={{
          background: 'var(--bg-secondary)',
          borderColor: 'var(--border)',
          maxHeight: '85vh',
        }}
      >
        <div
          className="flex items-center justify-between px-5 py-3 border-b gap-4"
          style={{ borderColor: 'var(--border)' }}
        >
          <div className="text-xs uppercase tracking-wide" style={{ color: 'var(--text-secondary)' }}>
            Task {id ? `· ${id}` : ''}
          </div>
          <div className="flex items-center gap-2">
            {!readonly && (
              <button
                type="button"
                onClick={onDelete}
                aria-label="Delete card"
                className="text-xs inline-flex items-center gap-1 px-2 py-1 rounded-md"
                style={{
                  background: 'transparent',
                  border: '1px solid var(--border)',
                  color: 'var(--error)',
                  cursor: 'pointer',
                }}
              >
                <Trash2 size={12} />
                Delete
              </button>
            )}
            <button
              onClick={onClose}
              aria-label="Close"
              className="text-lg leading-none px-2"
              style={{
                background: 'transparent',
                border: 'none',
                color: 'var(--text-secondary)',
                cursor: 'pointer',
              }}
            >
              ×
            </button>
          </div>
        </div>

        <div className="px-5 py-4 flex flex-col gap-4 overflow-y-auto">
          {/* Title */}
          <EditableTitle
            value={title}
            readonly={readonly}
            onSave={next => patchField(titleField, next)}
          />

          {/* Status dropdown */}
          <FieldRow label="Status">
            <StatusPicker
              value={status}
              columns={columns}
              readonly={readonly}
              onChange={next => patchField(groupBy, next)}
            />
          </FieldRow>

          {/* Assignees */}
          <FieldRow label="Assignees">
            <AssigneePicker
              value={assignees}
              readonly={readonly}
              users={users}
              teams={teams}
              onChange={next => patchField('assignees', next)}
            />
          </FieldRow>

          {/* Body */}
          <FieldRow label="Description">
            <BodyEditor
              value={body}
              readonly={readonly}
              onSave={next => patchField('body', next)}
            />
          </FieldRow>

          {/* Any other fields get a compact readonly echo so nothing
              silently vanishes — legacy cards that carry `priority`,
              `due`, etc. stay inspectable until we add typed editors. */}
          <OtherFields card={merged} skip={new Set([titleField, groupBy, 'id', 'assignees', 'body', 'order'])} />
        </div>
      </div>
    </div>
  )
}

// --- sub-components for the detail modal ---

function FieldRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="text-xs uppercase tracking-wide" style={{ color: 'var(--text-secondary)' }}>
        {label}
      </div>
      <div>{children}</div>
    </div>
  )
}

function EditableTitle({
  value,
  readonly,
  onSave,
}: {
  value: string
  readonly: boolean
  onSave: (next: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(value)

  useEffect(() => {
    if (!editing) setDraft(value)
  }, [value, editing])

  if (!editing) {
    return (
      <h2
        onClick={() => {
          if (!readonly) setEditing(true)
        }}
        className="font-semibold text-lg"
        style={{
          color: 'var(--text)',
          cursor: readonly ? 'default' : 'text',
          margin: 0,
          lineHeight: 1.3,
        }}
      >
        <RichText text={value || 'Untitled'} />
      </h2>
    )
  }
  const commit = () => {
    if (draft.trim() !== value.trim()) onSave(draft.trim())
    setEditing(false)
  }
  return (
    <input
      autoFocus
      value={draft}
      onChange={e => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={e => {
        if (e.key === 'Enter') {
          e.preventDefault()
          commit()
        } else if (e.key === 'Escape') {
          e.preventDefault()
          setEditing(false)
          setDraft(value)
        }
      }}
      className="font-semibold text-lg rounded-md px-2 py-1"
      style={{
        background: 'var(--bg)',
        border: '1px solid var(--accent)',
        color: 'var(--text)',
        outline: 'none',
      }}
    />
  )
}

function StatusPicker({
  value,
  columns,
  readonly,
  onChange,
}: {
  value: string
  columns: string[]
  readonly: boolean
  onChange: (next: string) => void
}) {
  if (readonly) {
    return <span className="text-sm" style={{ color: 'var(--text)' }}>{value || '—'}</span>
  }
  const opts = columns.length > 0 ? columns : [value].filter(Boolean)
  return (
    <select
      value={value}
      onChange={e => onChange(e.target.value)}
      className="text-sm rounded-md px-2 py-1"
      style={{
        background: 'var(--bg)',
        border: '1px solid var(--border)',
        color: 'var(--text)',
      }}
    >
      {!opts.includes(value) && value && <option value={value}>{value}</option>}
      {opts.map(c => (
        <option key={c} value={c}>
          {c.replace(/_/g, ' ')}
        </option>
      ))}
    </select>
  )
}

// Candidate shape for the combobox — union of user and team so the
// picker can mix both under one search box.
type AssigneeCandidate =
  | { kind: 'user'; username: string; display_name?: string; avatar_color?: string }
  | { kind: 'team'; slug: string; display_name?: string; member_count: number }

function AssigneePicker({
  value,
  readonly,
  users,
  teams,
  onChange,
}: {
  value: string[]
  readonly: boolean
  users: PublicUser[]
  teams: Team[]
  onChange: (next: string[]) => void
}) {
  const [adding, setAdding] = useState(false)
  const candidates = useMemo<AssigneeCandidate[]>(() => {
    const taken = new Set(value.map(v => v.toLowerCase().replace(/^@/, '')))
    const userCands: AssigneeCandidate[] = users
      .filter(u => !u.deactivated)
      .filter(u => !taken.has(u.username.toLowerCase()))
      .map(u => ({
        kind: 'user',
        username: u.username,
        display_name: u.display_name,
        avatar_color: u.avatar_color,
      }))
    const teamCands: AssigneeCandidate[] = teams
      .filter(t => !taken.has(t.slug.toLowerCase()))
      .map(t => ({
        kind: 'team',
        slug: t.slug,
        display_name: t.display_name,
        member_count: t.members?.length ?? 0,
      }))
    return [...userCands, ...teamCands]
  }, [users, teams, value])

  const remove = (name: string) => {
    onChange(value.filter(v => v !== name))
  }
  const add = (name: string) => {
    onChange([...value, name])
    setAdding(false)
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {value.length === 0 && !adding && (
        <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          Unassigned
        </span>
      )}
      {value.map(raw => {
        const name = raw.replace(/^@/, '')
        const u = findUser(users, name)
        const team = !u ? findTeam(teams, name) : undefined
        const isTeam = Boolean(team)
        const color = u?.avatar_color ?? 'var(--border)'
        const dimmed = Boolean(u?.deactivated)
        return (
          <span
            key={raw}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: '0.3rem',
              padding: '0.15rem 0.4rem 0.15rem 0.3rem',
              borderRadius: '9999px',
              border: isTeam
                ? '1px dashed color-mix(in srgb, var(--accent) 30%, transparent)'
                : '1px solid var(--border)',
              background: isTeam
                ? 'color-mix(in srgb, var(--accent) 8%, transparent)'
                : 'var(--bg)',
              fontSize: '0.8rem',
              color: 'var(--text)',
              opacity: dimmed ? 0.55 : 1,
            }}
          >
            {isTeam ? (
              <Users size={11} strokeWidth={2} aria-hidden />
            ) : (
              <span
                aria-hidden
                style={{
                  width: '0.5rem',
                  height: '0.5rem',
                  borderRadius: '9999px',
                  background: color,
                }}
              />
            )}
            @{name}
            {!readonly && (
              <button
                type="button"
                aria-label={`Remove ${name}`}
                onClick={() => remove(raw)}
                style={{
                  background: 'transparent',
                  border: 'none',
                  color: 'var(--text-secondary)',
                  cursor: 'pointer',
                  padding: 0,
                  marginLeft: 2,
                }}
              >
                ×
              </button>
            )}
          </span>
        )
      })}
      {!readonly && (
        <>
          {adding ? (
            <AssigneeCombobox
              candidates={candidates}
              onPick={add}
              onCancel={() => setAdding(false)}
            />
          ) : (
            <button
              type="button"
              onClick={() => setAdding(true)}
              className="text-xs inline-flex items-center gap-1 rounded-full px-2 py-1"
              style={{
                background: 'transparent',
                border: '1px dashed var(--border)',
                color: 'var(--text-secondary)',
                cursor: 'pointer',
              }}
            >
              + Add
            </button>
          )}
        </>
      )}
    </div>
  )
}

function AssigneeCombobox({
  candidates,
  onPick,
  onCancel,
}: {
  candidates: AssigneeCandidate[]
  onPick: (username: string) => void
  onCancel: () => void
}) {
  const [q, setQ] = useState('')
  const needle = q.toLowerCase()
  const filtered = candidates.filter(c => {
    const label = c.kind === 'user' ? c.username : c.slug
    const display = c.display_name ?? ''
    return label.includes(needle) || display.toLowerCase().includes(needle)
  })
  const pickKey = (c: AssigneeCandidate) =>
    c.kind === 'user' ? c.username : c.slug
  return (
    <div className="relative">
      <input
        autoFocus
        value={q}
        onChange={e => setQ(e.target.value)}
        onKeyDown={e => {
          if (e.key === 'Escape') onCancel()
          if (e.key === 'Enter' && filtered[0]) onPick(pickKey(filtered[0]))
        }}
        placeholder="@user or @team"
        className="text-xs rounded-full px-2 py-1"
        style={{
          background: 'var(--bg)',
          border: '1px solid var(--accent)',
          color: 'var(--text)',
          outline: 'none',
          width: 160,
        }}
      />
      {q && (
        <div
          className="absolute z-10 mt-1 rounded-md overflow-hidden"
          style={{
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            minWidth: 180,
          }}
        >
          {filtered.length === 0 ? (
            <div className="text-xs px-2 py-1" style={{ color: 'var(--text-secondary)' }}>
              No match
            </div>
          ) : (
            filtered.slice(0, 8).map(c => (
              <button
                key={`${c.kind}:${pickKey(c)}`}
                type="button"
                onClick={() => onPick(pickKey(c))}
                className="w-full text-left text-xs px-2 py-1 flex items-center gap-2"
                style={{ background: 'transparent', border: 'none', color: 'var(--text)', cursor: 'pointer' }}
              >
                {c.kind === 'team' ? (
                  <Users size={11} strokeWidth={2} aria-hidden />
                ) : (
                  <span
                    aria-hidden
                    style={{
                      display: 'inline-block',
                      width: '0.5rem',
                      height: '0.5rem',
                      borderRadius: '9999px',
                      background: c.avatar_color ?? 'var(--border)',
                    }}
                  />
                )}
                <span>@{pickKey(c)}</span>
                {c.display_name && (
                  <span style={{ color: 'var(--text-secondary)' }}> · {c.display_name}</span>
                )}
                {c.kind === 'team' && (
                  <span style={{ color: 'var(--text-secondary)' }}>
                    · {c.member_count} member{c.member_count === 1 ? '' : 's'}
                  </span>
                )}
              </button>
            ))
          )}
        </div>
      )}
    </div>
  )
}

function BodyEditor({
  value,
  readonly,
  onSave,
}: {
  value: string
  readonly: boolean
  onSave: (next: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(value)
  useEffect(() => {
    if (!editing) setDraft(value)
  }, [value, editing])

  if (!editing) {
    return (
      <div
        onClick={() => {
          if (!readonly) setEditing(true)
        }}
        className="text-sm rounded-md px-3 py-2"
        style={{
          background: 'var(--bg)',
          border: '1px dashed var(--border)',
          color: value ? 'var(--text)' : 'var(--text-secondary)',
          cursor: readonly ? 'default' : 'text',
          whiteSpace: 'pre-wrap',
          minHeight: 40,
        }}
      >
        {value ? <RichText text={value} /> : readonly ? '' : 'Add a description — @mentions are supported.'}
      </div>
    )
  }
  const commit = () => {
    if (draft !== value) onSave(draft)
    setEditing(false)
  }
  return (
    <textarea
      autoFocus
      value={draft}
      onChange={e => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={e => {
        if (e.key === 'Escape') {
          e.preventDefault()
          setEditing(false)
          setDraft(value)
        }
      }}
      className="text-sm rounded-md px-3 py-2"
      rows={5}
      style={{
        background: 'var(--bg)',
        border: '1px solid var(--accent)',
        color: 'var(--text)',
        outline: 'none',
        resize: 'vertical',
        fontFamily: 'inherit',
        width: '100%',
      }}
    />
  )
}

function OtherFields({
  card,
  skip,
}: {
  card: Record<string, unknown>
  skip: Set<string>
}) {
  const rest = Object.entries(card).filter(([k]) => !skip.has(k))
  if (rest.length === 0) return null
  return (
    <div
      className="flex flex-col gap-1 pt-3 mt-2 border-t"
      style={{ borderColor: 'var(--border)' }}
    >
      <div
        className="text-xs uppercase tracking-wide"
        style={{ color: 'var(--text-secondary)' }}
      >
        Other fields
      </div>
      <div className="flex flex-col gap-1">
        {rest.map(([k, v]) => (
          <div key={k} className="grid grid-cols-[7rem_1fr] gap-4 text-xs">
            <div style={{ color: 'var(--text-secondary)' }}>{k}</div>
            <div style={{ color: 'var(--text)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
              {formatValue(v)}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function formatValue(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'object') {
    try {
      return JSON.stringify(v, null, 2)
    } catch {
      return String(v)
    }
  }
  return String(v)
}

// AssigneeStrip renders the `assignees: string[]` field on a card as a
// small row of color dots + usernames. Unknown usernames still render
// (as plain text) so the attribution survives even if the card was
// authored before the user existed. `assignees` of the wrong shape is
// silently ignored — this is a convention, not a validated schema.
function AssigneeStrip({
  assignees,
  users,
  teams,
}: {
  assignees: unknown
  users: PublicUser[]
  teams: Team[]
}) {
  if (!Array.isArray(assignees) || assignees.length === 0) return null
  const names = assignees
    .filter((a): a is string => typeof a === 'string')
    .map((a) => a.replace(/^@/, ''))
  if (names.length === 0) return null
  return (
    <div
      style={{
        display: 'flex',
        flexWrap: 'wrap',
        gap: '0.25rem',
        fontSize: '0.6875rem',
        color: 'var(--text-secondary)',
      }}
    >
      {names.map((name) => {
        // User-first, fall through to team, fall back to plain.
        const u = findUser(users, name)
        if (u) {
          const color = u.avatar_color ?? 'var(--border)'
          const dimmed = Boolean(u.deactivated)
          return (
            <span
              key={name}
              title={u.display_name ? `${u.display_name} (@${name})` : `@${name}`}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.25rem',
                padding: '0.125rem 0.4rem 0.125rem 0.3rem',
                borderRadius: '9999px',
                border: '1px solid var(--border)',
                background: 'var(--bg-secondary)',
                opacity: dimmed ? 0.55 : 1,
              }}
            >
              <span
                aria-hidden
                style={{
                  width: '0.45rem',
                  height: '0.45rem',
                  borderRadius: '9999px',
                  background: color,
                  flexShrink: 0,
                }}
              />
              @{name}
            </span>
          )
        }
        const team = findTeam(teams, name)
        if (team) {
          const memberCount = team.members?.length ?? 0
          const title = team.display_name
            ? `${team.display_name} (@${team.slug}${memberCount ? ` — ${memberCount}` : ''})`
            : `@${team.slug}${memberCount ? ` — ${memberCount}` : ''}`
          return (
            <span
              key={name}
              title={title}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.25rem',
                padding: '0.125rem 0.4rem 0.125rem 0.3rem',
                borderRadius: '9999px',
                border: '1px dashed color-mix(in srgb, var(--accent) 30%, transparent)',
                background: 'color-mix(in srgb, var(--accent) 8%, transparent)',
              }}
            >
              <Users size={9} strokeWidth={2} aria-hidden />
              @{name}
            </span>
          )
        }
        // Unknown — plain fallback.
        return (
          <span
            key={name}
            title={`@${name}`}
            style={{
              padding: '0.125rem 0.4rem',
              borderRadius: '9999px',
              border: '1px solid var(--border)',
              background: 'var(--bg-secondary)',
              opacity: 0.55,
            }}
          >
            @{name}
          </span>
        )
      })}
    </div>
  )
}
