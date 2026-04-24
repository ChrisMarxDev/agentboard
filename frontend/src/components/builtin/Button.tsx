import { useState } from 'react'
import { Check, Loader2, Zap } from 'lucide-react'
import { apiFetch } from '../../lib/session'
import { beaconError, resetBeacon } from '../../lib/errorBeacon'

// Button fires a webhook event on click. One button, one verb, no
// side-door mutations: the only thing it does is POST to
// /api/webhooks/fire. Subscribers decide what happens from there.
//
// Usage:
//
//   <Button fires="deploy.prod">Deploy</Button>
//   <Button fires="alert.pager" payload={{severity:"high"}} variant="danger">Page oncall</Button>
//   <Button fires="ops.rebuild" confirm="Rebuild the search index?">Rebuild index</Button>
//
// The `fires` prop is the outbound event name. Patterns on
// subscriptions decide who gets it. Keeping this component dumb
// (literally one POST per click) means the webhook bus stays the only
// fan-out mechanism — no hidden MCP calls, no special admin hooks.

interface ButtonProps {
  /** Outbound event name, e.g. "deploy.prod". */
  fires?: string
  /** Structured payload passed as event.data. */
  payload?: Record<string, unknown>
  /** Confirm-before-firing prompt text. */
  confirm?: string
  /** Visual weight: default / accent / danger. */
  variant?: 'default' | 'accent' | 'danger'
  /** Button label — falls back to children. */
  label?: string
  /** Disabled state. */
  disabled?: boolean
  children?: React.ReactNode
}

export function Button({
  fires,
  payload,
  confirm,
  variant = 'default',
  label,
  disabled,
  children,
}: ButtonProps) {
  const [busy, setBusy] = useState(false)
  const [fired, setFired] = useState(false)
  const [subs, setSubs] = useState<number | null>(null)

  const displayLabel = label ?? children ?? (fires ? `Fire ${fires}` : 'Button')
  const noEvent = !fires || fires.trim() === ''

  async function fire() {
    if (!fires || busy) return
    if (confirm && !window.confirm(confirm)) return
    setBusy(true)
    try {
      const res = await apiFetch('/api/webhooks/fire', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ event: fires, payload: payload ?? {} }),
      })
      if (!res.ok) throw new Error(`fire ${fires} → ${res.status}`)
      const body = (await res.json()) as { subscribers?: number }
      setSubs(typeof body.subscribers === 'number' ? body.subscribers : null)
      setFired(true)
      resetBeacon('Button', fires)
      setTimeout(() => setFired(false), 1800)
    } catch (e) {
      beaconError({
        component: 'Button',
        source: fires,
        error: e instanceof Error ? e.message : 'fire failed',
      })
    } finally {
      setBusy(false)
    }
  }

  // Variant palette — keeps the style decisions out of the caller.
  const palette = (() => {
    switch (variant) {
      case 'accent':
        return {
          bg: 'var(--accent)',
          fg: 'white',
          border: 'var(--accent)',
        }
      case 'danger':
        return {
          bg: 'var(--error)',
          fg: 'white',
          border: 'var(--error)',
        }
      default:
        return {
          bg: 'var(--bg-secondary)',
          fg: 'var(--text)',
          border: 'var(--border)',
        }
    }
  })()

  const isDisabled = disabled || busy || noEvent

  return (
    <span className="inline-flex items-center gap-2">
      <button
        type="button"
        onClick={() => void fire()}
        disabled={isDisabled}
        title={
          noEvent
            ? 'This button has no `fires` prop and does nothing.'
            : `Fires event: ${fires}`
        }
        className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium"
        style={{
          background: palette.bg,
          color: palette.fg,
          border: `1px solid ${palette.border}`,
          cursor: isDisabled ? 'not-allowed' : 'pointer',
          opacity: isDisabled ? 0.6 : 1,
          transition: 'opacity 120ms ease',
        }}
      >
        {busy ? (
          <Loader2 size={13} className="animate-spin" />
        ) : fired ? (
          <Check size={13} />
        ) : (
          <Zap size={13} />
        )}
        <span>{displayLabel}</span>
      </button>
      {fired && subs !== null && (
        <span
          className="text-xs"
          style={{ color: 'var(--text-secondary)' }}
        >
          fired — {subs} {subs === 1 ? 'subscriber' : 'subscribers'}
        </span>
      )}
    </span>
  )
}
