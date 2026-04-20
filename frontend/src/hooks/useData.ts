import { useState, useEffect } from 'react'
import { useDataContext } from './DataContext'

export function useData(source: string) {
  const context = useDataContext()
  const hasSource = Boolean(source)
  const [data, setData] = useState<unknown>(hasSource ? context.get(source) : undefined)
  const [loading, setLoading] = useState(hasSource && data === undefined)
  const [error, setError] = useState<Error | null>(null)

  useEffect(() => {
    if (!hasSource) {
      setData(undefined)
      setLoading(false)
      return
    }

    // Get current value
    const current = context.get(source)
    if (current !== undefined) {
      setData(current)
      setLoading(false)
    }

    // Subscribe to updates
    const unsubscribe = context.subscribe(source, (value) => {
      setData(value)
      setLoading(false)
    })

    // Fetch if we don't have it
    if (current === undefined) {
      context.fetchKey(source).catch(setError)
    }

    return unsubscribe
  }, [source, context, hasSource])

  return { data, loading, error }
}
