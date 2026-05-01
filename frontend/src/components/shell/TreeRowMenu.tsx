import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { MoreHorizontal } from 'lucide-react'
import type { ContentTreeNode } from '../../lib/contentTree'
import { apiFetch } from '../../lib/session'
import { copyToClipboard } from '../../lib/clipboard'
import { beaconError } from '../../lib/errorBeacon'

interface TreeRowMenuProps {
  node: ContentTreeNode
  // When true, the trigger button is always opaque (active row).
  // Otherwise it lives at opacity 0 and reveals on row hover via the
  // parent `group` class.
  alwaysShow: boolean
}

type Tone = 'default' | 'danger'

interface ActionItem {
  id: string
  label: string
  tone: Tone
  run: () => void
}

// Pages talk to the unified /api/<path> route by their slug — no leading
// slash, no .md. The home page is the special slug "index". Files live
// under /api/files/<full-path-with-extension>.
function pageSlug(node: { href: string }): string {
  if (node.href === '/' || node.href === '') return 'index'
  return node.href.replace(/^\/+/, '')
}

export default function TreeRowMenu({ node, alwaysShow }: TreeRowMenuProps) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
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
  }, [open])

  const actions = buildActions({
    node,
    navigate,
    closeMenu: () => setOpen(false),
    openDialog: setDialog,
  })
  if (actions.length === 0) return null

  return (
    <div ref={rootRef} className="relative shrink-0" onClick={e => e.stopPropagation()}>
      <button
        type="button"
        aria-label="Row actions"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={e => {
          e.preventDefault()
          e.stopPropagation()
          setOpen(v => !v)
        }}
        className={`flex items-center justify-center rounded-md transition-opacity ${
          alwaysShow || open ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'
        }`}
        style={{
          width: 22,
          height: 22,
          background: open ? 'var(--bg)' : 'transparent',
          border: '1px solid',
          borderColor: open ? 'var(--border)' : 'transparent',
          color: 'var(--text-secondary)',
          cursor: 'pointer',
        }}
      >
        <MoreHorizontal size={14} />
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 mt-1 rounded-md shadow-sm min-w-[180px] py-1"
          style={{
            background: 'var(--bg)',
            border: '1px solid var(--border)',
            zIndex: 50,
          }}
        >
          {actions.map(a => (
            <button
              key={a.id}
              role="menuitem"
              onClick={e => {
                e.preventDefault()
                e.stopPropagation()
                a.run()
              }}
              className="w-full text-left px-3 py-1.5 text-sm"
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
              {a.label}
            </button>
          ))}
        </div>
      )}

      {dialog && <DialogHost dialog={dialog} onClose={() => setDialog(null)} />}
    </div>
  )
}

// ---------------- Action wiring ----------------

type DialogState =
  | { kind: 'rename'; node: ContentTreeNode & { kind: 'page' } }
  | { kind: 'newPage'; folder: ContentTreeNode & { kind: 'folder' } }
  | { kind: 'confirmDelete'; node: ContentTreeNode }

interface BuildArgs {
  node: ContentTreeNode
  navigate: ReturnType<typeof useNavigate>
  closeMenu: () => void
  openDialog: (d: DialogState) => void
}

