import { useData } from '../../hooks/useData'
import type { CSSProperties, ReactNode } from 'react'

type BadgeVariant = 'default' | 'accent' | 'success' | 'warning' | 'error'

interface BadgeProps {
  text?: string
  variant?: BadgeVariant
  children?: ReactNode
  source?: string
}

const variantStyles: Record<BadgeVariant, { bg: string; fg: string }> = {
  default: { bg: 'var(--bg-secondary)', fg: 'var(--text-secondary)' },
  accent:  { bg: 'var(--accent-light)', fg: 'var(--accent)' },
  success: { bg: 'rgba(16,185,129,0.15)', fg: 'var(--success)' },
  warning: { bg: 'rgba(245,158,11,0.15)', fg: 'var(--warning)' },
  error:   { bg: 'rgba(239,68,68,0.15)',  fg: 'var(--error)' },
}

export function Badge({ text, variant, children, source }: BadgeProps) {
  const { data, loading } = useData(source ?? '')

  let body: ReactNode
  let v: BadgeVariant = variant ?? 'default'

  if (text !== undefined) {
    body = text
  } else if (children !== undefined && children !== null && children !== '') {
    body = children
  } else if (source) {
    if (loading) return null
    if (data == null) return null
    if (typeof data === 'object' && !Array.isArray(data)) {
      const obj = data as Record<string, unknown>
      body = String(obj.text ?? obj.label ?? '')
      if (!variant && typeof obj.variant === 'string' && obj.variant in variantStyles) {
        v = obj.variant as BadgeVariant
      }
    } else {
      body = String(data)
    }
  } else {
    return null
  }

  if (body === '' || body === undefined) return null

  const styles = variantStyles[v]
  const style: CSSProperties = {
    display: 'inline-block',
    padding: '0.15rem 0.6rem',
    fontSize: '0.75rem',
    fontWeight: 600,
    borderRadius: '9999px',
    background: styles.bg,
    color: styles.fg,
    lineHeight: 1.4,
    verticalAlign: 'middle',
  }

  return <span style={style}>{body}</span>
}
