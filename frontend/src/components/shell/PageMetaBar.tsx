import { useState } from 'react'
import { Check, CircleAlert, CircleDot } from 'lucide-react'
import { apiFetch, isPublicMode } from '../../lib/session'
import { beaconError } from '../../lib/errorBeacon'

// PageMetaBar is the single top-of-page meta segment: last-edited
// attribution on the left, approval state + action on the right.
//
// Approval is a best-effort signal — any authenticated user can
// toggle it, and any page write auto-invalidates it (etag changes
// server-side, `stale` flips true on the next read). Anonymous
// visitors (public mode / share-link mode) see the state read-only.

export interface PageApprovalState {
  approved_by: string
  approved_at: string
  approved_etag: string
  stale: boolean
}

interface Props {
  pagePath: string
  lastActor?: string
  lastAt?: string
  approval?: PageApprovalState | null
  onApprovalChange?: (a: PageApprovalState | null) => void
}

export function PageMetaBar({
  pagePath,
  lastActor,
  lastAt,
  approval,
  onApprovalChange,
}: Props) {
  const [busy, setBusy] = useState(false)
  const readOnly = isPublicMode()
  const normalisedPath = pagePath === 'index' ? '/' : '/' + pagePath.replace(/^\//, '')

  const hasLast = lastActor && lastActor !== 'anonymous'
  // Render nothing if we have absolutely nothing to show.
  if (!hasLast && !approval && readOnly) return null

  async function approve() {
    setBusy(true)
    try {
      const res = await apiFetch('/api/approval', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: normalisedPath }),
      })
      if (!res.ok) throw new Error(`approve → ${res.status}`)
      const body = (await res.json()) as PageApprovalState
      onApprovalChange?.({ ...body, stale: false })
    } catch (e) {
      beaconError({
        component: 'PageMetaBar',
        source: pagePath,
        error: e instanceof Error ? e.message : 'approve failed',
      })
    } finally {
      setBusy(false)
    }
  }

  async function revoke() {
    setBusy(true)
    try {
      const res = await apiFetch('/api/approval?path=' + encodeURIComponent(normalisedPath), {
        method: 'DELETE',
      })
      if (!res.ok) throw new Error(`revoke → ${res.status}`)
      onApprovalChange?.(null)
    } catch (e) {
      beaconError({
        component: 'PageMetaBar',
        source: pagePath,
        error: e instanceof Error ? e.message : 'revoke failed',
      })
    } finally {
      setBusy(false)
    }
  }

  // Two-state model: approved or not. Stale approvals still render as
  // "Approved" with a muted color; the user can unapprove to clear it.
  // There is no separate re-approve button — simpler is better.
  const approvalChip = (() => {
    if (!approval) {
      if (readOnly) return null
      return (
        <button
          type="button"
          onClick={approve}
          disabled={busy}
          className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs"
          title="Mark this version as approved"
          style={{
            background: 'transparent',
            border: '1px solid var(--border)',
            color: 'var(--text-secondary)',
            cursor: busy ? 'not-allowed' : 'pointer',
          }}
        >
          <Check size={12} />
          <span>Approve</span>
        </button>
      )
    }
    const title = approval.stale
      ? `Approved at an earlier version by ${approval.approved_by} (${approval.approved_at}). Unapprove and approve again to confirm the current version.`
      : `Approved by ${approval.approved_by} (${approval.approved_at})`
    const chip = (
      <span
        className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs"
        title={title}
        style={{
          background: approval.stale ? 'var(--bg-secondary)' : 'rgba(34, 197, 94, 0.08)',
          border:
            '1px solid ' +
            (approval.stale ? 'var(--border)' : 'rgba(34, 197, 94, 0.4)'),
          color: approval.stale ? 'var(--text-secondary)' : 'rgb(22, 163, 74)',
        }}
      >
        {approval.stale ? <CircleAlert size={12} /> : <CircleDot size={12} />}
        <span>
          Approved by <strong>{approval.approved_by}</strong>
        </span>
        <span style={{ color: 'var(--text-secondary)' }}>· {relativeTime(approval.approved_at)}</span>
      </span>
    )
    if (readOnly) return chip
    return (
      <span className="inline-flex items-center gap-2">
        {chip}
        <button
          type="button"
          onClick={revoke}
          disabled={busy}
          className="text-xs px-2 py-1 rounded-md"
          title="Remove approval"
          style={{
            background: 'transparent',
            border: '1px solid var(--border)',
            color: 'var(--text-secondary)',
            cursor: busy ? 'not-allowed' : 'pointer',
          }}
        >
          Unapprove
        </button>
      </span>
    )
  })()

  const editedChip = hasLast ? (
    <span
      className="inline-flex items-center gap-1.5 text-xs"
      style={{ color: 'var(--text-secondary)' }}
    >
      <span>Last edited by</span>
      <span
        className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full"
        style={{
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border)',
          color: 'var(--text)',
        }}
      >
        <span
          aria-hidden
          className="inline-block rounded-full"
          style={{ width: 6, height: 6, background: colorFor(lastActor!) }}
        />
        {lastActor}
      </span>
      {lastAt && <span>· {relativeTime(lastAt)}</span>}
    </span>
  ) : null

  // Three-column grid: left = last-edited, center = approval, right =
  // reserved whitespace so the absolute-positioned PageActionsMenu (⋯)
  // in the parent doesn't collide with the approval chip. Auto columns
  // on each side keep centering true regardless of the edited-chip's
  // width.
  return (
    <div
      className="mb-4 pb-3 border-b flex items-center flex-wrap gap-2"
      style={{
        borderColor: 'var(--border)',
        display: 'grid',
        gridTemplateColumns: '1fr auto 1fr',
        alignItems: 'center',
      }}
    >
      <div style={{ justifySelf: 'start' }}>{editedChip}</div>
      <div style={{ justifySelf: 'center' }}>{approvalChip}</div>
      {/* Empty slot — reserved for the floating ⋯ actions menu. */}
      <div style={{ justifySelf: 'end', minWidth: '2.5rem' }} aria-hidden />
    </div>
  )
}

function relativeTime(iso?: string): string {
  if (!iso) return ''
  const then = Date.parse(iso)
  if (Number.isNaN(then)) return ''
  const diffSec = Math.round((Date.now() - then) / 1000)
  if (diffSec < 10) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`
  const m = Math.round(diffSec / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 48) return `${h}h ago`
  const d = Math.round(h / 24)
  if (d < 30) return `${d}d ago`
  const mo = Math.round(d / 30)
  if (mo < 12) return `${mo}mo ago`
  return `${Math.round(mo / 12)}y ago`
}

function colorFor(actor: string): string {
  let h = 0
  for (let i = 0; i < actor.length; i++) {
    h = (h * 31 + actor.charCodeAt(i)) >>> 0
  }
  const hue = h % 360
  return `hsl(${hue}deg 55% 55%)`
}
