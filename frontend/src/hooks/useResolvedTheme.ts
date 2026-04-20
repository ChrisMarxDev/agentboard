import { useEffect, useState } from 'react'

export type ResolvedTheme = 'light' | 'dark'

function compute(): ResolvedTheme {
  if (typeof document === 'undefined') return 'light'
  const attr = document.documentElement.getAttribute('data-theme')
  if (attr === 'light' || attr === 'dark') return attr
  if (typeof window !== 'undefined' && window.matchMedia) {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return 'light'
}

/**
 * Returns the currently resolved theme ('light' | 'dark') and re-renders when it
 * changes. Follows the sidebar ThemeSwitch (which sets `data-theme` on <html>)
 * when explicit, otherwise falls back to the system `prefers-color-scheme`.
 */
export function useResolvedTheme(): ResolvedTheme {
  const [theme, setTheme] = useState<ResolvedTheme>(compute)

  useEffect(() => {
    const update = () => setTheme(compute())

    // 1. Watch the <html data-theme> attribute for manual overrides.
    const observer = new MutationObserver(update)
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme'],
    })

    // 2. Watch system theme changes.
    const mq = window.matchMedia?.('(prefers-color-scheme: dark)')
    mq?.addEventListener('change', update)

    return () => {
      observer.disconnect()
      mq?.removeEventListener('change', update)
    }
  }, [])

  return theme
}
