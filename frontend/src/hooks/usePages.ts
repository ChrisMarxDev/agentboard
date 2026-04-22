import { useEffect, useState } from 'react'
import { apiFetch } from '../lib/session'

export interface PageEntry {
  path: string
  title: string
  order: number
}

export function usePages(): PageEntry[] {
  const [pages, setPages] = useState<PageEntry[]>([])

  useEffect(() => {
    const load = () =>
      apiFetch('/api/content')
        .then(r => r.ok ? r.json() : [])
        .then((data: PageEntry[]) => {
          const sorted = [...data].sort((a, b) => a.order - b.order)
          setPages(sorted)
        })
        .catch(() => {})
    load()
    window.addEventListener('agentboard:page-updated', load)
    return () => window.removeEventListener('agentboard:page-updated', load)
  }, [])

  return pages
}
