import { useEffect, useState } from 'react'
import { apiFetch } from '../lib/session'

// useUsers — cached fetch of the public user directory. Powers @mention
// resolution everywhere (Mention, RichText, assignee avatars, autocomplete).
//
// The directory is small (tens/hundreds of users for a team board), so we
// load it once per tab, keep it in a module-level cache, and refresh on
// demand. Deactivated users stay in the list so mentions of former users
// still resolve (dimmed) rather than rendering as plain text.

export interface PublicUser {
  username: string
  display_name?: string
  kind: 'admin' | 'agent'
  avatar_color?: string
  deactivated?: boolean
}

let cache: PublicUser[] | null = null
let inflight: Promise<PublicUser[]> | null = null
const subscribers = new Set<(users: PublicUser[]) => void>()

async function load(): Promise<PublicUser[]> {
  if (inflight) return inflight
  inflight = (async () => {
    try {
      const res = await apiFetch('/api/users')
      if (!res.ok) throw new Error(`${res.status}`)
      const body = (await res.json()) as { users?: PublicUser[] }
      cache = body.users ?? []
      subscribers.forEach((fn) => fn(cache!))
      return cache
    } finally {
      inflight = null
    }
  })()
  return inflight
}

/** Force a refresh — called after admin CRUD so changes land immediately. */
export function refreshUsers() {
  cache = null
  void load()
}

/** useUsers returns the public directory, refreshing lazily if empty. */
export function useUsers(): PublicUser[] {
  const [users, setUsers] = useState<PublicUser[]>(cache ?? [])

  useEffect(() => {
    let alive = true
    if (cache === null) {
      load().then((list) => {
        if (alive) setUsers(list)
      }).catch(() => {
        // Silent — mentions just fall back to plain text.
      })
    }
    const sub = (list: PublicUser[]) => {
      if (alive) setUsers(list)
    }
    subscribers.add(sub)
    return () => {
      alive = false
      subscribers.delete(sub)
    }
  }, [])

  return users
}

/** findUser does a case-insensitive lookup in the cache. */
export function findUser(users: PublicUser[], username: string): PublicUser | undefined {
  const needle = username.trim().toLowerCase()
  return users.find((u) => u.username.toLowerCase() === needle)
}
