import { useCallback, useEffect, useState } from 'react'

export interface FileEntry {
  name: string
  size: number
  content_type: string
  modified_at: string
}

/**
 * Fetch the full manifest of uploaded files from `/api/files`. Auto-refreshes
 * when the DataContext broadcasts `agentboard:file-updated`, so agent writes
 * show up live in the sidebar.
 */
export function useFiles() {
  const [files, setFiles] = useState<FileEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    fetch('/api/files')
      .then(r => {
        if (!r.ok) throw new Error(`/api/files → ${r.status}`)
        return r.json() as Promise<FileEntry[]>
      })
      .then(data => {
        setFiles(Array.isArray(data) ? data : [])
        setError(null)
      })
      .catch(e => setError(e instanceof Error ? e.message : 'failed to load files'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    load()
    const onUpd = () => load()
    window.addEventListener('agentboard:file-updated', onUpd)
    return () => window.removeEventListener('agentboard:file-updated', onUpd)
  }, [load])

  return { files, loading, error }
}
