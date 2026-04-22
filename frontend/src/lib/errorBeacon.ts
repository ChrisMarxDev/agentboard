import { apiFetch, getToken } from './session'

/**
 * Beacon a render-time error to the server so agents can see what broke.
 * Never throws — a failing beacon must not become the second failure.
 *
 * Server-side dedupe + rate-limiting handle flooding, so we don't need to
 * throttle here beyond per-component "only post what changed".
 */
export interface BeaconPayload {
  component: string
  source?: string
  error: string
}

// Cache of last-beaconed signatures per component, so rapid re-renders (React
// StrictMode double-invocation, theme flicker, etc.) don't trigger duplicate
// POSTs within a single page session. Server dedupes independently.
const lastBeaconed = new Map<string, string>()

function signature(p: BeaconPayload): string {
  // First line of error + source — same dedupe rule as the server.
  const firstLine = (p.error || '').split('\n')[0].trim()
  return `${p.source ?? ''}|${firstLine}`
}

export function beaconError(payload: BeaconPayload): void {
  if (!payload.error) return

  const sig = signature(payload)
  const cacheKey = payload.component + '::' + (payload.source ?? '')
  if (lastBeaconed.get(cacheKey) === sig) return
  lastBeaconed.set(cacheKey, sig)

  const page = typeof window !== 'undefined' ? window.location.pathname : ''
  const body = JSON.stringify({ ...payload, page })

  try {
    // Prefer sendBeacon so the post survives page navigation. Falls back
    // to apiFetch. sendBeacon can't set headers, so we attach ?token= for
    // authenticated instances — same fallback the SSE connection uses.
    if (typeof navigator !== 'undefined' && navigator.sendBeacon) {
      const tok = getToken()
      const url = tok ? `/api/errors?token=${encodeURIComponent(tok)}` : '/api/errors'
      const blob = new Blob([body], { type: 'application/json' })
      navigator.sendBeacon(url, blob)
      return
    }
    apiFetch('/api/errors', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
      keepalive: true,
    }).catch(() => {})
  } catch {
    // Swallow — beacon failures are diagnostic, not functional.
  }
}

/** Clear the in-memory dedupe cache for a component source (e.g. after source
 *  changes via SSE so the next render's error surfaces even if identical). */
export function resetBeacon(component: string, source?: string): void {
  lastBeaconed.delete(component + '::' + (source ?? ''))
}
