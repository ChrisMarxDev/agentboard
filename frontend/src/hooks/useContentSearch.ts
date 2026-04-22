import { useEffect, useState } from 'react'
import { apiFetch } from '../lib/session'

export interface SearchHit {
  path: string
  title: string
  snippet: string
  rank: number
}

/**
 * useContentSearch runs the server-side full-text search against page
 * content. Debounced to 200 ms so the sidebar doesn't hammer the endpoint
 * on every keystroke. Returns the current hits plus a `ready` flag that's
 * false while the first query is in flight — callers use it to decide
 * whether to render "No matches" or "Searching…".
 */
export function useContentSearch(query: string, enabled: boolean): {
  hits: SearchHit[]
  ready: boolean
} {
  const [hits, setHits] = useState<SearchHit[]>([])
  const [ready, setReady] = useState(true)

  useEffect(() => {
    if (!enabled || !query.trim()) {
      setHits([])
      setReady(true)
      return
    }

    setReady(false)
    const id = window.setTimeout(() => {
      apiFetch(`/api/search?q=${encodeURIComponent(query)}`)
        .then(r => (r.ok ? r.json() : []))
        .then((data: SearchHit[]) => {
          setHits(Array.isArray(data) ? data : [])
        })
        .catch(() => setHits([]))
        .finally(() => setReady(true))
    }, 200)

    return () => window.clearTimeout(id)
  }, [query, enabled])

  return { hits, ready }
}
