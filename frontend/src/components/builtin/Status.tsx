import { useData } from '../../hooks/useData'

interface StatusProps {
  state?: string
  label?: string
  detail?: string
  source?: string
}

const stateStyles: Record<string, { bg: string; dot: string; text: string }> = {
  running:  { bg: 'var(--accent-light)', dot: 'var(--accent)',  text: 'var(--accent)' },
  passing:  { bg: '#d1fae5',             dot: 'var(--success)', text: 'var(--success)' },
  failing:  { bg: '#fee2e2',             dot: 'var(--error)',   text: 'var(--error)' },
  waiting:  { bg: '#fef3c7',             dot: 'var(--warning)', text: 'var(--warning)' },
  stale:    { bg: 'var(--bg-secondary)', dot: 'var(--text-secondary)', text: 'var(--text-secondary)' },
}

export function Status(props: StatusProps) {
  const { data, loading } = useData(props.source ?? '')

  let state = props.state
  let label = props.label
  let detail = props.detail

  if (state === undefined && props.source) {
    if (loading) {
      return (
        <div className="inline-flex items-center gap-2 px-3 py-2 rounded-full text-sm"
          style={{ background: 'var(--bg-secondary)', color: 'var(--text-secondary)' }}>
          Loading...
        </div>
      )
    }
    if (!data || typeof data !== 'object') return null
    const obj = data as { state?: string; label?: string; detail?: string }
    state = obj.state
    label = label ?? obj.label
    detail = detail ?? obj.detail
  }

  if (!state && !label) return null

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
