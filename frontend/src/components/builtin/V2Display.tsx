// V2Display — read-only live view of one /api/v2/data/{key} envelope.
// Used on the spec showcase page so the demo isn't curl-only — humans
// and agents can watch the version timestamp tick forward as they
// poke at the API in another tab.
//
// Phase 4 footprint: tiny on purpose. This component does not replace
// any built-in. It exists so the showcase has a dogfooded "live mirror"
// without rewiring the legacy data path. When useData migrates to
// envelope-aware semantics (full Phase 4), this becomes redundant.

import { Loader2, AlertCircle } from 'lucide-react'
import { useDataV2 } from '../../hooks/useDataV2'

interface V2DisplayProps {
  /** Dotted v2 key, e.g. "showcase.demo". */
  source: string
  /** Optional label rendered above the envelope. */
  label?: string
}

export function V2Display({ source, label }: V2DisplayProps) {
  const { data, loading, error } = useDataV2(source)

  return (
    <div
      className="rounded-md border p-3 my-2 font-mono text-sm"
      style={{ borderColor: 'var(--border)', background: 'var(--bg-secondary)' }}
    >
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs font-sans" style={{ color: 'var(--text-secondary)' }}>
          {label ?? source}
        </span>
        {loading ? (
          <Loader2 size={12} className="animate-spin" style={{ color: 'var(--text-secondary)' }} />
        ) : error ? (
          <AlertCircle size={12} style={{ color: 'var(--error)' }} />
        ) : null}
      </div>

      {error ? (
        <pre className="text-xs whitespace-pre-wrap" style={{ color: 'var(--error)' }}>
          {error.message}
        </pre>
      ) : data === null ? (
        <span className="text-xs italic" style={{ color: 'var(--text-secondary)' }}>
          (no value at {source})
        </span>
      ) : (
        <pre className="text-xs whitespace-pre-wrap leading-snug" style={{ color: 'var(--text)' }}>
          {JSON.stringify(data, null, 2)}
        </pre>
      )}
    </div>
  )
}
