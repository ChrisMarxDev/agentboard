import { useEffect, useState } from 'react'
import { COPY_EVENT, type CopyEventDetail } from '../../lib/copyPage'

interface ToastState {
  ok: boolean
  message: string
}

const VISIBLE_MS = 1600

export default function CopyToast() {
  const [toast, setToast] = useState<ToastState | null>(null)

  useEffect(() => {
    function onCopy(e: Event) {
      const detail = (e as CustomEvent<CopyEventDetail>).detail
      setToast({
        ok: detail.ok,
        message: detail.ok ? 'Copied page source to clipboard' : detail.error || 'Copy failed',
      })
    }
    window.addEventListener(COPY_EVENT, onCopy)
    return () => window.removeEventListener(COPY_EVENT, onCopy)
  }, [])

  useEffect(() => {
    if (!toast) return
    const t = window.setTimeout(() => setToast(null), VISIBLE_MS)
    return () => window.clearTimeout(t)
  }, [toast])

  if (!toast) return null

  return (
    <div
      role="status"
      aria-live="polite"
      className="fixed bottom-6 left-1/2 -translate-x-1/2 z-[110] px-4 py-2 rounded-md text-sm shadow-md"
      style={{
        background: 'var(--bg-secondary)',
        border: '1px solid var(--border)',
        color: toast.ok ? 'var(--text)' : 'var(--error)',
      }}
    >
      {toast.message}
    </div>
  )
}
