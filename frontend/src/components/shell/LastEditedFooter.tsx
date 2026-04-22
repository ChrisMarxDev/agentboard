/**
 * Thin footer at the bottom of every rendered page that says who touched
 * it last and when. Part of the v0.5 dogfood cut — attribution without
 * the full activity feed. When nothing's recorded (older pages, seed
 * content), the footer renders nothing rather than bragging with
 * "anonymous · forever ago."
 */
export function LastEditedFooter({ actor, at }: { actor?: string; at?: string }) {
  if (!actor || actor === 'anonymous') return null

  const when = relativeTime(at)
  return (
    <div
      className="mt-12 pt-4 border-t text-xs flex items-center gap-2"
      style={{ color: 'var(--text-secondary)', borderColor: 'var(--border)' }}
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
          style={{ width: 6, height: 6, background: colorFor(actor) }}
        />
        {actor}
      </span>
      {when && <span>· {when}</span>}
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

// Deterministic pastel color per actor — stable across renders + sessions
// without any persistence. Rendered as a tiny dot next to the username.
function colorFor(actor: string): string {
  let h = 0
  for (let i = 0; i < actor.length; i++) {
    h = (h * 31 + actor.charCodeAt(i)) >>> 0
  }
  const hue = h % 360
  return `hsl(${hue}deg 55% 55%)`
}
