// Client-side state for the Grab feature. Mirrors internal/grab/slug.go —
// the kebab-case rules must match or picks won't resolve on the server.
//
// State lives in localStorage so picks survive navigation across pages;
// the useGrab hook exposes subscribe/add/remove/clear.

const PICKS_KEY = 'agentboard:grab:picks'
const MODE_KEY = 'agentboard:grab:mode'

export interface Pick {
  page: string   // window.location.pathname at pick time
  cardId: string // slug(title)
  cardTitle?: string
}

/** Slugify a Card title — same rules as Go's grab.Slug. */
export function slug(title: string): string {
  let out = ''
  let prevDash = true
  for (const ch of title) {
    // Unicode letter or digit test via regex on the grapheme.
    if (/[\p{L}\p{N}]/u.test(ch)) {
      out += ch.toLowerCase()
      prevDash = false
    } else {
      if (!prevDash) {
        out += '-'
        prevDash = true
      }
    }
  }
  return out.replace(/^-+|-+$/g, '')
}

// ---- localStorage helpers (no-op if unavailable) ----

function readPicks(): Pick[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = window.localStorage.getItem(PICKS_KEY)
    if (!raw) return []
    const v = JSON.parse(raw)
    return Array.isArray(v) ? (v as Pick[]) : []
  } catch {
    return []
  }
}

function writePicks(picks: Pick[]): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(PICKS_KEY, JSON.stringify(picks))
  } catch {
    // quota exceeded — not fatal
  }
  notify()
}

export function getPicks(): Pick[] {
  return readPicks()
}

export function isPicked(page: string, cardId: string): boolean {
  return readPicks().some(p => p.page === page && p.cardId === cardId)
}

export function addPick(p: Pick): void {
  const picks = readPicks()
  if (picks.some(x => x.page === p.page && x.cardId === p.cardId)) return
  picks.push(p)
  writePicks(picks)
}

export function removePick(page: string, cardId: string): void {
  writePicks(readPicks().filter(p => !(p.page === page && p.cardId === cardId)))
}

export function togglePick(p: Pick): void {
  if (isPicked(p.page, p.cardId)) removePick(p.page, p.cardId)
  else addPick(p)
}

export function clearPicks(): void {
  writePicks([])
}

// ---- Grab mode on/off ----

export function getMode(): boolean {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(MODE_KEY) === '1'
}

export function setMode(on: boolean): void {
  if (typeof window === 'undefined') return
  window.localStorage.setItem(MODE_KEY, on ? '1' : '0')
  notify()
}

// ---- Subscribe / notify ----
// CustomEvent fires on every state change. Components subscribe via the
// useGrab hook; cross-tab sync is incidentally free thanks to the native
// 'storage' event but not explicitly required.

const EVENT = 'agentboard:grab:changed'

function notify(): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(EVENT))
}

export function subscribe(cb: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  window.addEventListener(EVENT, cb)
  // Also listen for other tabs updating the same localStorage key.
  const onStorage = (e: StorageEvent) => {
    if (e.key === PICKS_KEY || e.key === MODE_KEY) cb()
  }
  window.addEventListener('storage', onStorage)
  return () => {
    window.removeEventListener(EVENT, cb)
    window.removeEventListener('storage', onStorage)
  }
}

// ---- Format + copy ----

export type GrabFormat = 'markdown' | 'xml' | 'json'

export async function materialize(picks: Pick[], format: GrabFormat): Promise<string> {
  const body = JSON.stringify({
    picks: picks.map(p => ({ page: p.page, card_id: p.cardId })),
    format,
  })
  const r = await fetch('/api/grab', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body,
  })
  if (!r.ok) throw new Error(`grab failed: ${r.status}`)
  const data = await r.json()
  return String(data.text ?? '')
}

export async function copyToClipboard(text: string): Promise<void> {
  if (typeof navigator === 'undefined' || !navigator.clipboard) {
    throw new Error('clipboard API unavailable')
  }
  await navigator.clipboard.writeText(text)
}
