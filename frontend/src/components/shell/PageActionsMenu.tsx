import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { beaconError } from '../../lib/errorBeacon'
import { copyPageSource } from '../../lib/copyPage'
import { copyToClipboard } from '../../lib/clipboard'
import { addPick, setMode } from '../../lib/grab'
import { apiFetch } from '../../lib/session'
import Kbd from './Kbd'

interface PageActionsMenuProps {
  pagePath: string
  pageTitle?: string
}

type Tone = 'default' | 'danger'

interface ActionItem {
  id: string
  label: string
  tone: Tone
  run: () => void
  shortcut?: string
}

export default function PageActionsMenu({ pagePath, pageTitle }: PageActionsMenuProps) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [shareModal, setShareModal] = useState<{ url: string; expiresAt?: string } | null>(null)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open && !confirming) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (confirming) setConfirming(false)
        else setOpen(false)
      }
    }
    const onClick = (e: MouseEvent) => {
      if (!rootRef.current) return
      if (rootRef.current.contains(e.target as Node)) return
      setOpen(false)
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('mousedown', onClick)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('mousedown', onClick)
    }
  }, [open, confirming])

  async function deletePage() {
    setBusy(true)
    setErr(null)
    try {
      const res = await apiFetch(`/api/${encodeURI(pagePath)}`, { method: 'DELETE' })
      if (!res.ok) {
        const body = await res.text().catch(() => '')
        throw new Error(body || `DELETE ${pagePath} → ${res.status}`)
      }
      setConfirming(false)
      setOpen(false)
      navigate('/')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'delete failed'
      setErr(msg)
      beaconError({ component: 'PageActionsMenu', source: pagePath, error: msg })
    } finally {
      setBusy(false)
    }
  }

  async function shareePage() {
    setOpen(false)
    try {
      const pageUrl = pagePath === 'index' ? '/' : '/' + pagePath
      const res = await apiFetch('/api/share', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: pageUrl }),
      })
      if (!res.ok) throw new Error(`share ${pagePath} → ${res.status}`)
      const body = (await res.json()) as { url: string; expires_at?: string }
      setShareModal({ url: body.url, expiresAt: body.expires_at })
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'share failed'
      beaconError({ component: 'PageActionsMenu', source: pagePath, error: msg })
    }
  }

  async function exportPage() {
    setOpen(false)
    try {
      const res = await apiFetch(`/api/${encodeURI(pagePath)}`, {
        headers: { Accept: 'text/markdown' },
      })
      if (!res.ok) throw new Error(`GET ${pagePath} → ${res.status}`)
      const mdx = await res.text()

      // Filename = last path segment + .md; `index` stays `index.md`.
      const filename = (pagePath.split('/').pop() || 'page') + '.md'

      const blob = new Blob([mdx], { type: 'text/markdown' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'export failed'
      beaconError({ component: 'PageActionsMenu', source: pagePath, error: msg })
    }
  }

  const actions: ActionItem[] = []
  if (pagePath) {
    actions.push({
      id: 'copy',
      label: 'Copy source',
      tone: 'default',
      shortcut: 'C',
      run: () => {
        setOpen(false)
        void copyPageSource(pagePath)
      },
    })
    actions.push({
      id: 'export',
      label: 'Export page',
      tone: 'default',
      run: exportPage,
    })
    actions.push({
      id: 'share',
      label: 'Share publicly…',
      tone: 'default',
      run: shareePage,
    })
    actions.push({
      id: 'grab-page',
      label: 'Grab whole page',
      tone: 'default',
      run: () => {
        setOpen(false)
        const pageUrl = pagePath === 'index' ? '/' : '/' + pagePath
        addPick({ kind: 'page', page: pageUrl })
        setMode(true)
      },
    })
  }
  if (pagePath && pagePath !== 'index') {
    actions.push({
      id: 'delete',
      label: 'Delete page',
      tone: 'danger',
      run: () => {
        setOpen(false)
        setConfirming(true)
      },
    })
  }

  if (actions.length === 0) return null

  return (
    <div ref={rootRef} className="absolute top-2 right-2 z-20">
      <button
        aria-label="Page actions"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(v => !v)}
        className="flex items-center justify-center rounded-md px-2 py-1 text-base leading-none"
        style={{
          background: open ? 'var(--bg-secondary)' : 'transparent',
          border: '1px solid var(--border)',
          color: 'var(--text-secondary)',
          cursor: 'pointer',
        }}
      >
        ⋯
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 mt-1 rounded-md shadow-sm min-w-[160px] py-1"
          style={{
            background: 'var(--bg)',
            border: '1px solid var(--border)',
          }}
        >
          {actions.map(a => (
            <button
              key={a.id}
              role="menuitem"
              onClick={a.run}
              className="w-full text-left px-3 py-1.5 text-sm flex items-center justify-between gap-3"
              style={{
                background: 'transparent',
                border: 'none',
                color: a.tone === 'danger' ? 'var(--error)' : 'var(--text)',
                cursor: 'pointer',
              }}
              onMouseEnter={e => {
                ;(e.currentTarget as HTMLButtonElement).style.background = 'var(--bg-secondary)'
              }}
              onMouseLeave={e => {
                ;(e.currentTarget as HTMLButtonElement).style.background = 'transparent'
              }}
            >
              <span>{a.label}</span>
              {a.shortcut && <Kbd>{a.shortcut}</Kbd>}
            </button>
          ))}
        </div>
      )}

      {confirming && (
        <ConfirmDeleteDialog
          pagePath={pagePath}
          pageTitle={pageTitle}
          busy={busy}
          error={err}
          onCancel={() => {
            setConfirming(false)
            setErr(null)
          }}
          onConfirm={deletePage}
        />
      )}

      {shareModal && (
        <ShareLinkDialog
          url={shareModal.url}
          expiresAt={shareModal.expiresAt}
          onClose={() => setShareModal(null)}
        />
      )}
    </div>
  )
}

