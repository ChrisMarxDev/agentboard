import type { CSSProperties } from 'react'
import { Users } from 'lucide-react'
import { findUser, useUsers } from '../../hooks/useUsers'
import { findTeam, useTeams } from '../../hooks/useTeams'

// <Mention username="alice" /> renders a compact @username pill backed by
// the user's avatar color. Used directly by MDX authors and indirectly
// by <RichText/> (which scans a string for @username patterns).
//
// Resolution rules (user wins over team wins over reserved):
//   - active user → colored pill
//   - deactivated user → dimmed pill (preserves historical attribution)
//   - stored team → group-glyph pill with the team's display name
//   - reserved pseudo-team (@all, @admins, @agents, @here) → team pill
//   - unknown username → plain `@username` text, no pill
//
// The component reads from the module-level user + team caches so a
// page with 50 mentions costs two fetches at mount, not 100.

interface MentionProps {
  username: string
  /** Override the visual label; defaults to "@username". */
  display?: string
  /** Drop the color pill and render plain text with the leading @. */
  plain?: boolean
}

export function Mention({ username, display, plain }: MentionProps) {
  const users = useUsers()
  const teams = useTeams()
  const u = findUser(users, username)
  const team = !u ? findTeam(teams, username) : undefined

  if (plain || (!u && !team)) {
    // Unknown or explicitly plain — render as literal text so the original
    // intent survives (and the username eventually resolves if the user
    // gets created).
    return <span style={{ color: 'var(--text-secondary)' }}>@{username}</span>
  }

  if (team) {
    const label = display ?? `@${team.slug}`
    const memberCount = team.members?.length ?? 0
    const title = team.display_name
      ? `${team.display_name} (@${team.slug}${memberCount ? ` — ${memberCount} member${memberCount === 1 ? '' : 's'}` : ''})`
      : `@${team.slug}${memberCount ? ` — ${memberCount} member${memberCount === 1 ? '' : 's'}` : ''}`
    const teamStyle: CSSProperties = {
      display: 'inline-flex',
      alignItems: 'center',
      gap: '0.25rem',
      padding: '0.05rem 0.4rem 0.05rem 0.3rem',
      borderRadius: '9999px',
      fontSize: '0.8125rem',
      lineHeight: 1.5,
      fontWeight: 500,
      color: 'var(--text)',
      background: 'color-mix(in srgb, var(--accent) 10%, transparent)',
      border: '1px dashed color-mix(in srgb, var(--accent) 30%, transparent)',
      verticalAlign: 'baseline',
    }
    return (
      <span style={teamStyle} title={title}>
        <Users size={11} strokeWidth={2} aria-hidden />
        {label}
      </span>
    )
  }

  if (!u) {
    // Unreachable: plain/unknown handled above, team handled above.
    return <span style={{ color: 'var(--text-secondary)' }}>@{username}</span>
  }
  const dimmed = Boolean(u.deactivated)
  const style: CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '0.25rem',
    padding: '0.05rem 0.4rem 0.05rem 0.3rem',
    borderRadius: '9999px',
    fontSize: '0.8125rem',
    lineHeight: 1.5,
    fontWeight: 500,
    color: dimmed ? 'var(--text-secondary)' : 'var(--text)',
    background: dimmed ? 'var(--bg-secondary)' : 'color-mix(in srgb, ' + (u.avatar_color ?? 'var(--accent)') + ' 14%, transparent)',
    border: '1px solid ' + (dimmed ? 'var(--border)' : 'color-mix(in srgb, ' + (u.avatar_color ?? 'var(--accent)') + ' 28%, transparent)'),
    textDecoration: dimmed ? 'line-through' : 'none',
    verticalAlign: 'baseline',
  }

  const dot: CSSProperties = {
    width: '0.5rem',
    height: '0.5rem',
    borderRadius: '9999px',
    background: u.avatar_color ?? 'var(--accent)',
    flexShrink: 0,
  }

  const label = display ?? `@${u.username}`
  const title = u.display_name
    ? `${u.display_name} (@${u.username}${dimmed ? ' — deactivated' : ''})`
    : `@${u.username}${dimmed ? ' — deactivated' : ''}`

  return (
    <span style={style} title={title}>
      <span style={dot} aria-hidden />
      {label}
    </span>
  )
}
