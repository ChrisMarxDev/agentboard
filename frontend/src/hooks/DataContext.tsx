import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  useCallback,
  useMemo,
  type ReactNode,
} from 'react'
import { apiFetch, sseURL } from '../lib/session'

// Shape returned by POST /api/view/open. The broker resolves data and
// files server-side with its own authority, so the SPA never fetches
// /api/data/* directly for reads.
export interface ViewBundle {
  path: string
  title?: string
  source: string
  etag?: string
  data: Record<string, unknown>
  files: string[]
  subpages: Array<{ path: string; title: string }>
  authority: 'admin' | 'agent' | 'share' | 'anonymous' | 'unknown'
  last_actor?: string
  last_at?: string
  approval?: {
    approved_by: string
    approved_at: string
    approved_etag: string
    stale: boolean
  } | null
}

interface ViewContextType {
  bundle: ViewBundle | null
  // get/subscribe preserved so existing useData callers work unchanged.
  get: (key: string) => unknown
  subscribe: (key: string, callback: (value: unknown) => void) => () => void
  // Legacy fields kept as no-ops so older components still compile.
  data: Record<string, unknown>
  fetchKey: (key: string) => Promise<void>
  fetchAll: () => Promise<void>
  // View-specific.
  path: string | null
  loading: boolean
  error: string | null
  reopen: () => Promise<void>
}

const ViewContext = createContext<ViewContextType | null>(null)

export function useDataContext() {
  const ctx = useContext(ViewContext)
  if (!ctx) throw new Error('useDataContext must be used within DataProvider')
  return ctx
}

export function useViewBundle() {
  return useDataContext().bundle
}

interface DataProviderProps {
  children: ReactNode
  path: string | null
}

// DataProvider is the view broker's client. Mounted with a path, it
// POSTs /api/view/open, stashes the returned bundle in state, and
// subscribes to /api/view/events for live updates — scoped to the
// server-authorised key-set. Out-of-scope keys are never seen.
//
// Kept the name DataProvider (rather than ViewProvider) to minimise
// churn at call sites; the import path is identical.
export function DataProvider({ children, path }: DataProviderProps) {
  const [bundle, setBundle] = useState<ViewBundle | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState<boolean>(Boolean(path))
  const subscribersRef = useRef<Map<string, Set<(value: unknown) => void>>>(new Map())
  const esRef = useRef<EventSource | null>(null)

  const notify = useCallback((key: string, value: unknown) => {
    const subs = subscribersRef.current.get(key)
    subs?.forEach(cb => cb(value))
  }, [])

  // Single round-trip page open. Replaces the old fetchAll() + per-key
  // calls. Data outside the view's scope simply isn't in the response.
  const open = useCallback(async (p: string) => {
    setLoading(true)
    setError(null)
    try {
      const res = await apiFetch('/api/view/open', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: p }),
      })
      if (!res.ok) {
        // 404 = page doesn't exist at this path. Not an error
        // condition — let PageRenderer's fallback resolve to a folder
        // landing or render the "page not found" affordance. Reserve
        // the error channel for actual failures (5xx, network) and
        // for auth (401/403, surfaced as the AuthRequiredPanel).
        if (res.status === 404) {
          setBundle(null)
          return
        }
        setError(`view/open ${p} → ${res.status}`)
        setBundle(null)
        return
      }
      const b = (await res.json()) as ViewBundle
      setBundle(b)
      // Replay all initial values to existing subscribers.
      for (const [k, v] of Object.entries(b.data)) notify(k, v)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'view/open failed')
    } finally {
      setLoading(false)
    }
  }, [notify])

  useEffect(() => {
    if (!path) {
      setBundle(null)
      setLoading(false)
      return
    }
    void open(path)
  }, [path, open])

  // SSE — scoped per view. Reopened whenever path changes.
  useEffect(() => {
    if (!path) return
    let retryDelay = 1000
    let cancelled = false

    function connect() {
      if (cancelled) return
      const url = sseURL(`/api/view/events?path=${encodeURIComponent(path!)}`)
      const es = new EventSource(url, { withCredentials: true })
      esRef.current = es

      es.addEventListener('data', (evt) => {
        try {
          const { key, value } = JSON.parse((evt as MessageEvent).data)
          setBundle(prev => prev ? { ...prev, data: { ...prev.data, [key]: value } } : prev)
          notify(key, value)
        } catch {
          // ignore
        }
      })

      es.addEventListener('scope-changed', () => {
        // The page was edited and now references a different set of
        // data/files. Re-open to refresh the bundle.
        if (!cancelled) void open(path!)
      })

      es.addEventListener('page-updated', () => {
        if (!cancelled) void open(path!)
        window.dispatchEvent(new CustomEvent('agentboard:page-updated'))
      })

      es.addEventListener('ready', () => {
        retryDelay = 1000
      })

      es.onerror = () => {
        es.close()
        if (cancelled) return
        setTimeout(() => {
          retryDelay = Math.min(retryDelay * 2, 30000)
          connect()
        }, retryDelay)
      }
    }

    connect()
    return () => {
      cancelled = true
      esRef.current?.close()
    }
  }, [path, open, notify])

  const get = useCallback(
    (key: string) => (bundle ? bundle.data[key] : undefined),
    [bundle],
  )

  const subscribe = useCallback(
    (key: string, callback: (value: unknown) => void) => {
      if (!subscribersRef.current.has(key)) {
        subscribersRef.current.set(key, new Set())
      }
      subscribersRef.current.get(key)!.add(callback)
      return () => {
        subscribersRef.current.get(key)?.delete(callback)
      }
    },
    [],
  )

  // Legacy no-op stubs — preserved so any stragglers compiling against
  // the old ctx shape don't crash. New code uses `bundle` directly.
  const fetchKey = useCallback(async () => {}, [])
  const fetchAll = useCallback(async () => {}, [])
  const data = bundle?.data ?? ({} as Record<string, unknown>)

  const reopen = useCallback(async () => {
    if (path) await open(path)
  }, [path, open])

  const value = useMemo<ViewContextType>(
    () => ({
      bundle,
      get,
      subscribe,
      data,
      fetchKey,
      fetchAll,
      path,
      loading,
      error,
      reopen,
    }),
    [bundle, get, subscribe, data, fetchKey, fetchAll, path, loading, error, reopen],
  )

  return <ViewContext.Provider value={value}>{children}</ViewContext.Provider>
}
