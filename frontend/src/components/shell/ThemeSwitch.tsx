import { useEffect, useState } from 'react'
import { Sun, Moon } from 'lucide-react'

type StoredTheme = 'light' | 'dark' | null

const STORAGE_KEY = 'agentboard:theme'

function readStored(): StoredTheme {
  if (typeof window === 'undefined') return null
  const v = window.localStorage.getItem(STORAGE_KEY)
  return v === 'light' || v === 'dark' ? v : null
}

function systemTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined' || !window.matchMedia) return 'light'
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function applyTheme(stored: StoredTheme) {
  const html = document.documentElement
  if (stored === null) {
    html.removeAttribute('data-theme')
  } else {
    html.setAttribute('data-theme', stored)
  }
}

export function ThemeSwitch() {
  const [stored, setStored] = useState<StoredTheme>(() => readStored())
  const [sysTheme, setSysTheme] = useState<'light' | 'dark'>(() => systemTheme())

  useEffect(() => {
    applyTheme(stored)
    if (stored === null) {
      window.localStorage.removeItem(STORAGE_KEY)
    } else {
      window.localStorage.setItem(STORAGE_KEY, stored)
    }
  }, [stored])

  useEffect(() => {
    const mq = window.matchMedia?.('(prefers-color-scheme: dark)')
    if (!mq) return
    const update = () => setSysTheme(mq.matches ? 'dark' : 'light')
    mq.addEventListener('change', update)
    return () => mq.removeEventListener('change', update)
  }, [])

  const resolved = stored ?? sysTheme
  const next: 'light' | 'dark' = resolved === 'dark' ? 'light' : 'dark'
  const Icon = resolved === 'dark' ? Moon : Sun
  const label = `Switch to ${next} mode`

  return (
    <button
      type="button"
      onClick={() => setStored(next)}
      aria-label={label}
      title={label}
      className="h-8 w-full flex items-center justify-center rounded-md"
      style={{
        background: 'var(--bg)',
        border: '1px solid var(--border)',
        color: 'var(--text-secondary)',
        cursor: 'pointer',
      }}
    >
      <Icon size={16} />
    </button>
  )
}
