import { useEffect, useState } from 'react'

type Theme = 'auto' | 'light' | 'dark'

const STORAGE_KEY = 'agentboard:theme'

function applyTheme(theme: Theme) {
  const html = document.documentElement
  if (theme === 'auto') {
    html.removeAttribute('data-theme')
  } else {
    html.setAttribute('data-theme', theme)
  }
}

export function readStoredTheme(): Theme {
  if (typeof window === 'undefined') return 'auto'
  const v = window.localStorage.getItem(STORAGE_KEY)
  return v === 'light' || v === 'dark' || v === 'auto' ? v : 'auto'
}

export function ThemeSwitch() {
  const [theme, setTheme] = useState<Theme>(() => readStoredTheme())

  useEffect(() => {
    applyTheme(theme)
    window.localStorage.setItem(STORAGE_KEY, theme)
  }, [theme])

  const options: { value: Theme; label: string; glyph: string }[] = [
    { value: 'light', label: 'Light', glyph: '☀' },
    { value: 'auto', label: 'Auto',  glyph: '◐' },
    { value: 'dark',  label: 'Dark',  glyph: '☾' },
  ]

  return (
    <div
      className="flex items-center rounded-md overflow-hidden"
      style={{ border: '1px solid var(--border)', background: 'var(--bg)' }}
      role="group"
      aria-label="Theme"
    >
      {options.map(opt => {
        const active = theme === opt.value
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => setTheme(opt.value)}
            aria-pressed={active}
            title={opt.label}
            className="flex-1 flex items-center justify-center text-sm py-1.5 transition-colors"
            style={{
              background: active ? 'var(--accent-light)' : 'transparent',
              color: active ? 'var(--accent)' : 'var(--text-secondary)',
              border: 'none',
              cursor: 'pointer',
            }}
          >
            {opt.glyph}
          </button>
        )
      })}
    </div>
  )
}
