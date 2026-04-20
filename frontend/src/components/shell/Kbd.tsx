import type { ReactNode } from 'react'

interface KbdProps {
  children: ReactNode
  active?: boolean
}

export default function Kbd({ children, active = false }: KbdProps) {
  return (
    <kbd
      className="inline-flex items-center justify-center rounded-md border font-mono font-medium select-none"
      style={{
        minWidth: '1.375rem',
        height: '1.375rem',
        padding: '0 0.375rem',
        fontSize: '11px',
        lineHeight: 1,
        borderColor: active ? 'var(--accent)' : 'var(--border)',
        background: 'var(--bg)',
        color: active ? 'var(--accent)' : 'var(--text-secondary)',
        textTransform: 'uppercase',
      }}
    >
      {children}
    </kbd>
  )
}
