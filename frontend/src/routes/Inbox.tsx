import type { CSSProperties } from 'react'
import { Inbox as InboxIcon } from 'lucide-react'
import { Inbox } from '../components/builtin/Inbox'

// /inbox — full-page wrapper around the <Inbox /> built-in component.
// The component itself handles polling, item display, mark-read, archive,
// and delete. This route just gives it a Page-shaped frame in the shell.

const PAGE: CSSProperties = {
  padding: '2rem 1.5rem',
  maxWidth: '64rem',
  margin: '0 auto',
  width: '100%',
  color: 'var(--text)',
}

export default function InboxPage() {
  return (
    <div style={PAGE}>
      <header style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <InboxIcon size={18} style={{ color: 'var(--accent)' }} />
          <h1 style={{ fontSize: '1.25rem', fontWeight: 600, margin: 0 }}>Inbox</h1>
        </div>
        <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: '0.25rem 0 0' }}>
          Mentions, assignments, approval requests, and dead-lettered webhooks
          routed to you. Strictly per-user; nobody else can see this list.
        </p>
      </header>
      <Inbox />
    </div>
  )
}
