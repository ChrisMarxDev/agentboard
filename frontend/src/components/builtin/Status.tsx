import { useData } from '../../hooks/useData'

interface StatusProps {
  source: string
}

const stateStyles: Record<string, { bg: string; dot: string; text: string }> = {
  running:  { bg: 'var(--accent-light)', dot: 'var(--accent)',  text: 'var(--accent)' },
  passing:  { bg: '#d1fae5',             dot: 'var(--success)', text: 'var(--success)' },
  failing:  { bg: '#fee2e2',             dot: 'var(--error)',   text: 'var(--error)' },
  waiting:  { bg: '#fef3c7',             dot: 'var(--warning)', text: 'var(--warning)' },
  stale:    { bg: 'var(--bg-secondary)', dot: 'var(--text-secondary)', text: 'var(--text-secondary)' },
}

export function Status({ source }: StatusProps) {
  const { data, loading } = useData(source)

  if (loading) {
    return (
      <div className="inline-flex items-center gap-2 px-3 py-2 rounded-full text-sm"
        style={{ background: 'var(--bg-secondary)', color: 'var(--text-secondary)' }}>
        Loading...
      </div>
    )
  }

  if (!data || typeof data !== 'object') return null
  const { state, label, detail } = data as { state?: string; label?: string; detail?: string }
  const styles = stateStyles[state ?? 'stale'] ?? stateStyles.stale

  return (
    <div className="inline-flex items-center gap-2 px-3 py-2 rounded-full text-sm"
      style={{ background: styles.bg }}>
      <span className="w-2 h-2 rounded-full inline-block" style={{ background: styles.dot }} />
      <span style={{ color: styles.text, fontWeight: 500 }}>{label ?? state}</span>
      {detail && <span style={{ color: 'var(--text-secondary)' }}>— {detail}</span>}
    </div>
  )
}