function ShareLinkDialog({
  url,
  expiresAt,
  onClose,
}: {
  url: string
  expiresAt?: string
  onClose: () => void
}) {
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  const copy = async () => {
    await copyToClipboard(url)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  const expiresText = expiresAt
    ? new Date(expiresAt).toLocaleString()
    : 'never'
  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-[100] flex items-center justify-center p-4"
      style={{ background: 'rgba(0, 0, 0, 0.4)' }}
      role="dialog"
      aria-modal="true"
      aria-label="Share page link"
    >
      <div
        onClick={e => e.stopPropagation()}
        className="rounded-lg border w-full max-w-md"
        style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border)' }}
      >
        <div
          className="px-5 py-3 border-b"
          style={{ borderColor: 'var(--border)' }}
        >
          <div className="font-semibold text-sm" style={{ color: 'var(--text)' }}>
            Public share link
          </div>
        </div>
        <div className="px-5 py-4 text-sm" style={{ color: 'var(--text)' }}>
          <p style={{ color: 'var(--text-secondary)' }} className="mb-2">
            Anyone with this link can view the page without signing in. Writes still require auth. Expires:{' '}
            <strong style={{ color: 'var(--text)' }}>{expiresText}</strong>
          </p>
          <div
            className="flex items-stretch gap-2 mt-3 p-2 rounded-md font-mono text-xs"
            style={{ background: 'var(--bg)', border: '1px solid var(--border)' }}
          >
            <input
              type="text"
              readOnly
              value={url}
              onClick={e => (e.currentTarget as HTMLInputElement).select()}
              style={{
                flex: 1,
                background: 'transparent',
                border: 'none',
                color: 'var(--text)',
                outline: 'none',
                minWidth: 0,
              }}
            />
            <button
              onClick={() => void copy()}
              className="px-2 py-1 rounded-md text-xs"
              style={{
                background: 'var(--accent)',
                color: 'white',
                border: 'none',
                cursor: 'pointer',
              }}
            >
              {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>
        </div>
        <div
          className="px-5 py-3 border-t flex items-center justify-end"
          style={{ borderColor: 'var(--border)' }}
        >
          <button
            onClick={onClose}
            className="text-sm px-3 py-1.5 rounded-md"
            style={{
              background: 'transparent',
              border: '1px solid var(--border)',
              color: 'var(--text)',
              cursor: 'pointer',
            }}
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

interface ConfirmDeleteDialogProps {
  pagePath: string
  pageTitle?: string
  busy: boolean
  error: string | null
  onCancel: () => void
  onConfirm: () => void
}

function ConfirmDeleteDialog({
  pagePath,
  pageTitle,
  busy,
  error,
  onCancel,
  onConfirm,
}: ConfirmDeleteDialogProps) {
  return (
    <div
      onClick={onCancel}
      className="fixed inset-0 z-[100] flex items-center justify-center p-4"
      style={{ background: 'rgba(0, 0, 0, 0.4)' }}
      role="dialog"
      aria-modal="true"
      aria-label="Confirm delete page"
    >
      <div
        onClick={e => e.stopPropagation()}
        className="rounded-lg border w-full max-w-md"
        style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border)' }}
      >
        <div
          className="px-5 py-3 border-b"
          style={{ borderColor: 'var(--border)' }}
        >
          <div className="font-semibold text-sm" style={{ color: 'var(--text)' }}>
            Delete page?
          </div>
        </div>
        <div className="px-5 py-4 text-sm" style={{ color: 'var(--text)' }}>
          <p>
            <span style={{ color: 'var(--text-secondary)' }}>Path:</span>{' '}
            <code
              className="px-1 py-0.5 rounded"
              style={{ background: 'var(--bg)', border: '1px solid var(--border)' }}
            >
              /{pagePath}
            </code>
          </p>
          {pageTitle && (
            <p className="mt-2">
              <span style={{ color: 'var(--text-secondary)' }}>Title:</span>{' '}
              {pageTitle}
            </p>
          )}
          <p className="mt-3" style={{ color: 'var(--text-secondary)' }}>
            The source file is removed from disk. Any data keys it referenced stay in
            the store — prune them separately if you no longer need them.
          </p>
          {error && (
            <p
              className="mt-3 px-2 py-1 rounded text-xs"
              style={{ background: 'rgba(239,68,68,0.12)', color: 'var(--error)' }}
            >
              {error}
            </p>
          )}
        </div>
        <div
          className="px-5 py-3 border-t flex items-center justify-end gap-2"
          style={{ borderColor: 'var(--border)' }}
        >
          <button
            onClick={onCancel}
            disabled={busy}
            className="text-sm px-3 py-1.5 rounded-md"
            style={{
              background: 'transparent',
              border: '1px solid var(--border)',
              color: 'var(--text)',
              cursor: busy ? 'not-allowed' : 'pointer',
              opacity: busy ? 0.5 : 1,
            }}
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={busy}
            className="text-sm px-3 py-1.5 rounded-md font-medium"
            style={{
              background: 'var(--error)',
              border: '1px solid var(--error)',
              color: 'white',
              cursor: busy ? 'not-allowed' : 'pointer',
              opacity: busy ? 0.7 : 1,
            }}
          >
            {busy ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </div>
    </div>
  )
}
