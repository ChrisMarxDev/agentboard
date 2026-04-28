import { useEffect, useState, type CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { AtSign, UserPlus, CheckCircle, AlertTriangle, Inbox as InboxIcon, ArrowRight } from 'lucide-react'
import { apiFetch } from '../../lib/session'

// <InboxPreview limit={3} /> — the top items from /api/inbox plus a
// "you have N waiting" link to the full /inbox page. Polls every 30s
// like the full Inbox component.
//
// NOT a recency feed — the items themselves are intent-routed (only
// surface when something pings me) so the preview honors the same
// "intent over recency" rule.

interface InboxPreviewProps {
  limit?: number
  unreadOnly?: boolean
}

interface InboxItem {
  id: number
  kind: 'mention' | 'assignment' | 'approval_request' | 'webhook_failure'
  title: string
  subject_path?: string
  actor?: string
  read_at?: string
  at: string
}

const ICON_FOR_KIND = {
  mention: AtSign,
  assignment: UserPlus,
  approval_request: CheckCircle,
  webhook_failure: AlertTriangle,
} as const

const ROW: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: '0.6rem',
  padding: '0.5rem 0.75rem',
  borderRadius: '0.5rem',
  textDecoration: 'none',
  color: 'var(--text)',
  fontSize: '0.875rem',
}
const COUNT_LINK: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  gap: '0.35rem',
  padding: '0.4rem 0.75rem',
  borderRadius: '0.5rem',
  background: 'var(--accent-light)',
  color: 'var(--accent)',
  textDecoration: 'none',
  fontSize: '0.875rem',
  fontWeight: 600,
}

export function InboxPreview({ limit = 3, unreadOnly = true }: InboxPreviewProps) {
  const [items, setItems] = useState<InboxItem[] | null>(null)
  const [unreadCount, setUnreadCount] = useState<number | null>(null)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const qs = new URLSearchParams()
        if (unreadOnly) qs.set('unread', 'true')
        qs.set('limit', String(limit))
        const [listRes, countRes] = await Promise.all([
          apiFetch(`/api/inbox?${qs.toString()}`),
          apiFetch('/api/inbox/count'),
        ])
        if (cancelled) return
        if (listRes.ok) {
          const list = (await listRes.json()) as InboxItem[] | null
          setItems(Array.isArray(list) ? list : [])
        }
        if (countRes.ok) {
          const j = (await countRes.json()) as { unread?: number }
          setUnreadCount(j.unread ?? 0)
        }
      } catch {
        // network blip — keep prior state, retry on next tick
      }
    }
    void load()
    const id = setInterval(load, 30_000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [limit, unreadOnly])

  if (items === null) {
    return (
      <div style={{ color: 'var(--text-secondary)', fontSize: '0.875rem', padding: '0.5rem' }}>
        Loading inbox…
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <div
        style={{
          ...ROW,
          color: 'var(--text-secondary)',
          background: 'var(--bg-secondary)',
          fontStyle: 'italic',
        }}
      >
        <InboxIcon size={14} />
        No items waiting for you.
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
      {items.map(it => {
        const Icon = ICON_FOR_KIND[it.kind] ?? InboxIcon
        return (
          <Link
            key={it.id}
            to={it.subject_path ?? '/inbox'}
            style={{
              ...ROW,
              border: '1px solid var(--border)',
              background: 'var(--bg)',
            }}
          >
            <Icon size={14} style={{ color: 'var(--accent)', flexShrink: 0 }} />
            <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {it.title}
            </span>
            {it.actor && (
              <span style={{ fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
                @{it.actor}
              </span>
            )}
          </Link>
        )
      })}
      {unreadCount !== null && unreadCount > limit && (
        <Link to="/inbox" style={{ ...COUNT_LINK, alignSelf: 'flex-start' }}>
          View all {unreadCount} waiting
          <ArrowRight size={13} />
        </Link>
      )}
      {unreadCount !== null && unreadCount > 0 && unreadCount <= limit && (
        <Link
          to="/inbox"
          style={{
            ...COUNT_LINK,
            alignSelf: 'flex-start',
            background: 'transparent',
            color: 'var(--text-secondary)',
            fontWeight: 500,
          }}
        >
          Open Inbox
          <ArrowRight size={13} />
        </Link>
      )}
    </div>
  )
}
