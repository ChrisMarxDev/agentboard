import { useCallback, useEffect, useState } from 'react'
import { AtSign, UserPlus, CheckCircle, AlertTriangle, Inbox as InboxIcon, Archive, Check, X } from 'lucide-react'
import { apiFetch } from '../../lib/session'
import { beaconError } from '../../lib/errorBeacon'

// Inbox renders the current user's reminder queue. Polls /api/inbox +
// /api/inbox/count on mount and again every 30s. Entries are click-
// navigable to their subject_path, plus per-row mark-read / archive /
// delete actions and a bulk "mark all read" at the top.
//
// Shape assumptions match the server-side inbox.Item:
//   kind ∈ mention | assignment | approval_request | webhook_failure

interface InboxItem {
  id: number
  kind: 'mention' | 'assignment' | 'approval_request' | 'webhook_failure'
  title: string
  subject_path?: string
  subject_ref?: string
  actor?: string
  at: string
  read_at?: string
  archived_at?: string
}

interface InboxProps {
  /** Show only unread. Handy for nav-adjacent widgets. */
  unreadOnly?: boolean
  /** Max items to load. */
  limit?: number
}

export function Inbox({ unreadOnly, limit }: InboxProps) {
  const [items, setItems] = useState<InboxItem[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const qs = new URLSearchParams()
      if (unreadOnly) qs.set('unread', 'true')
      if (limit) qs.set('limit', String(limit))
      const q = qs.toString()
      const res = await apiFetch('/api/inbox' + (q ? '?' + q : ''))
      if (!res.ok) throw new Error(`/api/inbox → ${res.status}`)
      setItems((await res.json()) as InboxItem[])
      setErr(null)
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'inbox load failed'
      setErr(msg)
      beaconError({ component: 'Inbox', source: '', error: msg })
    }
  }, [unreadOnly, limit])

  useEffect(() => {
    void refresh()
    const id = setInterval(() => void refresh(), 30_000)
    return () => clearInterval(id)
  }, [refresh])

  const markRead = async (id: number) => {
    setBusy(true)
    try {
      const res = await apiFetch(`/api/inbox/${id}/read`, { method: 'POST' })
      if (!res.ok) throw new Error(`read ${id} → ${res.status}`)
      await refresh()
    } catch (e) {
      beaconError({ component: 'Inbox', source: String(id), error: e instanceof Error ? e.message : 'mark-read failed' })
    } finally {
      setBusy(false)
    }
  }
  const archive = async (id: number) => {
    setBusy(true)
    try {
      const res = await apiFetch(`/api/inbox/${id}/archive`, { method: 'POST' })
      if (!res.ok) throw new Error(`archive ${id} → ${res.status}`)
      await refresh()
    } catch (e) {
      beaconError({ component: 'Inbox', source: String(id), error: e instanceof Error ? e.message : 'archive failed' })
    } finally {
      setBusy(false)
    }
  }
  const del = async (id: number) => {
    setBusy(true)
    try {
      const res = await apiFetch(`/api/inbox/${id}`, { method: 'DELETE' })
      if (!res.ok) throw new Error(`delete ${id} → ${res.status}`)
      await refresh()
    } catch (e) {
      beaconError({ component: 'Inbox', source: String(id), error: e instanceof Error ? e.message : 'delete failed' })
    } finally {
      setBusy(false)
    }
  }
  const markAll = async () => {
    setBusy(true)
    try {
      const res = await apiFetch('/api/inbox/read-all', { method: 'POST' })
      if (!res.ok) throw new Error(`read-all → ${res.status}`)
      await refresh()
    } catch (e) {
      beaconError({ component: 'Inbox', source: '', error: e instanceof Error ? e.message : 'read-all failed' })
    } finally {
      setBusy(false)
    }
  }

  if (items === null) {
    return (
      <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>
        Loading inbox…
      </div>
    )
  }
  if (err) {
    return (
      <div className="p-4 text-sm" style={{ color: 'var(--error)' }}>
        {err}
      </div>
    )
  }
  if (items.length === 0) {
    return (
      <div
        className="px-4 py-10 rounded-lg border text-center text-sm"
        style={{
          background: 'var(--bg-secondary)',
          borderColor: 'var(--border)',
          color: 'var(--text-secondary)',
        }}
      >
        <InboxIcon size={22} className="mx-auto mb-2" />
        Nothing to look at. You&apos;ll see mentions, assignments, approval asks, and
        webhook failures here.
      </div>
    )
  }

  const unread = items.filter(i => !i.read_at).length

  return (
    <div
      className="rounded-lg border"
      style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border)' }}
    >
      <div
        className="flex items-center justify-between px-4 py-2 border-b"
        style={{ borderColor: 'var(--border)' }}
      >
        <div className="text-sm" style={{ color: 'var(--text)' }}>
          {unread} unread · {items.length} total
        </div>
        {unread > 0 && (
          <button
            type="button"
            onClick={() => void markAll()}
            disabled={busy}
            className="text-xs inline-flex items-center gap-1 rounded-md px-2 py-1"
            style={{
              background: 'transparent',
              border: '1px solid var(--border)',
              color: 'var(--text-secondary)',
              cursor: busy ? 'not-allowed' : 'pointer',
            }}
          >
            <Check size={12} /> Mark all read
          </button>
        )}
      </div>

      <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
        {items.map(it => (
          <InboxRow
            key={it.id}
            item={it}
            busy={busy}
            onRead={() => void markRead(it.id)}
            onArchive={() => void archive(it.id)}
            onDelete={() => void del(it.id)}
          />
        ))}
      </ul>
    </div>
  )
}

