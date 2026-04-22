import { AlertTriangle } from 'lucide-react'
import type { ReactNode } from 'react'

/**
 * Fallback rendered in place of an MDX component the registry doesn't know
 * about. Keeps the rest of the page rendering — the composition stays
 * readable even if a brick is missing, mis-spelled, or not yet installed.
 *
 * Intentionally small and grey: a placeholder, not an error.
 */
export function MissingBrick({
  name,
  reason,
  children,
}: {
  name: string
  reason?: string
  children?: ReactNode
}) {
  return (
    <div
      role="note"
      className="my-4 p-3 rounded-md text-sm flex items-start gap-3"
      style={{
        background: 'var(--bg-secondary)',
        border: '1px dashed var(--border)',
        color: 'var(--text-secondary)',
      }}
    >
      <AlertTriangle size={16} style={{ marginTop: 2, flexShrink: 0 }} />
      <div className="min-w-0">
        <div style={{ color: 'var(--text)' }}>
          Brick <code style={{ fontFamily: 'ui-monospace, monospace' }}>&lt;{name}/&gt;</code> not available.
        </div>
        {reason && <div className="mt-1 text-xs">{reason}</div>}
        {!reason && (
          <div className="mt-1 text-xs">
            Install or fix the component, or remove this reference to render the rest of the page.
          </div>
        )}
        {children && (
          <details className="mt-2">
            <summary className="cursor-pointer text-xs">Show original markup</summary>
            <pre
              className="mt-2 p-2 rounded text-xs overflow-auto"
              style={{ background: 'var(--bg)', border: '1px solid var(--border)' }}
            >
              {children as string}
            </pre>
          </details>
        )}
      </div>
    </div>
  )
}
