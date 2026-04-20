import { createContext, useContext, useEffect, useRef, useState, useCallback, type ReactNode } from 'react'

interface DataContextType {
  data: Record<string, unknown>
  get: (key: string) => unknown
  subscribe: (key: string, callback: (value: unknown) => void) => () => void
  fetchKey: (key: string) => Promise<void>
  fetchAll: () => Promise<void>
}

const DataContext = createContext<DataContextType | null>(null)

export function useDataContext() {
  const ctx = useContext(DataContext)
  if (!ctx) throw new Error('useDataContext must be used within DataProvider')
  return ctx
}

export function DataProvider({ children }: { children: ReactNode }) {
  const [data, setData] = useState<Record<string, unknown>>({})
  const subscribersRef = useRef<Map<string, Set<(value: unknown) => void>>>(new Map())
  const eventSourceRef = useRef<EventSource | null>(null)

  const notifySubscribers = useCallback((key: string, value: unknown) => {
    const subs = subscribersRef.current.get(key)
    if (subs) {
      subs.forEach(cb => cb(value))
    }
  }, [])

  // SSE connection
  useEffect(() => {
    let retryDelay = 1000

    function connect() {
      const es = new EventSource('/api/events')
      eventSourceRef.current = es

      es.addEventListener('data', (event) => {
        try {
          const { key, value } = JSON.parse(event.data)
          setData(prev => ({ ...prev, [key]: value }))
          notifySubscribers(key, value)
        } catch {
          // ignore parse errors
        }
      })

      es.addEventListener('page-updated', () => {
        // Trigger page re-render — handled by PageRenderer
        window.dispatchEvent(new CustomEvent('agentboard:page-updated'))
      })

      es.addEventListener('components-updated', () => {
        window.dispatchEvent(new CustomEvent('agentboard:components-updated'))
      })

      es.addEventListener('file-updated', (event) => {
        try {
          const detail = JSON.parse(event.data) as { name?: string; deleted?: boolean }
          window.dispatchEvent(new CustomEvent('agentboard:file-updated', { detail }))
        } catch {
          // ignore
        }
      })

      es.addEventListener('connected', () => {
        retryDelay = 1000
      })

      es.onerror = () => {
        es.close()
        setTimeout(() => {
          retryDelay = Math.min(retryDelay * 2, 30000)
          connect()
        }, retryDelay)
      }
    }

    connect()
    return () => {
      eventSourceRef.current?.close()
    }
  }, [notifySubscribers])

  // Initial data fetch
  const fetchAll = useCallback(async () => {
    try {
      const resp = await fetch('/api/data')
      const allData = await resp.json()
      setData(allData)
    } catch {
      // silent fail on initial fetch
    }
  }, [])

  useEffect(() => {
    fetchAll()
  }, [fetchAll])

  const get = useCallback((key: string) => data[key], [data])

  const subscribe = useCallback((key: string, callback: (value: unknown) => void) => {
    if (!subscribersRef.current.has(key)) {
      subscribersRef.current.set(key, new Set())
    }
    subscribersRef.current.get(key)!.add(callback)
    return () => {
      subscribersRef.current.get(key)?.delete(callback)
    }
  }, [])

  const fetchKey = useCallback(async (key: string) => {
    try {
      const resp = await fetch(`/api/data/${key}`)
      if (resp.ok) {
        const meta = await resp.json()
        setData(prev => ({ ...prev, [key]: meta.value }))
      }
    } catch {
      // silent
    }
  }, [])

  return (
    <DataContext.Provider value={{ data, get, subscribe, fetchKey, fetchAll }}>
      {children}
    </DataContext.Provider>
  )
}
