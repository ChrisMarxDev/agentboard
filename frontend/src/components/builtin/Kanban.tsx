import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Send, Trash2, Users } from 'lucide-react'
import { useData } from '../../hooks/useData'
import { useDataContext } from '../../hooks/DataContext'
import { apiFetch } from '../../lib/session'
import { beaconError, resetBeacon } from '../../lib/errorBeacon'
import {
  deleteCollectionItem,
  patchCollectionItem,
} from '../../lib/collectionWrites'
import { findUser, useUsers, type PublicUser } from '../../hooks/useUsers'
import { findTeam, useTeams, type Team } from '../../hooks/useTeams'
import { useMe } from '../../hooks/useMe'
import { RichText } from './RichText'

interface KanbanProps {
  /** Folder collection path (e.g. "tasks/"). Omit to auto-attach to
   *  the rendering page's own folder. */
  source?: string
  groupBy: string
  columns?: string[]
  titleField?: string
}

// KNOWN_COL_ORDER is the canonical workflow order for `col`-shaped
// kanbans. Anything not in this list slots in alphabetically after the
// known set. Set explicit `columns={[...]}` on the Kanban to override.
const KNOWN_COL_ORDER = [
  'backlog',
  'todo',
  'in-progress', 'in_progress', 'doing',
  'review',
  'done', 'shipped',
  'cancelled', 'archive',
]

