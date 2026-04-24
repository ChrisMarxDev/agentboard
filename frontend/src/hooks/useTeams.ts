import { useEffect, useState } from 'react'
import { apiFetch } from '../lib/session'

// useTeams — cached fetch of /api/teams. Mirrors useUsers exactly: one
// module-level cache, lazy load on first mount, refresh on demand. Used
// by <Mention> to resolve team pills and by <TeamRoster> to render a
// roster card in MDX. The admin UI uses its own uncached fetch path so
// form state doesn't get stomped mid-edit.

export interface TeamMember {
  username: string
  role?: string
  added_at: string
}

export interface Team {
  slug: string
  display_name?: string
  description?: string
  created_at: string
  created_by?: string
  members?: TeamMember[]
}

// Reserved pseudo-teams — the frontend shows them as team pills without
// hitting the server. Keep in lockstep with internal/teams.reservedSlugs.
export const RESERVED_TEAMS: Record<string, { display_name: string; description: string }> = {
  all: { display_name: 'Everyone', description: 'Every active user' },
  admins: { display_name: 'Admins', description: 'Every active admin' },
  agents: { display_name: 'Agents', description: 'Every active agent' },
  here: { display_name: 'Here', description: 'Users currently viewing (deferred)' },
}

let cache: Team[] | null = null
let inflight: Promise<Team[]> | null = null
const subscribers = new Set<(teams: Team[]) => void>()

async function load(): Promise<Team[]> {
  if (inflight) return inflight
  inflight = (async () => {
    try {
      const res = await apiFetch('/api/teams')
      if (!res.ok) throw new Error(`${res.status}`)
      const body = (await res.json()) as { teams?: Team[] }
      cache = body.teams ?? []
      subscribers.forEach((fn) => fn(cache!))
      return cache
    } finally {
      inflight = null
    }
  })()
  return inflight
}

export function refreshTeams() {
  cache = null
  void load()
}

export function useTeams(): Team[] {
  const [teams, setTeams] = useState<Team[]>(cache ?? [])

  useEffect(() => {
    let alive = true
    if (cache === null) {
      load().then((list) => {
        if (alive) setTeams(list)
      }).catch(() => {
        // Silent — team pills fall back to plain text if the endpoint
        // isn't reachable (no auth, older build, etc).
      })
    }
    const sub = (list: Team[]) => {
      if (alive) setTeams(list)
    }
    subscribers.add(sub)
    return () => {
      alive = false
      subscribers.delete(sub)
    }
  }, [])

  return teams
}

/** Case-insensitive lookup. Falls back to reserved pseudo-teams. */
export function findTeam(teams: Team[], slug: string): Team | undefined {
  const needle = slug.trim().toLowerCase()
  const stored = teams.find((t) => t.slug.toLowerCase() === needle)
  if (stored) return stored
  const reserved = RESERVED_TEAMS[needle]
  if (reserved) {
    return {
      slug: needle,
      display_name: reserved.display_name,
      description: reserved.description,
      created_at: '',
    }
  }
  return undefined
}
