import { apiFetch } from './session'

// Client-side state for the Grab feature. Mirrors internal/grab/slug.go —
// the kebab-case rules must match or picks won't resolve on the server.
//
// Picks come in three kinds:
//   - card:    legacy; a <Card title="…"> hit with data-card-id
//   - heading: an H1/H2/H3 on a doc-style page, sliced from heading to next
//              heading at equal-or-higher level
//   - page:    the whole page, shortcut from the PageActionsMenu

const PICKS_KEY = 'agentboard:grab:picks'
const MODE_KEY = 'agentboard:grab:mode'

export type PickKind = 'card' | 'heading' | 'page'

export interface Pick {
  kind: PickKind
  page: string   // window.location.pathname at pick time
  // card
  cardId?: string
  cardTitle?: string
  // heading
  headingSlug?: string
  headingText?: string
  headingLevel?: 1 | 2 | 3
}

/** Slugify a Card title or heading — same rules as Go's grab.Slug. */
export function slug(title: string): string {
  let out = ''
  let prevDash = true
  for (const ch of title) {
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

// Unique key for a pick, used for dedupe / isPicked / removePick.
function pickKey(p: Pick): string {
  switch (p.kind) {
    case 'card':
      return `card:${p.page}#${p.cardId ?? ''}`
    case 'heading':
      return `heading:${p.page}#${p.headingSlug ?? ''}`
    case 'page':
      return `page:${p.page}`
  }
}

// ---- localStorage helpers (no-op if unavailable) ----

function readPicks(): Pick[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = window.localStorage.getItem(PICKS_KEY)
    if (!raw) return []
    const v = JSON.parse(raw)
    if (!Array.isArray(v)) return []
    // Tolerate legacy picks that predate the `kind` field — default to card.
    return v.map((p: Record<string, unknown>) => ({
      kind: (p.kind as PickKind) ?? 'card',
      page: String(p.page ?? ''),
      cardId: p.cardId != null ? String(p.cardId) : undefined,
      cardTitle: p.cardTitle != null ? String(p.cardTitle) : undefined,
      headingSlug: p.headingSlug != null ? String(p.headingSlug) : undefined,
      headingText: p.headingText != null ? String(p.headingText) : undefined,
      headingLevel: typeof p.headingLevel === 'number'
        ? (p.headingLevel as 1 | 2 | 3)
        : undefined,
    }))
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

export function isPickedByKey(key: string): boolean {
  return readPicks().some(p => pickKey(p) === key)
}

export function isCardPicked(page: string, cardId: string): boolean {
  return isPickedByKey(`card:${page}#${cardId}`)
}

export function isHeadingPicked(page: string, headingSlug: string): boolean {
  return isPickedByKey(`heading:${page}#${headingSlug}`)
}

export function isPagePicked(page: string): boolean {
  return isPickedByKey(`page:${page}`)
}

export function addPick(p: Pick): void {
  const picks = readPicks()
  const key = pickKey(p)
  if (picks.some(x => pickKey(x) === key)) return
  picks.push(p)
  writePicks(picks)
}

export function removePickByKey(key: string): void {
  writePicks(readPicks().filter(p => pickKey(p) !== key))
}

export function removePick(p: Pick): void {
  removePickByKey(pickKey(p))
}

export function togglePick(p: Pick): void {
  if (isPickedByKey(pickKey(p))) removePickByKey(pickKey(p))
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

const EVENT = 'agentboard:grab:changed'

function notify(): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(EVENT))
}

export function subscribe(cb: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  window.addEventListener(EVENT, cb)
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
    picks: picks.map(p => ({
      kind: p.kind,
      page: p.page,
      card_id: p.cardId,
      heading_slug: p.headingSlug,
      heading_level: p.headingLevel,
    })),
    format,
  })
  const r = await apiFetch('/api/grab', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body,
  })
  if (!r.ok) throw new Error(`grab failed: ${r.status}`)
  const data = await r.json()
  return String(data.text ?? '')
}

// Kept as a re-export so any existing callers still resolve; the
// implementation now lives in lib/clipboard with a plain-HTTP
// (execCommand) fallback so copy works on the hosted dogfood box.
export { copyToClipboard } from './clipboard'
