import { useEffect, useState } from 'react'

export interface PageEntry {
  path: string
  title: string
  order: number
}

export function usePages(): PageEntry[] {
  const [pages, setPages] = useState<PageEntry[]>([])

  useEffect(() => {
    const load = () =>
      fetch('/api/content')
        .then(r => r.json())
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