function buildActions({ node, navigate, closeMenu, openDialog }: BuildArgs): ActionItem[] {
  if (node.kind === 'page') {
    const slug = pageSlug(node)
    const isIndex = slug === 'index'
    const items: ActionItem[] = [
      {
        id: 'download',
        label: 'Download .md',
        tone: 'default',
        run: () => {
          closeMenu()
          void downloadPage(slug)
        },
      },
      {
        id: 'open-new',
        label: 'Open in new tab',
        tone: 'default',
        run: () => {
          closeMenu()
          window.open(node.href, '_blank', 'noopener,noreferrer')
        },
      },
      {
        id: 'copy-link',
        label: 'Copy link',
        tone: 'default',
        run: () => {
          closeMenu()
          void copyToClipboard(window.location.origin + node.href)
        },
      },
      {
        id: 'copy-path',
        label: 'Copy path',
        tone: 'default',
        run: () => {
          closeMenu()
          void copyToClipboard(slug)
        },
      },
    ]
    if (!isIndex) {
      items.push({
        id: 'duplicate',
        label: 'Duplicate',
        tone: 'default',
        run: () => {
          closeMenu()
          void duplicatePage(slug, navigate)
        },
      })
      items.push({
        id: 'rename',
        label: 'Rename…',
        tone: 'default',
        run: () => {
          closeMenu()
          openDialog({ kind: 'rename', node })
        },
      })
      items.push({
        id: 'delete',
        label: 'Delete',
        tone: 'danger',
        run: () => {
          closeMenu()
          openDialog({ kind: 'confirmDelete', node })
        },
      })
    }
    return items
  }

  if (node.kind === 'file') {
    return [
      {
        id: 'download',
        label: 'Download',
        tone: 'default',
        run: () => {
          closeMenu()
          void downloadFile(node.path, node.name)
        },
      },
      {
        id: 'open-new',
        label: 'Open in new tab',
        tone: 'default',
        run: () => {
          closeMenu()
          window.open(node.href, '_blank', 'noopener,noreferrer')
        },
      },
      {
        id: 'copy-link',
        label: 'Copy link',
        tone: 'default',
        run: () => {
          closeMenu()
          void copyToClipboard(window.location.origin + node.href)
        },
      },
      {
        id: 'copy-path',
        label: 'Copy path',
        tone: 'default',
        run: () => {
          closeMenu()
          void copyToClipboard(node.path)
        },
      },
      {
        id: 'delete',
        label: 'Delete',
        tone: 'danger',
        run: () => {
          closeMenu()
          openDialog({ kind: 'confirmDelete', node })
        },
      },
    ]
  }

  // folder
  return [
    {
      id: 'new-page',
      label: 'New page here…',
      tone: 'default',
      run: () => {
        closeMenu()
        openDialog({ kind: 'newPage', folder: node })
      },
    },
    {
      id: 'copy-path',
      label: 'Copy path',
      tone: 'default',
      run: () => {
        closeMenu()
        void copyToClipboard(node.path)
      },
    },
  ]
}

// ---------------- Action runners ----------------

async function downloadPage(slug: string) {
  try {
    const res = await apiFetch(`/api/${encodeURI(slug)}`, {
      headers: { Accept: 'text/markdown' },
    })
    if (!res.ok) throw new Error(`GET ${slug} → ${res.status}`)
    const mdx = await res.text()
    const filename = (slug.split('/').pop() || 'page') + '.md'
    triggerDownload(new Blob([mdx], { type: 'text/markdown' }), filename)
  } catch (e) {
    beaconError({
      component: 'TreeRowMenu',
      source: slug,
      error: e instanceof Error ? e.message : 'download failed',
    })
  }
}

async function downloadFile(path: string, name: string) {
  try {
    const res = await apiFetch(`/api/files/${encodeURI(path)}`)
    if (!res.ok) throw new Error(`GET files/${path} → ${res.status}`)
    const blob = await res.blob()
    triggerDownload(blob, name || path.split('/').pop() || 'file')
  } catch (e) {
    beaconError({
      component: 'TreeRowMenu',
      source: path,
      error: e instanceof Error ? e.message : 'download failed',
    })
  }
}

function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

