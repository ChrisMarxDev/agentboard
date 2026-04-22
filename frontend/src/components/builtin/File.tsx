import { useEffect, useState } from 'react'
import type { CSSProperties } from 'react'
import { useData } from '../../hooks/useData'
import { resolveFileUrl, resolveFileLabel, type FileRef } from '../../lib/fileUrl'
import { apiFetch } from '../../lib/session'

interface FileProps {
  source?: string
  src?: string
  label?: string
}

interface ServerInfo {
  name: string
  size: number
  content_type: string
  etag?: string
  url: string
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

// Rough icon picker by extension. Emoji keeps us dependency-free; swap for
// proper icons if the project ever takes on an icon set.
function iconFor(name: string): string {
  const ext = (name.split('.').pop() ?? '').toLowerCase()
  if (['pdf'].includes(ext)) return '📑'
  if (['zip', 'tar', 'gz', 'tgz', '7z', 'rar'].includes(ext)) return '🗄'
  if (['csv', 'xlsx', 'xls', 'numbers'].includes(ext)) return '📊'
  if (['doc', 'docx', 'md', 'txt', 'rtf'].includes(ext)) return '📄'
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp'].includes(ext)) return '🖼'
  if (['mp3', 'wav', 'flac', 'ogg'].includes(ext)) return '🎵'
  if (['mp4', 'mov', 'mkv', 'webm'].includes(ext)) return '🎬'
  if (['json', 'yaml', 'yml', 'xml'].includes(ext)) return '⚙'
  return '📦'
}

export function File({ source, src, label }: FileProps) {
  const { data, loading } = useData(source ?? '')
  const resolved = src ? src : resolveFileUrl(data as unknown)
  const displayLabel = label ?? resolveFileLabel(src ? src : data, '')

  const [meta, setMeta] = useState<ServerInfo | null>(null)

  useEffect(() => {
    if (!resolved) return
    // Only fetch metadata for files we host — skip remote URLs.
    if (!resolved.startsWith('/api/files/')) return
    let cancelled = false
    const name = decodeURIComponent(resolved.replace(/^\/api\/files\//, ''))
    apiFetch('/api/files', { headers: { Accept: 'application/json' } })
      .then(r => r.ok ? r.json() : null)
      .then((list: ServerInfo[] | null) => {
        if (cancelled || !list) return
        const m = list.find(f => f.name === name)
        if (m) setMeta(m)
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [resolved])

  if (source && loading) return null
  if (!resolved) return null

  const containerStyle: CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: '0.75rem',
    padding: '0.75rem 1rem',
    borderRadius: '0.5rem',
    border: '1px solid var(--border)',
    background: 'var(--bg-secondary)',
    textDecoration: 'none',
    color: 'var(--text)',
    transition: 'background 0.1s ease',
  }

  // Human-readable filename for download attribute.
  const downloadName = displayLabel || (meta?.name ?? 'download')

  return (
    <a
      href={resolved}
      download={downloadName}
      className="my-2"
      style={containerStyle}
      onMouseEnter={e => { e.currentTarget.style.background = 'var(--bg)' }}
      onMouseLeave={e => { e.currentTarget.style.background = 'var(--bg-secondary)' }}
    >
      <span style={{ fontSize: '1.5rem', lineHeight: 1 }}>{iconFor(meta?.name ?? downloadName)}</span>
      <span style={{ display: 'flex', flexDirection: 'column', minWidth: 0, flex: 1 }}>
        <span style={{ fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {displayLabel || meta?.name || 'Download'}
        </span>
        <span style={{ fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
          {meta ? `${meta.content_type} · ${formatBytes(meta.size)}` : 'file'}
        </span>
      </span>
      <span style={{ fontSize: '0.875rem', color: 'var(--accent)', fontWeight: 500 }}>Download ↓</span>
    </a>
  )
}
