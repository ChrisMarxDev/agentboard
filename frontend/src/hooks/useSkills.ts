import { useCallback, useEffect, useState } from 'react'

export interface SkillSummary {
  slug: string
  name: string
  description: string
  path: string
  updated_at: string
}

/**
 * Fetch the list of skills hosted by this AgentBoard instance via
 * GET /api/skills. Auto-refetches when any file under `skills/` changes — the
 * backend broadcasts `file-updated` events, DataContext rebroadcasts them as
 * `agentboard:file-updated` with the changed path in `detail.name`.
 */
export function useSkills() {
  const [skills, setSkills] = useState<SkillSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    fetch('/api/skills')
      .then(r => {
        if (!r.ok) throw new Error(`GET /api/skills → ${r.status}`)
        return r.json() as Promise<SkillSummary[]>
      })
      .then(data => {
        setSkills(Array.isArray(data) ? data : [])
        setError(null)
      })
      .catch(e => setError(e instanceof Error ? e.message : 'Failed to load skills'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    load()
    const onFile = (e: Event) => {
      const detail = (e as CustomEvent<{ name?: string }>).detail
      if (typeof detail?.name === 'string' && detail.name.startsWith('skills/')) {
        load()
      }
    }
    window.addEventListener('agentboard:file-updated', onFile)
    return () => window.removeEventListener('agentboard:file-updated', onFile)
  }, [load])

  return { skills, loading, error, reload: load }
}
