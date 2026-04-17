import { useData } from '../../hooks/useData'

interface TableProps {
  source: string
  linkField?: string
}

export function Table({ source, linkField }: TableProps) {
  const { data, loading } = useData(source)

  if (loading) {
    return <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>Loading table...</div>
  }

  if (!data) return null

  let columns: string[] = []
  let rows: Record<string, unknown>[] = []

  if (Array.isArray(data)) {
    rows = data as Record<string, unknown>[]
    if (rows.length > 0) {
      columns = Object.keys(rows[0])
    }
  } else if (typeof data === 'object') {
    const obj = data as { columns?: string[]; rows?: Record<string, unknown>[] }
    columns = obj.columns ?? []
    rows = obj.rows ?? []
    if (columns.length === 0 && rows.length > 0) {
      columns = Object.keys(rows[0])
    }
  }

  if (rows.length === 0) {
    return <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>No data</div>
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr style={{ background: 'var(--bg-secondary)' }}>
            {columns.map(col => (
              <th
                key={col}
                className="text-left px-4 py-2 font-medium"
                style={{ color: 'var(--text-secondary)', borderBottom: '1px solid var(--border)' }}
              >
                {col}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
              {columns.map(col => {
                const val = row[col]
                const isLink = linkField && col === linkField && typeof val === 'string'
                return (
                  <td key={col} className="px-4 py-2" style={{ color: 'var(--text)' }}>
                    {isLink ? (
                      <a href={val as string} target="_blank" rel="noopener" style={{ color: 'var(--accent)' }}>
                        {String(val)}
                      </a>
                    ) : (
                      String(val ?? '')
                    )}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
