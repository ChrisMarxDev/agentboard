import { Fragment, type ReactNode } from 'react'
import { Mention } from './Mention'
import { useData } from '../../hooks/useData'

// <RichText text="..." /> or <RichText source="foo.bar" /> renders a plain
// string with inline @mentions parsed into <Mention/> badges. Used for
// free-text values coming from the data store (log entries, task
// descriptions, comments, etc.) where agents write strings and the
// dashboard wants them rich.
//
// Keeping the wire format "just a string" is deliberate — producers
// (agents, curl, MDX authors) don't need to know about our mention shape,
// and the same text renders fine anywhere (markdown, CLI, Slack excerpt).
// The UI is where we lift it.
//
// Grammar recognised:
//   `@username`   — where username matches ^[a-z][a-z0-9_-]{0,31}$,
//                   preceded by start/whitespace/punctuation,
//                   followed by end/whitespace/punctuation.
// Everything else renders as literal text.

// Must mirror usernameRE in internal/auth/username.go. The surrounding
// (?:^|[...boundary]) group prevents mid-word matches in e.g. "email@host".
const MENTION_RE = /(^|[\s(])@([a-z][a-z0-9_-]{0,31})(?=$|[\s.,!?;:)])/g

interface RichTextProps {
  /** Inline string (from MDX prose or another component's data). */
  text?: string | null
  /** Data store key to subscribe to; overrides `text` when both are given. */
  source?: string
  /** Render this placeholder when the resolved text is empty or missing. */
  emptyLabel?: string
}

export function RichText({ text, source, emptyLabel }: RichTextProps) {
  // Always call the hook (rules-of-hooks). If `source` is empty the hook
  // returns loading=false, data=undefined immediately and we fall back to
  // the `text` prop.
  const { data } = useData(source ?? '')
  const resolved = source
    ? typeof data === 'string'
      ? data
      : data == null
        ? null
        : JSON.stringify(data)
    : text

  if (!resolved) {
    return emptyLabel ? (
      <span style={{ color: 'var(--text-secondary)' }}>{emptyLabel}</span>
    ) : null
  }

  const parts: ReactNode[] = []
  let lastIndex = 0
  let i = 0

  for (const match of resolved.matchAll(MENTION_RE)) {
    const [full, prefix, username] = match
    const start = match.index ?? 0
    // Prefix (whitespace or opening bracket) is part of the match — keep it
    // in the literal stream so spacing around the pill stays correct.
    if (start > lastIndex) {
      parts.push(<Fragment key={`t-${i}`}>{resolved.slice(lastIndex, start)}</Fragment>)
      i++
    }
    if (prefix) {
      parts.push(<Fragment key={`p-${i}`}>{prefix}</Fragment>)
      i++
    }
    parts.push(<Mention key={`m-${i}`} username={username} />)
    i++
    lastIndex = start + full.length
  }

  if (lastIndex < resolved.length) {
    parts.push(<Fragment key={`t-${i}`}>{resolved.slice(lastIndex)}</Fragment>)
  }

  return <>{parts}</>
}
