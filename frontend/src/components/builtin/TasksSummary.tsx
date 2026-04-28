import type { CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { CircleDashed, Lock, Clock } from 'lucide-react'
import { useData } from '../../hooks/useData'

// <TasksSummary source="tasks/" /> — three-up status strip for the team's
// task collection. NO chronological feed — just aggregate counts that
// answer "is the team's pile of work healthy?"
//
//   - open:        items where col != "done"
//   - blocked:     items with at least one blocked_by entry, still open
//   - due-this-week: items with `due` within 7 days, still open
//
// Each tile links to /tasks (with a hint query string the future task
// list view can read; the Tasks page itself ignores it for now).

interface TasksSummaryProps {
  source?: string  // folder collection path (default: "tasks/")
  href?: string    // where each tile links (default: "/tasks")
}

interface TaskCard {
  id?: string
  col?: string
  status?: string
  blocked_by?: unknown
  due?: string
}

const STAT: CSSProperties = {
  flex: 1,
  display: 'flex',
  flexDirection: 'column',
  gap: '0.35rem',
  padding: '0.75rem 1rem',
  borderRadius: '0.625rem',
  border: '1px solid var(--border)',
  background: 'var(--bg)',
  textDecoration: 'none',
  color: 'var(--text)',
}
const NUM: CSSProperties = {
  fontSize: '1.875rem',
  fontWeight: 700,
  lineHeight: 1,
}
const LABEL: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: '0.4rem',
  fontSize: '0.75rem',
  color: 'var(--text-secondary)',
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  fontWeight: 600,
}

export function TasksSummary({ source = 'tasks/', href = '/tasks' }: TasksSummaryProps) {
  const { data, loading } = useData(source)

  if (loading) {
    return (
      <div style={{ display: 'flex', gap: '0.75rem' }}>
        {[0, 1, 2].map(i => (
          <div key={i} style={{ ...STAT, opacity: 0.4 }}>
            <div style={NUM}>·</div>
            <div style={LABEL}>—</div>
          </div>
        ))}
      </div>
    )
  }

  const cards: TaskCard[] = Array.isArray(data) ? (data as TaskCard[]) : []
  const isOpen = (c: TaskCard) => {
    const col = (c.col ?? c.status ?? '').toString().toLowerCase()
    return col !== 'done' && col !== 'shipped' && col !== 'cancelled'
  }
  const isBlocked = (c: TaskCard) => {
    if (!isOpen(c)) return false
    return Array.isArray(c.blocked_by) && c.blocked_by.length > 0
  }
  const isDueThisWeek = (c: TaskCard) => {
    if (!isOpen(c) || !c.due) return false
    const due = new Date(c.due)
    if (Number.isNaN(due.getTime())) return false
    const now = new Date()
    const week = new Date(now.getTime() + 7 * 24 * 60 * 60 * 1000)
    return due <= week
  }

  const openCount = cards.filter(isOpen).length
  const blockedCount = cards.filter(isBlocked).length
  const dueCount = cards.filter(isDueThisWeek).length

  return (
    <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
      <Link to={href} style={STAT}>
        <span style={NUM}>{openCount}</span>
        <span style={LABEL}>
          <CircleDashed size={12} strokeWidth={2.5} />
          Open
        </span>
      </Link>
      <Link to={href} style={STAT}>
        <span style={{ ...NUM, color: blockedCount > 0 ? 'var(--warning)' : undefined }}>
          {blockedCount}
        </span>
        <span style={LABEL}>
          <Lock size={12} strokeWidth={2.5} />
          Blocked
        </span>
      </Link>
      <Link to={href} style={STAT}>
        <span style={NUM}>{dueCount}</span>
        <span style={LABEL}>
          <Clock size={12} strokeWidth={2.5} />
          Due ≤ 7d
        </span>
      </Link>
    </div>
  )
}
