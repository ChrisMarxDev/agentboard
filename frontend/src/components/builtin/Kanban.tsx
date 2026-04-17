import { useData } from '../../hooks/useData'

interface KanbanProps {
  source: string
  groupBy: string
  columns?: string[]
  titleField?: string
}

export function Kanban({ source, groupBy, columns, titleField = 'title' }: KanbanProps) {
  const { data, loading } = useData(source)

  if (loading) {
    return <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>Loading...</div>
  }

  if (!data || !Array.isArray(data)) return null

  const items = data as Record<string, unknown>[]

  // Group items by the groupBy field
  const groups = new Map<string, Record<string, unknown>[]>()
  for (const item of items) {
    const group = String(item[groupBy] ?? 'other')
    if (!groups.has(group)) groups.set(group, [])
    groups.get(group)!.push(item)
  }

  // Determine column order
  const colOrder = columns ?? Array.from(groups.keys())

  const colLabels: Record<string, string> = {
    todo: 'To Do',
    in_progress: 'In Progress',
    done: 'Done',
    backlog: 'Backlog',
    review: 'Review',
    blocked: 'Blocked',
  }

  return (
    <div className="flex gap-4 overflow-x-auto my-4 pb-2">
      {colOrder.map(col => {
        const colItems = groups.get(col) ?? []
        return (
          <div
            key={col}
            className="min-w-[220px] flex-1 rounded-lg p-3"
            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}
          >
            <div className="text-sm font-medium mb-3 flex items-center justify-between" style={{ color: 'var(--text-secondary)' }}>
              <span>{colLabels[col] ?? col.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())}</span>
              <span className="text-xs px-1.5 py-0.5 rounded" style={{ background: 'var(--border)' }}>
                {colItems.length}
              </span>
            </div>
            <div className="space-y-2">
              {colItems.map((item, i) => (
                <div
                  key={String(item.id ?? i)}
                  className="p-3 rounded-md text-sm"
                  style={{ background: 'var(--bg)', border: '1px solid var(--border)', color: 'var(--text)' }}
                >
                  {String(item[titleField] ?? item.name ?? item.id ?? '')}
                </div>
              ))}
              {colItems.length === 0 && (
                <div className="text-xs py-4 text-center" style={{ color: 'var(--text-secondary)' }}>
                  Empty
                </div>
              )}
            </div>
          </div>
        )
      })}
    </div>
  )
}
