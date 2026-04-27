import { useEffect, useState } from 'react'
import { fetchMe, type Me } from '../lib/auth'

// useMe — shell-level hook for "who am I signed in as." One module-level
// cache so multiple consumers in the same render tree share one fetch.
// Returns null while loading or when unauthenticated; the UserMenu uses
// this to decide whether to render or hide.

let cache: Me | null | undefined = undefined // undefined = not fetched yet
let inflight: Promise<Me | null> | null = null
const subscribers = new Set<(me: Me | null) => void>()

async function load(): Promise<Me | null> {
  if (inflight) return inflight
  inflight = (async () => {
    try {
      const me = await fetchMe()
      cache = me
      subscribers.forEach((fn) => fn(me))
      return me
    } catch {
      cache = null
      subscribers.forEach((fn) => fn(null))
      return null
    } finally {
      inflight = null
    }
  })()
  return inflight
}

/** Force a refresh — call after login, redeem, or a kind-changing admin edit. */
export function refreshMe() {
  cache = undefined
  void load()
}

export function useMe(): Me | null {
  const [me, setMe] = useState<Me | null>(cache === undefined ? null : cache)

  useEffect(() => {
    let alive = true
    if (cache === undefined) {
      load().then((m) => {
        if (alive) setMe(m)
      })
    }
    const sub = (m: Me | null) => {
      if (alive) setMe(m)
    }
    subscribers.add(sub)
    return () => {
      alive = false
      subscribers.delete(sub)
    }
  }, [])

  return me
}