function InboxRow({
  item,
  busy,
  onRead,
  onArchive,
  onDelete,
}: {
  item: InboxItem
  busy: boolean
  onRead: () => void
  onArchive: () => void
  onDelete: () => void
}) {
  const isRead = Boolean(item.read_at)
  const kindConfig = kindInfo(item.kind)

  const href = item.subject_path
    ? item.subject_ref
      ? `${item.subject_path}#${item.subject_ref}`
      : item.subject_path
    : undefined

  return (
    <li
      className="flex items-start gap-3 px-4 py-3 border-b"
      style={{
        borderColor: 'var(--border)',
        opacity: isRead ? 0.65 : 1,
      }}
    >
      {/* Kind icon */}
      <div
        className="mt-0.5 flex items-center justify-center rounded-full"
        style={{
          width: 28,
          height: 28,
          background: kindConfig.bg,
          color: kindConfig.fg,
          flexShrink: 0,
        }}
        title={item.kind}
      >
        <kindConfig.Icon size={14} />
      </div>

      {/* Main column */}
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="text-sm" style={{ color: 'var(--text)' }}>
          {href ? (
            <a
              href={href}
              onClick={() => {
                // Clicking through implicitly marks read so users don't
                // have to double-interact. Same as most mail clients.
                if (!isRead) onRead()
              }}
              style={{ color: 'var(--text)', textDecoration: 'none' }}
            >
              {item.title}
            </a>
          ) : (
            <span>{item.title}</span>
          )}
        </div>
        <div
          className="text-xs mt-0.5"
          style={{ color: 'var(--text-secondary)' }}
        >
          {item.actor && <>by @{item.actor} · </>}
          {relativeTime(item.at)}
          {item.subject_path && (
            <>
              {' · '}
              <span style={{ fontFamily: 'monospace' }}>{item.subject_path}</span>
            </>
          )}
        </div>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-1 flex-shrink-0">
        {!isRead && (
          <button
            type="button"
            title="Mark read"
            onClick={onRead}
            disabled={busy}
            className="inline-flex items-center justify-center rounded-md"
            style={{
              width: 26,
              height: 26,
              background: 'transparent',
              border: '1px solid var(--border)',
              color: 'var(--text-secondary)',
              cursor: busy ? 'not-allowed' : 'pointer',
            }}
          >
            <Check size={12} />
          </button>
        )}
        <button
          type="button"
          title="Archive"
          onClick={onArchive}
          disabled={busy}
          className="inline-flex items-center justify-center rounded-md"
          style={{
            width: 26,
            height: 26,
            background: 'transparent',
            border: '1px solid var(--border)',
            color: 'var(--text-secondary)',
            cursor: busy ? 'not-allowed' : 'pointer',
          }}
        >
          <Archive size={12} />
        </button>
        <button
          type="button"
          title="Delete"
          onClick={onDelete}
          disabled={busy}
          className="inline-flex items-center justify-center rounded-md"
          style={{
            width: 26,
            height: 26,
            background: 'transparent',
            border: '1px solid var(--border)',
            color: 'var(--error)',
            cursor: busy ? 'not-allowed' : 'pointer',
          }}
        >
          <X size={12} />
        </button>
      </div>
    </li>
  )
}

function kindInfo(kind: InboxItem['kind']) {
  switch (kind) {
    case 'mention':
      return { Icon: AtSign, bg: 'rgba(59, 130, 246, 0.12)', fg: 'rgb(37, 99, 235)' }
    case 'assignment':
      return { Icon: UserPlus, bg: 'rgba(139, 92, 246, 0.12)', fg: 'rgb(124, 58, 237)' }
    case 'approval_request':
      return { Icon: CheckCircle, bg: 'rgba(34, 197, 94, 0.12)', fg: 'rgb(22, 163, 74)' }
    case 'webhook_failure':
      return { Icon: AlertTriangle, bg: 'rgba(239, 68, 68, 0.12)', fg: 'rgb(220, 38, 38)' }
    default:
      return { Icon: InboxIcon, bg: 'var(--bg)', fg: 'var(--text-secondary)' }
  }
}

function relativeTime(iso: string): string {
  const then = Date.parse(iso)
  if (!Number.isFinite(then)) return iso
  const diff = Math.round((Date.now() - then) / 1000)
  if (diff < 10) return 'just now'
  if (diff < 60) return `${diff}s ago`
  const m = Math.round(diff / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 48) return `${h}h ago`
  const d = Math.round(h / 24)
  if (d < 30) return `${d}d ago`
  const mo = Math.round(d / 30)
  if (mo < 12) return `${mo}mo ago`
  return `${Math.round(mo / 12)}y ago`
}
