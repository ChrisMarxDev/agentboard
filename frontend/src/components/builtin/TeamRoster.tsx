import type { CSSProperties } from 'react'
import { Users } from 'lucide-react'
import { findTeam, useTeams } from '../../hooks/useTeams'
import { findUser, useUsers } from '../../hooks/useUsers'
import { Mention } from './Mention'

// <TeamRoster slug="marketing" /> renders a card-style roster for one
// team: the team pill as a header, description line, then a wrapped
// list of member Mention pills. When the slug is unknown (no such
// stored team, not a reserved pseudo-team), renders a subtle inline
// warning instead of throwing.
//
// Authors use this inside Card/Deck layouts on MDX pages:
//
//     <Card title="Marketing">
//       <TeamRoster slug="marketing" />
//     </Card>

interface Props {
  slug: string
}

export function TeamRoster({ slug }: Props) {
  const teams = useTeams()
  const users = useUsers()
  const team = findTeam(teams, slug)

  if (!team) {
    return (
      <div style={{ color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
        Unknown team: @{slug}
      </div>
    )
  }

  const wrap: CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    gap: '0.75rem',
  }
  const header: CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: '0.5rem',
    flexWrap: 'wrap',
  }
  const meta: CSSProperties = {
    color: 'var(--text-secondary)',
    fontSize: '0.875rem',
  }
  const chips: CSSProperties = {
    display: 'flex',
    flexWrap: 'wrap',
    gap: '0.35rem',
  }

  const members = team.members ?? []

  return (
    <div style={wrap}>
      <div style={header}>
        <Mention username={team.slug} />
        {team.display_name && (
          <span style={{ fontWeight: 500 }}>{team.display_name}</span>
        )}
      </div>
      {team.description && <div style={meta}>{team.description}</div>}
      {members.length === 0 ? (
        <div style={meta}>
          <Users size={14} style={{ verticalAlign: 'text-bottom', marginRight: '0.25rem' }} />
          No members yet.
        </div>
      ) : (
        <div style={chips}>
          {members.map((m) => {
            const u = findUser(users, m.username)
            const display = u?.display_name ?? m.username
            return (
              <span
                key={m.username}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: '0.25rem',
                  padding: '0.15rem 0.5rem',
                  borderRadius: '9999px',
                  fontSize: '0.8125rem',
                  background: 'var(--bg-secondary)',
                  border: '1px solid var(--border)',
                }}
                title={m.role ? `${display} — ${m.role}` : display}
              >
                <Mention username={m.username} plain />
                {m.role && (
                  <span
                    style={{
                      fontSize: '0.6875rem',
                      color: 'var(--text-secondary)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.05em',
                    }}
                  >
                    {m.role}
                  </span>
                )}
              </span>
            )
          })}
        </div>
      )}
    </div>
  )
}
