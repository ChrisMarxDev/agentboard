// Copy a page's raw MDX source to the clipboard. Success/failure is broadcast
// via a CustomEvent so a single toast component in the shell can render the
// feedback regardless of which UI surface triggered the copy (shortcut, menu).

import { beaconError } from './errorBeacon'
import { apiFetch } from './session'
import { copyToClipboard } from './clipboard'

export const COPY_EVENT = 'agentboard:page-copied'

export interface CopyEventDetail {
  ok: boolean
  pagePath: string
  error?: string
}

function broadcast(detail: CopyEventDetail) {
  window.dispatchEvent(new CustomEvent<CopyEventDetail>(COPY_EVENT, { detail }))
}

/**
 * Resolves the current page path from a location pathname.
 * `/` → `index`, `/runbooks/deploy` → `runbooks/deploy`.
 */
export function pagePathFromLocation(pathname: string): string {
  const stripped = pathname.replace(/^\/+/, '').replace(/\/+$/, '')
  return stripped === '' ? 'index' : stripped
}

/**
 * Fetches the raw MDX/markdown for a page and writes it to the clipboard.
 * Broadcasts the outcome so the shell can show a transient toast.
 */
export async function copyPageSource(pagePath: string): Promise<void> {
  if (!pagePath) {
    broadcast({ ok: false, pagePath, error: 'no page path' })
    return
  }
  try {
    const res = await apiFetch(`/api/${encodeURI(pagePath)}`, {
      headers: { Accept: 'text/markdown' },
    })
    if (!res.ok) {
      const body = await res.text().catch(() => '')
      throw new Error(body || `GET ${pagePath} → ${res.status}`)
    }
    const source = await res.text()
    const copied = await copyToClipboard(source)
    if (!copied) throw new Error('clipboard write rejected')
    broadcast({ ok: true, pagePath })
  } catch (e) {
    const msg = e instanceof Error ? e.message : 'copy failed'
    beaconError({ component: 'CopyPage', source: pagePath, error: msg })
    broadcast({ ok: false, pagePath, error: msg })
  }
}
