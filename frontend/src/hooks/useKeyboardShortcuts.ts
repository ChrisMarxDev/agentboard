import { useEffect, useRef } from 'react'

export type ShortcutMap = Record<string, (e: KeyboardEvent) => void>

export function isTypingInto(el: Element | null): boolean {
  if (!el) return false
  const tag = el.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  if ((el as HTMLElement).isContentEditable) return true
  const ce = el.getAttribute?.('contenteditable')
  if (ce === '' || ce === 'true' || ce === 'plaintext-only') return true
  return false
}

export function useKeyboardShortcuts(shortcuts: ShortcutMap, enabled: boolean = true) {
  const shortcutsRef = useRef(shortcuts)
  shortcutsRef.current = shortcuts

  useEffect(() => {
    if (!enabled) return

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return
      if (isTypingInto(document.activeElement)) return

      const handler = shortcutsRef.current[e.key]
      if (!handler) return

      e.preventDefault()
      e.stopPropagation()
      handler(e)
    }

    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [enabled])
}