async function duplicatePage(srcSlug: string, navigate: (to: string) => void) {
  try {
    // Read source
    const src = await apiFetch(`/api/${encodeURI(srcSlug)}`, {
      headers: { Accept: 'text/markdown' },
    })
    if (!src.ok) throw new Error(`GET ${srcSlug} → ${src.status}`)
    const body = await src.text()

    // Find a free destination. Slug ends in -copy, -copy-2, …
    const destSlug = await findFreeCopySlug(srcSlug)

    const put = await apiFetch(`/api/${encodeURI(destSlug)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'text/plain' },
      body,
    })
    if (!put.ok) {
      const text = await put.text().catch(() => '')
      throw new Error(text || `PUT ${destSlug} → ${put.status}`)
    }
    navigate('/' + destSlug)
  } catch (e) {
    beaconError({
      component: 'TreeRowMenu',
      source: srcSlug,
      error: e instanceof Error ? e.message : 'duplicate failed',
    })
  }
}

async function findFreeCopySlug(srcSlug: string): Promise<string> {
  const base = srcSlug + '-copy'
  for (let i = 0; i < 25; i++) {
    const candidate = i === 0 ? base : `${base}-${i + 1}`
    const probe = await apiFetch(`/api/${encodeURI(candidate)}`, { method: 'GET' })
    if (probe.status === 404) return candidate
    // Drain to keep connections clean.
    void probe.text().catch(() => '')
  }
  // Fall back to a timestamp suffix if 25 candidates were taken.
  return `${srcSlug}-copy-${Date.now()}`
}

// ---------------- Dialogs ----------------

function DialogHost({
  dialog,
  onClose,
}: {
  dialog: DialogState
  onClose: () => void
}) {
  if (dialog.kind === 'rename') return <RenameDialog node={dialog.node} onClose={onClose} />
  if (dialog.kind === 'newPage') return <NewPageDialog folder={dialog.folder} onClose={onClose} />
  return <ConfirmDeleteDialog node={dialog.node} onClose={onClose} />
}

function ModalShell({
  title,
  children,
  onClose,
}: {
  title: string
  children: ReactNode
  onClose: () => void
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return (
    <div
      onClick={onClose}
      className="fixed inset-0 z-[100] flex items-center justify-center p-4"
      style={{ background: 'rgba(0,0,0,0.4)' }}
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div
        onClick={e => e.stopPropagation()}
        className="rounded-lg border w-full max-w-md"
        style={{ background: 'var(--bg-secondary)', borderColor: 'var(--border)' }}
      >
        <div className="px-5 py-3 border-b" style={{ borderColor: 'var(--border)' }}>
          <div className="font-semibold text-sm" style={{ color: 'var(--text)' }}>{title}</div>
        </div>
        {children}
      </div>
    </div>
  )
}

function RenameDialog({
  node,
  onClose,
}: {
  node: ContentTreeNode & { kind: 'page' }
  onClose: () => void
}) {
  const navigate = useNavigate()
  const fromSlug = pageSlug(node)
  const [toSlug, setToSlug] = useState(fromSlug)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  useEffect(() => {
    inputRef.current?.focus()
    inputRef.current?.select()
  }, [])

  const submit = async () => {
    const to = toSlug.trim().replace(/^\/+/, '').replace(/\/+$/, '')
    if (!to || to === fromSlug) {
      onClose()
      return
    }
    setBusy(true)
    setErr(null)
    try {
      const res = await apiFetch('/api/content/move', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ from: fromSlug, to }),
      })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(text || `move → ${res.status}`)
      }
      onClose()
      navigate('/' + to)
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'rename failed'
      setErr(msg)
      beaconError({ component: 'TreeRowMenu.Rename', source: fromSlug, error: msg })
    } finally {
      setBusy(false)
    }
  }

  return (
    <ModalShell title="Rename page" onClose={onClose}>
      <div className="px-5 py-4 text-sm" style={{ color: 'var(--text)' }}>
        <p style={{ color: 'var(--text-secondary)' }} className="mb-2">
          Edit the page slug. Use slashes to move it into a different folder.
        </p>
        <input
          ref={inputRef}
          type="text"
          value={toSlug}
          onChange={e => setToSlug(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Enter') void submit()
          }}
          className="w-full px-2 py-1.5 rounded-md font-mono text-xs"
          style={{
            background: 'var(--bg)',
            border: '1px solid var(--border)',
            color: 'var(--text)',
            outline: 'none',
          }}
        />
        {err && (
          <p
            className="mt-3 px-2 py-1 rounded text-xs"
            style={{ background: 'rgba(239,68,68,0.12)', color: 'var(--error)' }}
          >
            {err}
          </p>
        )}
      </div>
      <DialogFooter
        onCancel={onClose}
        onConfirm={() => void submit()}
        confirmLabel={busy ? 'Renaming…' : 'Rename'}
        busy={busy}
        confirmTone="default"
      />
    </ModalShell>
  )
}

function NewPageDialog({
  folder,
  onClose,
}: {
  folder: ContentTreeNode & { kind: 'folder' }
  onClose: () => void
}) {
  const navigate = useNavigate()
  const [slug, setSlug] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const submit = async () => {
    const leaf = slug.trim().replace(/^\/+/, '').replace(/\/+$/, '')
    if (!leaf) return
    const target = folder.path ? `${folder.path}/${leaf}` : leaf
    const title = leaf
      .split('/')
      .pop()!
      .replace(/[-_]+/g, ' ')
      .replace(/\b\w/g, c => c.toUpperCase())
    const body = `# ${title}\n`
    setBusy(true)
    setErr(null)
    try {
      const res = await apiFetch(`/api/${encodeURI(target)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'text/plain' },
        body,
      })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(text || `PUT ${target} → ${res.status}`)
      }
      onClose()
      navigate('/' + target)
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'create failed'
      setErr(msg)
      beaconError({ component: 'TreeRowMenu.NewPage', source: target, error: msg })
    } finally {
      setBusy(false)
    }
  }

  return (
    <ModalShell title={`New page in /${folder.path}`} onClose={onClose}>
      <div className="px-5 py-4 text-sm" style={{ color: 'var(--text)' }}>
        <p style={{ color: 'var(--text-secondary)' }} className="mb-2">
          Slug for the new page. Lower-case with dashes is conventional.
        </p>
        <input
          ref={inputRef}
          type="text"
          value={slug}
          placeholder="my-new-page"
          onChange={e => setSlug(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Enter') void submit()
          }}
          className="w-full px-2 py-1.5 rounded-md font-mono text-xs"
          style={{
            background: 'var(--bg)',
            border: '1px solid var(--border)',
            color: 'var(--text)',
            outline: 'none',
          }}
        />
        {err && (
          <p
            className="mt-3 px-2 py-1 rounded text-xs"
            style={{ background: 'rgba(239,68,68,0.12)', color: 'var(--error)' }}
          >
            {err}
          </p>
        )}
      </div>
      <DialogFooter
        onCancel={onClose}
        onConfirm={() => void submit()}
        confirmLabel={busy ? 'Creating…' : 'Create'}
        busy={busy}
        confirmTone="default"
      />
    </ModalShell>
  )
}

function ConfirmDeleteDialog({
  node,
  onClose,
}: {
  node: ContentTreeNode
  onClose: () => void
}) {
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const isPage = node.kind === 'page'
  const url = isPage
    ? `/api/${encodeURI(pageSlug(node as ContentTreeNode & { kind: 'page' }))}`
    : `/api/files/${encodeURI((node as ContentTreeNode & { kind: 'file' }).path)}`
  const label = isPage ? 'page' : 'file'
  const path = isPage
    ? pageSlug(node as ContentTreeNode & { kind: 'page' })
    : (node as ContentTreeNode & { kind: 'file' }).path

  const submit = async () => {
    setBusy(true)
    setErr(null)
    try {
      const res = await apiFetch(url, { method: 'DELETE' })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(text || `DELETE → ${res.status}`)
      }
      onClose()
      // If the active route is the one being deleted, bounce home.
      if (window.location.pathname === node.href) navigate('/')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'delete failed'
      setErr(msg)
      beaconError({ component: 'TreeRowMenu.Delete', source: path, error: msg })
    } finally {
      setBusy(false)
    }
  }

  return (
    <ModalShell title={`Delete ${label}?`} onClose={onClose}>
      <div className="px-5 py-4 text-sm" style={{ color: 'var(--text)' }}>
        <p>
          <span style={{ color: 'var(--text-secondary)' }}>Path:</span>{' '}
          <code
            className="px-1 py-0.5 rounded"
            style={{ background: 'var(--bg)', border: '1px solid var(--border)' }}
          >
            /{path}
          </code>
        </p>
        <p className="mt-3" style={{ color: 'var(--text-secondary)' }}>
          The {label} is removed from disk. This can&apos;t be undone from the UI.
        </p>
        {err && (
          <p
            className="mt-3 px-2 py-1 rounded text-xs"
            style={{ background: 'rgba(239,68,68,0.12)', color: 'var(--error)' }}
          >
            {err}
          </p>
        )}
      </div>
      <DialogFooter
        onCancel={onClose}
        onConfirm={() => void submit()}
        confirmLabel={busy ? 'Deleting…' : 'Delete'}
        busy={busy}
        confirmTone="danger"
      />
    </ModalShell>
  )
}

function DialogFooter({
  onCancel,
  onConfirm,
  confirmLabel,
  confirmTone,
  busy,
}: {
  onCancel: () => void
  onConfirm: () => void
  confirmLabel: string
  confirmTone: Tone
  busy: boolean
}) {
  return (
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
          background: confirmTone === 'danger' ? 'var(--error)' : 'var(--accent)',
          border: '1px solid',
          borderColor: confirmTone === 'danger' ? 'var(--error)' : 'var(--accent)',
          color: 'white',
          cursor: busy ? 'not-allowed' : 'pointer',
          opacity: busy ? 0.7 : 1,
        }}
      >
        {confirmLabel}
      </button>
    </div>
  )
}