function defaultColOrder(present: string[], groupBy: string): string[] {
  // Only apply the workflow heuristic when the page is grouped by the
  // canonical "col" field; for other groupBy keys, fall back to the
  // discovery order the items naturally produce (alphabetical).
  if (groupBy !== 'col') return present
  // Fresh boards with no cards yet still need columns to render against,
  // otherwise the user lands on a Kanban-shaped void after creating a
  // project. Seed the canonical workflow trio so the page is usable
  // from the first second.
  if (present.length === 0) return ['todo', 'in_progress', 'done']
  const set = new Set(present)
  const known = KNOWN_COL_ORDER.filter(c => set.has(c))
  const knownSet = new Set(known)
  const others = present.filter(p => !knownSet.has(p)).sort()
  return [...known, ...others]
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

  // Folder boards (source ends with "/") render an empty-but-usable
  // board on first paint — the + New task affordance is the whole
  // point of a fresh project page. Inline-array boards still need
  // real data; bail to keep the legacy demo shape unchanged.
  const isFolderBoard = effectiveSource.endsWith('/')
  if (!data || !Array.isArray(data)) {
    if (!isFolderBoard) return null
  }

  const items = (Array.isArray(data) ? data : []) as Record<string, unknown>[]

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

  const colOrder = columns ?? defaultColOrder(Array.from(groups.keys()), groupBy)

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

  // (Folder-collection boards (source ends with "/") are the only place
  //  the + New Task affordance makes sense — that's where each card is a
  //  real .md doc we can write to. For frontmatter-array boards (inline
  //  demo data), skip the button. The flag is computed near the top of
  //  the function alongside the empty-board early-render logic.)

  return (
    <>
      {isFolderBoard && <NewTaskBar source={effectiveSource} />}
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
          siblings={items}
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
  /** All cards in the same collection. Powers parent_id picker, the
   *  blocked_by picker, and the derived sub-tasks + blockers blocks. */
  siblings: Record<string, unknown>[]
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
  siblings,
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
  const priority = typeof merged.priority === 'number' ? merged.priority : null
  const due = typeof merged.due === 'string' ? merged.due : ''
  const labels = Array.isArray(merged.labels)
    ? (merged.labels as unknown[]).filter((l): l is string => typeof l === 'string')
    : []
  const parentID = typeof merged.parent_id === 'string' ? merged.parent_id : ''
  const blockedBy = Array.isArray(merged.blocked_by)
    ? (merged.blocked_by as unknown[]).filter((b): b is string => typeof b === 'string')
    : []

  // Derived: cards whose parent_id points at us → render as sub-tasks.
  // Filter siblings for tasks where parent_id matches this id.
  const subtasks = id
    ? siblings.filter(s => typeof s.parent_id === 'string' && (s.parent_id as string) === id)
    : []
  // Resolve blocked_by id list → full sibling rows so we can show their status.
  const blockerCards = blockedBy
    .map(bid => siblings.find(s => String(s.id ?? '') === bid))
    .filter((c): c is Record<string, unknown> => Boolean(c))

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

          {/* Priority — segmented 1..5. Empty when unset; click any
              segment to set, click the active one again to clear. */}
          <FieldRow label="Priority">
            <PriorityPicker
              value={priority}
              readonly={readonly}
              onChange={next => patchField('priority', next)}
            />
          </FieldRow>

          {/* Due date */}
          <FieldRow label="Due">
            <DueEditor
              value={due}
              readonly={readonly}
              onChange={next => patchField('due', next)}
            />
          </FieldRow>

          {/* Labels — string-array chips with + */}
          <FieldRow label="Labels">
            <LabelChips
              value={labels}
              readonly={readonly}
              onChange={next => patchField('labels', next)}
            />
          </FieldRow>

          {/* Parent — single task picker over siblings */}
          <FieldRow label="Parent">
            <TaskPicker
              value={parentID}
              siblings={siblings}
              excludeId={id ?? undefined}
              readonly={readonly}
              groupBy={groupBy}
              titleField={titleField}
              onChange={next => patchField('parent_id', next)}
            />
          </FieldRow>

          {/* Blocked by — multi-task picker over siblings */}
          <FieldRow label="Blocked by">
            <TaskMultiPicker
              value={blockedBy}
              siblings={siblings}
              excludeId={id ?? undefined}
              readonly={readonly}
              groupBy={groupBy}
              titleField={titleField}
              onChange={next => patchField('blocked_by', next)}
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

          {/* Comments — sibling stream, single per board, filtered to
              this task's id. @mentions in comments produce inbox items. */}
          {id && (
            <FieldRow label="Comments">
              <TaskComments taskId={id} source={source} />
            </FieldRow>
          )}

          {/* Sub-tasks — only when at least one sibling references this
              card via parent_id. */}
          {subtasks.length > 0 && (
            <FieldRow label={`Sub-tasks (${subtasks.length})`}>
              <SubtasksBlock
                tasks={subtasks}
                groupBy={groupBy}
                titleField={titleField}
              />
            </FieldRow>
          )}

          {/* Blockers — render each blocked_by id with its status inline. */}
          {blockerCards.length > 0 && (
            <FieldRow label="Blockers">
              <BlockersBlock
                blockers={blockerCards}
                groupBy={groupBy}
                titleField={titleField}
              />
            </FieldRow>
          )}

          {/* Any other fields get a compact readonly echo so nothing
              silently vanishes — legacy cards with custom fields stay
              inspectable until we add typed editors for them. */}
          <OtherFields
            card={merged}
            skip={new Set([
              titleField, groupBy, 'id', 'assignees', 'body', 'order',
              'priority', 'due', 'labels', 'parent_id', 'blocked_by',
            ])}
          />
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

// Priority — segmented 1..5. null/undefined = unset (empty pill row);
// click any number to set; click the active one to clear.
function PriorityPicker({
  value,
  readonly,
  onChange,
}: {
  value: number | null
  readonly: boolean
  onChange: (next: number | null) => void
}) {
  const opts = [1, 2, 3, 4, 5]
  if (readonly) {
    return (
      <span className="text-sm" style={{ color: 'var(--text)' }}>
        {value ?? '—'}
      </span>
    )
  }
  return (
    <div className="inline-flex gap-1">
      {opts.map(n => {
        const active = value === n
        return (
          <button
            key={n}
            type="button"
            onClick={() => onChange(active ? null : n)}
            className="text-xs rounded-md px-2 py-1"
            style={{
              background: active ? 'var(--accent-light)' : 'var(--bg)',
              border: `1px solid ${active ? 'var(--accent)' : 'var(--border)'}`,
              color: active ? 'var(--accent)' : 'var(--text)',
              fontWeight: active ? 600 : 400,
              cursor: 'pointer',
              minWidth: 28,
            }}
            aria-pressed={active}
          >
            {n}
          </button>
        )
      })}
      {value !== null && (
        <button
          type="button"
          onClick={() => onChange(null)}
          className="text-xs px-2 py-1"
          style={{
            background: 'transparent',
            border: 'none',
            color: 'var(--text-secondary)',
            cursor: 'pointer',
          }}
          title="Clear priority"
        >
          clear
        </button>
      )}
    </div>
  )
}

function DueEditor({
  value,
  readonly,
  onChange,
}: {
  value: string
  readonly: boolean
  onChange: (next: string) => void
}) {
  // Accept any ISO-8601 prefix (date or datetime). Render an <input
  // type="date"> for simplicity; we keep whatever the user typed and
  // trim to the YYYY-MM-DD prefix for the input.
  const dateValue = (value || '').slice(0, 10)
  if (readonly) {
    return <span className="text-sm" style={{ color: 'var(--text)' }}>{value || '—'}</span>
  }
  return (
    <div className="flex items-center gap-2">
      <input
        type="date"
        value={dateValue}
        onChange={e => onChange(e.target.value)}
        className="text-sm rounded-md px-2 py-1"
        style={{
          background: 'var(--bg)',
          border: '1px solid var(--border)',
          color: 'var(--text)',
        }}
      />
      {value && (
        <button
          type="button"
          onClick={() => onChange('')}
          className="text-xs"
          style={{
            background: 'transparent',
            border: 'none',
            color: 'var(--text-secondary)',
            cursor: 'pointer',
          }}
        >
          clear
        </button>
      )}
    </div>
  )
}

function LabelChips({
  value,
  readonly,
  onChange,
}: {
  value: string[]
  readonly: boolean
  onChange: (next: string[]) => void
}) {
  const [draft, setDraft] = useState('')
  const add = () => {
    const v = draft.trim()
    if (!v || value.includes(v)) {
      setDraft('')
      return
    }
    onChange([...value, v])
    setDraft('')
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {value.length === 0 && readonly && (
        <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>—</span>
      )}
      {value.map(label => (
        <span
          key={label}
          className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs"
          style={{
            background: 'var(--bg)',
            border: '1px solid var(--border)',
            color: 'var(--text)',
          }}
        >
          {label}
          {!readonly && (
            <button
              type="button"
              onClick={() => onChange(value.filter(v => v !== label))}
              aria-label={`Remove ${label}`}
              className="leading-none"
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
      ))}
      {!readonly && (
        <input
          value={draft}
          onChange={e => setDraft(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Enter' || e.key === ',') {
              e.preventDefault()
              add()
            }
          }}
          onBlur={add}
          placeholder="+ label"
          className="text-xs rounded-full px-2 py-1"
          style={{
            background: 'transparent',
            border: '1px dashed var(--border)',
            color: 'var(--text-secondary)',
            outline: 'none',
            width: 100,
          }}
        />
      )}
    </div>
  )
}

// TaskPicker — single-select over siblings. Empty value = no parent.
function TaskPicker({
  value,
  siblings,
  excludeId,
  readonly,
  groupBy,
  titleField,
  onChange,
}: {
  value: string
  siblings: Record<string, unknown>[]
  excludeId?: string
  readonly: boolean
  groupBy: string
  titleField: string
  onChange: (next: string) => void
}) {
  const opts = siblings
    .map(s => ({
      id: String(s.id ?? ''),
      title: String(s[titleField] ?? s.id ?? ''),
      col: String(s[groupBy] ?? ''),
    }))
    .filter(s => s.id && s.id !== excludeId)
    .sort((a, b) => a.title.localeCompare(b.title))
  if (readonly) {
    if (!value) return <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>—</span>
    const found = opts.find(o => o.id === value)
    return <span className="text-sm" style={{ color: 'var(--text)' }}>{found?.title ?? value}</span>
  }
  return (
    <select
      value={value}
      onChange={e => onChange(e.target.value)}
      className="text-sm rounded-md px-2 py-1 max-w-full"
      style={{
        background: 'var(--bg)',
        border: '1px solid var(--border)',
        color: 'var(--text)',
        maxWidth: '100%',
      }}
    >
      <option value="">— none —</option>
      {opts.map(o => (
        <option key={o.id} value={o.id}>
          {o.title}
        </option>
      ))}
    </select>
  )
}

// TaskMultiPicker — multi-select via chips + searchable combobox.
function TaskMultiPicker({
  value,
  siblings,
  excludeId,
  readonly,
  groupBy,
  titleField,
  onChange,
}: {
  value: string[]
  siblings: Record<string, unknown>[]
  excludeId?: string
  readonly: boolean
  groupBy: string
  titleField: string
  onChange: (next: string[]) => void
}) {
  const [adding, setAdding] = useState(false)
  const [q, setQ] = useState('')
  const candidates = siblings
    .map(s => ({
      id: String(s.id ?? ''),
      title: String(s[titleField] ?? s.id ?? ''),
      col: String(s[groupBy] ?? ''),
    }))
    .filter(s => s.id && s.id !== excludeId && !value.includes(s.id))
  const filtered = candidates.filter(c =>
    !q.trim() || c.title.toLowerCase().includes(q.toLowerCase()) || c.id.includes(q),
  )
  const titleOf = (id: string): string => {
    const s = siblings.find(x => String(x.id ?? '') === id)
    return s ? String(s[titleField] ?? s.id ?? id) : id
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {value.length === 0 && !adding && readonly && (
        <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>—</span>
      )}
      {value.map(bid => (
        <span
          key={bid}
          className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs"
          style={{
            background: 'var(--bg)',
            border: '1px solid var(--border)',
            color: 'var(--text)',
          }}
        >
          {titleOf(bid)}
          {!readonly && (
            <button
              type="button"
              onClick={() => onChange(value.filter(v => v !== bid))}
              aria-label={`Remove ${bid}`}
              className="leading-none"
              style={{
                background: 'transparent',
                border: 'none',
                color: 'var(--text-secondary)',
                cursor: 'pointer',
                padding: 0,
              }}
            >
              ×
            </button>
          )}
        </span>
      ))}
      {!readonly && (
        adding ? (
          <div className="relative">
            <input
              autoFocus
              value={q}
              onChange={e => setQ(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Escape') {
                  setAdding(false)
                  setQ('')
                } else if (e.key === 'Enter' && filtered[0]) {
                  onChange([...value, filtered[0].id])
                  setQ('')
                }
              }}
              placeholder="search task…"
              className="text-xs rounded-full px-2 py-1"
              style={{
                background: 'var(--bg)',
                border: '1px solid var(--accent)',
                color: 'var(--text)',
                outline: 'none',
                width: 180,
              }}
            />
            {q && (
              <div
                className="absolute z-10 mt-1 rounded-md overflow-hidden"
                style={{
                  background: 'var(--bg-secondary)',
                  border: '1px solid var(--border)',
                  minWidth: 220,
                  maxHeight: 200,
                  overflowY: 'auto',
                }}
              >
                {filtered.length === 0 ? (
                  <div className="text-xs px-2 py-1" style={{ color: 'var(--text-secondary)' }}>
                    No match
                  </div>
                ) : (
                  filtered.slice(0, 8).map(c => (
                    <button
                      key={c.id}
                      type="button"
                      onClick={() => {
                        onChange([...value, c.id])
                        setQ('')
                      }}
                      className="block w-full text-left text-xs px-2 py-1"
                      style={{ background: 'transparent', border: 'none', color: 'var(--text)', cursor: 'pointer' }}
                    >
                      <span style={{ fontWeight: 500 }}>{c.title}</span>
                      <span style={{ color: 'var(--text-secondary)' }}> · {c.col}</span>
                    </button>
                  ))
                )}
              </div>
            )}
          </div>
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
        )
      )}
    </div>
  )
}

// SubtasksBlock — read-only list of cards whose parent_id points at us.
// Click-through navigates to the sub-task's own page.
function SubtasksBlock({
  tasks,
  groupBy,
  titleField,
}: {
  tasks: Record<string, unknown>[]
  groupBy: string
  titleField: string
}) {
  return (
    <div className="flex flex-col gap-1">
      {tasks.map(t => {
        const tid = String(t.id ?? '')
        const tTitle = String(t[titleField] ?? tid)
        const tCol = String(t[groupBy] ?? '')
        return (
          <div
            key={tid}
            className="flex items-center gap-2 px-2 py-1 rounded-md text-sm"
            style={{ background: 'var(--bg)', border: '1px solid var(--border)' }}
          >
            <StatusPill col={tCol} />
            <span style={{ flex: 1, color: 'var(--text)' }}>{tTitle}</span>
            <code style={{ fontSize: '0.7rem', color: 'var(--text-secondary)' }}>{tid}</code>
          </div>
        )
      })}
    </div>
  )
}

// BlockersBlock — list of cards this task is blocked_by, with status.
function BlockersBlock({
  blockers,
  groupBy,
  titleField,
}: {
  blockers: Record<string, unknown>[]
  groupBy: string
  titleField: string
}) {
  return (
    <div className="flex flex-col gap-1">
      {blockers.map(t => {
        const tid = String(t.id ?? '')
        const tTitle = String(t[titleField] ?? tid)
        const tCol = String(t[groupBy] ?? '')
        const isDone = ['done', 'shipped', 'cancelled'].includes(tCol.toLowerCase())
        return (
          <div
            key={tid}
            className="flex items-center gap-2 px-2 py-1 rounded-md text-sm"
            style={{
              background: 'var(--bg)',
              border: '1px solid var(--border)',
              opacity: isDone ? 0.55 : 1,
            }}
          >
            <StatusPill col={tCol} />
            <span style={{ flex: 1, color: 'var(--text)', textDecoration: isDone ? 'line-through' : 'none' }}>
              {tTitle}
            </span>
            <code style={{ fontSize: '0.7rem', color: 'var(--text-secondary)' }}>{tid}</code>
          </div>
        )
      })}
    </div>
  )
}

// StatusPill renders a small colored pill for a column value. Reused
// by the SubtasksBlock and BlockersBlock so both speak the same
// vocabulary visually.
// NewTaskBar renders an inline "+ New task" affordance above the
// kanban. Click → input expands → type title → Enter posts a new
// task .md to <source><slug>.md with frontmatter `{title, col: 'todo',
// created: <today>}`. SSE refreshes the kanban automatically.
//
// Slug is derived from the title (lowercase, alphanumerics + hyphens).
// Collisions are resolved by appending `-2`, `-3`, etc.
function NewTaskBar({ source }: { source: string }) {
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  function slugify(s: string): string {
    return s
      .toLowerCase()
      .trim()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 64) || 'task'
  }

  async function exists(path: string): Promise<boolean> {
    const res = await apiFetch(`/api/content/${path}`, { method: 'HEAD' })
    return res.ok
  }

  async function uniqueSlug(base: string): Promise<string> {
    const folder = source.replace(/\/$/, '')
    let slug = base
    for (let i = 2; await exists(`${folder}/${slug}`); i++) {
      slug = `${base}-${i}`
      if (i > 200) throw new Error('too many name collisions')
    }
    return slug
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    const t = title.trim()
    if (!t) return
    setBusy(true)
    setErr(null)
    try {
      const slug = await uniqueSlug(slugify(t))
      const folder = source.replace(/\/$/, '')
      const today = new Date().toISOString().slice(0, 10)
      const body = `---
title: ${JSON.stringify(t)}
col: todo
created: ${today}
---

# ${t}

`
      const res = await apiFetch(`/api/content/${folder}/${slug}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'text/markdown' },
        body,
      })
      if (!res.ok) {
        let msg = `create ${res.status}`
        try {
          const j = (await res.json()) as { error?: string; message?: string }
          msg = j.error ?? j.message ?? msg
        } catch { /* ignore */ }
        throw new Error(msg)
      }
      setTitle('')
      setOpen(false)
      // SSE will broadcast the file-updated event; useData refetches.
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'create failed')
    } finally {
      setBusy(false)
    }
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="text-sm inline-flex items-center gap-1.5 rounded-md px-3 py-1.5"
        style={{
          background: 'var(--bg)',
          border: '1px dashed var(--border)',
          color: 'var(--text-secondary)',
          cursor: 'pointer',
        }}
      >
        + New task
      </button>
    )
  }

  return (
    <form
      onSubmit={submit}
      className="flex items-center gap-2"
      style={{
        background: 'var(--bg)',
        border: '1px solid var(--accent)',
        borderRadius: '0.375rem',
        padding: '0.4rem',
      }}
    >
      <input
        autoFocus
        value={title}
        onChange={e => setTitle(e.target.value)}
        onKeyDown={e => {
          if (e.key === 'Escape') {
            setOpen(false)
            setTitle('')
            setErr(null)
          }
        }}
        placeholder="Task title…"
        className="flex-1 text-sm"
        style={{
          background: 'transparent',
          border: 'none',
          outline: 'none',
          color: 'var(--text)',
          padding: '0.25rem 0.5rem',
        }}
      />
      <button
        type="submit"
        disabled={busy || !title.trim()}
        className="text-sm inline-flex items-center rounded-md px-3 py-1"
        style={{
          background: title.trim() ? 'var(--accent)' : 'var(--bg-secondary)',
          color: title.trim() ? 'white' : 'var(--text-secondary)',
          border: 'none',
          cursor: title.trim() ? 'pointer' : 'default',
        }}
      >
        {busy ? 'Creating…' : 'Create'}
      </button>
      <button
        type="button"
        onClick={() => {
          setOpen(false)
          setTitle('')
          setErr(null)
        }}
        className="text-xs px-2"
        style={{
          background: 'transparent',
          border: 'none',
          color: 'var(--text-secondary)',
          cursor: 'pointer',
        }}
      >
        cancel
      </button>
      {err && (
        <span className="text-xs" style={{ color: 'var(--error)' }}>
          {err}
        </span>
      )}
    </form>
  )
}

function StatusPill({ col }: { col: string }) {
  const lower = col.toLowerCase()
  let color = 'var(--text-secondary)'
  if (lower === 'done' || lower === 'shipped') color = 'var(--success)'
  else if (lower === 'in-progress' || lower === 'in_progress') color = 'var(--accent)'
  else if (lower === 'blocked') color = 'var(--warning)'
  else if (lower === 'cancelled') color = 'var(--error)'
  return (
    <span
      className="inline-block rounded-full"
      style={{
        width: 8,
        height: 8,
        background: color,
        flexShrink: 0,
      }}
      title={col}
    />
  )
}

// TaskComments — append-only stream rendered below the task body.
//
// One stream per BOARD (not per task) keeps the catalog tidy: collection
// `tasks/` has a sibling stream `tasks.comments` (the trailing slash
// stripped → dot suffix). Each line is `{task_id, body, actor?, at?}`.
// We filter by task_id at render time so a busy board doesn't bloat the
// modal load.
//
// @mention support — appends pass through `dispatchInboxForValueWrite`
// server-side, so a comment with `@dana` produces an inbox item exactly
// like any other prose.
function TaskComments({
  taskId,
  source,
}: {
  taskId: string
  source: string
}) {
  const streamKey = source.replace(/\/$/, '') + '.comments'
  const me = useMe()
  type CommentLine = {
    task_id?: string
    body?: string
    actor?: string
    at?: string
    ts?: string
  }
  const [comments, setComments] = useState<CommentLine[] | null>(null)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const res = await apiFetch(`/api/data/${encodeURIComponent(streamKey)}?limit=200`)
      if (res.status === 404) {
        setComments([])
        return
      }
      if (!res.ok) throw new Error(`stream read ${res.status}`)
      const j = (await res.json()) as { lines?: Array<{ value: CommentLine; ts: string }> }
      const lines = (j.lines ?? [])
        .map(l => ({ ...l.value, ts: l.ts }))
        .filter(l => l.task_id === taskId)
      setComments(lines)
      setErr(null)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'comments load failed')
      setComments([])
    }
  }, [streamKey, taskId])

  useEffect(() => { void refresh() }, [refresh])

  async function submit(e: FormEvent) {
    e.preventDefault()
    const body = draft.trim()
    if (!body) return
    setBusy(true)
    try {
      const res = await apiFetch(`/api/data/${encodeURIComponent(streamKey)}?op=append`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          value: {
            task_id: taskId,
            body,
            actor: me?.username,
            at: new Date().toISOString(),
          },
        }),
      })
      if (!res.ok) {
        let msg = `append ${res.status}`
        try {
          const j = await res.json() as { message?: string; error?: string }
          msg = j.message ?? j.error ?? msg
        } catch { /* ignore */ }
        throw new Error(msg)
      }
      setDraft('')
      await refresh()
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'comment post failed')
    } finally {
      setBusy(false)
    }
  }

  function fmtTime(iso?: string): string {
    if (!iso) return ''
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    const now = new Date()
    const sameDay = d.toDateString() === now.toDateString()
    return sameDay
      ? d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
      : d.toLocaleDateString([], { month: 'short', day: 'numeric' })
  }

  return (
    <div className="flex flex-col gap-2">
      {comments === null ? (
        <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>Loading…</div>
      ) : comments.length === 0 ? (
        <div className="text-sm italic" style={{ color: 'var(--text-secondary)' }}>
          No comments yet.
        </div>
      ) : (
        <div className="flex flex-col gap-1.5">
          {comments.map((c, i) => (
            <div
              key={i}
              className="rounded-md p-2.5 text-sm"
              style={{
                background: 'var(--bg)',
                border: '1px solid var(--border)',
              }}
            >
              <div className="text-xs mb-1" style={{ color: 'var(--text-secondary)' }}>
                <strong style={{ color: 'var(--text)' }}>@{c.actor || 'unknown'}</strong>
                {' · '}
                {fmtTime(c.at || c.ts)}
              </div>
              <div style={{ color: 'var(--text)', whiteSpace: 'pre-wrap' }}>
                <RichText text={c.body || ''} />
              </div>
            </div>
          ))}
        </div>
      )}

      <form onSubmit={submit} className="flex gap-2 items-start mt-1">
        <textarea
          value={draft}
          onChange={e => setDraft(e.target.value)}
          onKeyDown={e => {
            // Cmd/Ctrl-Enter posts. Shift-Enter inserts a newline.
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
              e.preventDefault()
              void submit(e as unknown as FormEvent)
            }
          }}
          placeholder="Add a comment… @mentions are routed to inboxes. ⌘↵ to post."
          rows={2}
          className="text-sm rounded-md p-2 flex-1 resize-y"
          style={{
            background: 'var(--bg)',
            border: '1px solid var(--border)',
            color: 'var(--text)',
            outline: 'none',
            minHeight: 36,
          }}
        />
        <button
          type="submit"
          disabled={busy || !draft.trim()}
          aria-label="Post comment"
          className="text-sm inline-flex items-center gap-1 rounded-md px-2.5 py-1.5"
          style={{
            background: draft.trim() ? 'var(--accent)' : 'var(--bg-secondary)',
            border: 'none',
            color: draft.trim() ? 'white' : 'var(--text-secondary)',
            cursor: draft.trim() ? 'pointer' : 'default',
            alignSelf: 'flex-end',
          }}
        >
          <Send size={12} />
          Post
        </button>
      </form>

      {err && (
        <div className="text-xs" style={{ color: 'var(--error)' }}>
          {err}
        </div>
      )}
    </div>
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
