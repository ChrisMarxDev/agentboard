import { useEffect, useRef, useState } from 'react'
import { useData } from '../../hooks/useData'
import { beaconError, resetBeacon } from '../../lib/errorBeacon'
import { apiFetch } from '../../lib/session'

interface KanbanProps {
  source: string
  groupBy: string
  columns?: string[]
  titleField?: string
}

export function Kanban({ source, groupBy, columns, titleField = 'title' }: KanbanProps) {
  const { data, loading } = useData(source)
  const [openCard, setOpenCard] = useState<Record<string, unknown> | null>(null)
  const [dragOverCol, setDragOverCol] = useState<string | null>(null)
  const draggingIdRef = useRef<string | null>(null)

  if (loading) {
    return <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>Loading...</div>
  }

  if (!data || !Array.isArray(data)) return null

  const items = data as Record<string, unknown>[]

  const groups = new Map<string, Record<string, unknown>[]>()
  for (const item of items) {
    const group = String(item[groupBy] ?? 'other')
    if (!groups.has(group)) groups.set(group, [])
    groups.get(group)!.push(item)
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

  async function handleDrop(targetCol: string) {
    const id = draggingIdRef.current
    draggingIdRef.current = null
    setDragOverCol(null)
    if (!id) return
    const card = items.find(it => it.id != null && String(it.id) === id)
    if (!card) return
    if (String(card[groupBy] ?? 'other') === targetCol) return

    try {
      const res = await apiFetch(`/api/data/${encodeURIComponent(source)}/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ [groupBy]: targetCol }),
      })
      if (!res.ok) throw new Error(`PATCH ${source}/${id} → ${res.status}`)
      resetBeacon('Kanban', source)
    } catch (e) {
      beaconError({
        component: 'Kanban',
        source,
        error: e instanceof Error ? e.message : 'card move failed',
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
              className="min-w-[220px] flex-1 rounded-lg p-3"
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
                {colItems.map((item, i) => {
                  const id = item.id != null ? String(item.id) : null
                  const draggable = id !== null
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
                      }}
                      onClick={() => setOpenCard(item)}
                      role="button"
                      tabIndex={0}
                      onKeyDown={e => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          setOpenCard(item)
                        }
                      }}
                      className="p-3 rounded-md text-sm select-none"
                      style={{
                        background: 'var(--bg)',
                        border: '1px solid var(--border)',
                        color: 'var(--text)',
                        cursor: draggable ? 'grab' : 'pointer',
                      }}
                    >
                      {String(item[titleField] ?? item.name ?? item.id ?? '')}
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
          titleField={titleField}
          onClose={() => setOpenCard(null)}
        />
      )}
    </>
  )
}

interface KanbanCardModalProps {
  card: Record<string, unknown>
  titleField: string
  onClose: () => void
}

function KanbanCardModal({ card, titleField, onClose }: KanbanCardModalProps) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const title = String(card[titleField] ?? card.name ?? card.id ?? '')
  const entries = Object.entries(card).filter(([k]) => k !== titleField)

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
        className="rounded-lg border w-full max-w-lg"
        style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border)' }}
      >
        <div
          className="flex items-center justify-between px-5 py-3 border-b gap-4"
          style={{ borderColor: 'var(--border)' }}
        >
          <div className="font-semibold text-sm truncate" style={{ color: 'var(--text)' }}>
            {title || 'Card'}
          </div>
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
        <div className="px-5 py-4">
          {entries.length === 0 ? (
            <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>
              No additional fields.
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              {entries.map(([k, v]) => (
                <div key={k} className="grid grid-cols-[8rem_1fr] gap-4 items-start text-sm">
                  <div style={{ color: 'var(--text-secondary)' }}>{k}</div>
                  <div
                    className="whitespace-pre-wrap break-words"
                    style={{ color: 'var(--text)' }}
                  >
                    {formatValue(v)}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
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
