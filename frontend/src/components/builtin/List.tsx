import { useData } from '../../hooks/useData'

interface ListProps {
  source: string
  variant?: 'ordered' | 'unordered'
}

const statusDots: Record<string, string> = {
  done: 'var(--success)',
  complete: 'var(--success)',
  completed: 'var(--success)',
  active: 'var(--accent)',
  in_progress: 'var(--accent)',
  pending: 'var(--warning)',
  todo: 'var(--text-secondary)',
  failed: 'var(--error)',
  error: 'var(--error)',
}

export function List({ source, variant = 'unordered' }: ListProps) {
  const { data, loading } = useData(source)

  if (loading) {
    return <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>Loading...</div>
  }

  if (!data || !Array.isArray(data)) return null

  const items = data as (string | Record<string, unknown>)[]

  const Tag = variant === 'ordered' ? 'ol' : 'ul'

  return (
    <Tag className="my-4 pl-0 list-none space-y-1">
      {items.map((item, i) => {
        if (typeof item === 'string') {
          return (
            <li key={i} className="flex items-center gap-2 px-3 py-1.5 rounded" style={{ color: 'var(--text)' }}>
              {variant === 'ordered' && <span style={{ color: 'var(--text-secondary)' }}>{i + 1}.</span>}
              <span>{item}</span>
            </li>
          )
        }

        const text = String(item.text ?? item.title ?? item.name ?? '')
        const status = String(item.status ?? '')
        const url = item.url as string | undefined
        const dotColor = statusDots[status] ?? 'var(--text-secondary)'

        return (
          <li key={i} className="flex items-center gap-2 px-3 py-1.5 rounded" style={{ color: 'var(--text)' }}>
            {status && (
              <span className="w-2 h-2 rounded-full shrink-0" style={{ background: dotColor }} />
            )}
            {variant === 'ordered' && !status && (
              <span style={{ color: 'var(--text-secondary)' }}>{i + 1}.</span>
            )}
            {url ? (
              <a href={url} target="_blank" rel="noopener" style={{ color: 'var(--accent)' }}>{text}</a>
            ) : (
              <span>{text}</span>
            )}
          </li>
        )
      })}
    </Tag>
  )
}
