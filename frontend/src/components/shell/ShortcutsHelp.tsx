import Kbd from './Kbd'

interface ShortcutsHelpProps {
  open: boolean
  onClose: () => void
}

interface Row {
  keys: string[]
  label: string
}

const ROWS: Row[] = [
  { keys: ['B'], label: 'Toggle sidebar' },
  { keys: ['J'], label: 'Next page' },
  { keys: ['K'], label: 'Previous page' },
  { keys: ['1', '…', '9'], label: 'Jump to page' },
  { keys: ['?'], label: 'Open this help' },
  { keys: ['Esc'], label: 'Close this help' },
]

export default function ShortcutsHelp({ open, onClose }: ShortcutsHelpProps) {
  if (!open) return null

  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-[100] flex items-center justify-center p-4"
      style={{ background: 'rgba(0, 0, 0, 0.4)' }}
      role="dialog"
      aria-modal="true"
      aria-label="Keyboard shortcuts"
    >
      <div
        onClick={e => e.stopPropagation()}
        className="rounded-lg border w-full max-w-md"
        style={{
          background: 'var(--bg-secondary)',
          borderColor: 'var(--border)',
        }}
      >
        <div
          className="flex items-center justify-between px-5 py-3 border-b"
          style={{ borderColor: 'var(--border)' }}
        >
          <div className="font-semibold text-sm" style={{ color: 'var(--text)' }}>
            Keyboard shortcuts
          </div>
          <button
            onClick={onClose}
            aria-label="Close"
            className="text-lg leading-none px-2"
            style={{
              background: 'transparent',
              border: 'none',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
            }}
          >
            ×
          </button>
        </div>
        <div className="px-5 py-4 flex flex-col gap-2.5">
          {ROWS.map(row => (
            <div key={row.label} className="flex items-center justify-between gap-4">
              <div className="text-sm" style={{ color: 'var(--text)' }}>
                {row.label}
              </div>
              <div className="flex items-center gap-1">
                {row.keys.map((k, i) =>
                  k === '…' ? (
                    <span
                      key={i}
                      className="text-xs"
                      style={{ color: 'var(--text-secondary)' }}
                    >
                      …
                    </span>
                  ) : (
                    <Kbd key={i}>{k}</Kbd>
                  ),
                )}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
