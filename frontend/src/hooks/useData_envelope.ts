// useData — direct read against /api/data/{key} with SSE-driven
// invalidation. Parallel to useData (which goes through the view
// broker). Used by the <DataView> component on the showcase page so
// agents and humans can watch envelope state change live without
// touching the legacy data path.
//
// Phase 4 (per spec-file-storage.md) eventually replaces useData with
// envelope-aware semantics; until that lands, this hook is the escape
// hatch for pages that explicitly want v2 reads.

import { useCallback, useEffect, useRef, useState } from 'react'
import { apiFetch } from '../lib/session'

// Envelope mirrors internal/store/envelope.go. Kept loose (untyped
// `value`) because the user's data can be any JSON.
export interface V2Envelope {
  _meta: {
    version: string
    created_at?: string
    modified_by?: string
    shape?: 'singleton' | 'collection' | 'stream'
  }
  value: unknown
}

export interface V2CollectionItem {
  id: string
  envelope: V2Envelope
}

// V2Read covers any of the three response shapes /api/data returns:
//   - singleton: V2Envelope
//   - collection: { _meta, items: [...] }
//   - stream: { _meta, lines: [...] }
// Discriminate via _meta.shape.
export type V2Read =
  | V2Envelope
  | { _meta: { shape: 'collection'; key: string; count: number }; items: V2CollectionItem[] }
  | { _meta: { shape: 'stream'; key: string; line_count: number }; lines: Array<{ ts: string; value: unknown }> }

// useData fetches a key on mount and re-fetches whenever an SSE
// event for that key arrives. Errors and loading state surfaced like
// the legacy useData so consumers can mirror UX.
export function useData(key: string) {
  const [data, setData] = useState<V2Read | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const fetchOnce = useCallback(async () => {
    if (!key) {
      setData(null)
      setLoading(false)
      return
    }
    abortRef.current?.abort()
    const ctrl = new AbortController()
    abortRef.current = ctrl
    try {
      const res = await apiFetch(`/api/data/${encodeURIComponent(key)}`, { signal: ctrl.signal })
      if (!res.ok) {
        if (res.status === 404) {
          setData(null)
          setLoading(false)
          setError(null)
          return
        }
        throw new Error(`v2 fetch ${key} → ${res.status}`)
      }
      const body = (await res.json()) as V2Read
      setData(body)
      setLoading(false)
      setError(null)
    } catch (e) {
      if ((e as { name?: string }).name === 'AbortError') return
      setError(e instanceof Error ? e : new Error('fetch failed'))
      setLoading(false)
    }
  }, [key])

  useEffect(() => {
    fetchOnce()

    // Listen on the global SSE feed for v2 changes. The /api/events
    // stream emits "data" events whose payload is the store.Event
    // struct ({key, op, shape, version, id?}). When the key matches,
    // re-fetch — a tiny debounce avoids thrash if many writes land in
    // quick succession.
    const url = '/api/events'
    const es = new EventSource(url, { withCredentials: false })
    let timer: ReturnType<typeof setTimeout> | null = null

    const onMessage = (e: MessageEvent) => {
      try {
        const evt = JSON.parse(e.data) as { key?: string }
        if (evt.key !== key) return
        if (timer) clearTimeout(timer)
        timer = setTimeout(() => fetchOnce(), 60)
      } catch {
        // ignore malformed events
      }
    }
    es.addEventListener('data', onMessage)

    return () => {
      es.removeEventListener('data', onMessage)
      es.close()
      if (timer) clearTimeout(timer)
      abortRef.current?.abort()
    }
  }, [key, fetchOnce])

  return { data, loading, error, refetch: fetchOnce }
}
