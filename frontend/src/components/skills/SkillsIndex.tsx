import { Link } from 'react-router-dom'
import { Download, ExternalLink } from 'lucide-react'
import { useSkills } from '../../hooks/useSkills'

/**
 * Route component for `/skills`. Lists every skill the server exposes via
 * GET /api/skills with name + description and actions to view or download.
 * The download link goes straight to the backend zip endpoint; the browser
 * handles the save dialog.
 */
export default function SkillsIndex() {
  const { skills, loading, error } = useSkills()

  return (
    <div className="relative">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold mb-2" style={{ color: 'var(--text)' }}>
          Skills
        </h1>
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          Anthropic-format skills hosted on this AgentBoard. Agents can discover them via
          <code
            className="mx-1 px-1 py-0.5 rounded text-xs"
            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}
          >
            agentboard_list_skills
          </code>
          and fetch individual bundles with
          <code
            className="mx-1 px-1 py-0.5 rounded text-xs"
            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}
          >
            agentboard_get_skill
          </code>
          .
        </p>
      </header>

      {error && (
        <div
          className="p-3 mb-4 rounded-md text-sm"
          style={{ background: 'rgba(239,68,68,0.08)', color: 'var(--error)' }}
        >
          {error}
        </div>
      )}

      {loading && skills.length === 0 && (
        <div style={{ color: 'var(--text-secondary)' }} className="text-sm">
          Loading…
        </div>
      )}

      {!loading && skills.length === 0 && !error && (
        <div
          className="p-6 rounded-md text-sm"
          style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', color: 'var(--text-secondary)' }}
        >
          <p className="mb-2" style={{ color: 'var(--text)' }}>No skills hosted yet.</p>
          <p>
            Ask an agent to write one under
            <code
              className="mx-1 px-1 py-0.5 rounded text-xs"
              style={{ background: 'var(--bg)', border: '1px solid var(--border)' }}
            >
              files/skills/&lt;slug&gt;/SKILL.md
            </code>
            with <code>name</code> and <code>description</code> in YAML frontmatter.
          </p>
        </div>
      )}

      <ul className="flex flex-col gap-3">
        {skills.map(s => (
          <li
            key={s.slug}
            className="rounded-md p-4 flex items-start justify-between gap-4"
            style={{
              background: 'var(--bg-secondary)',
              border: '1px solid var(--border)',
            }}
          >
            <div className="min-w-0 flex-1">
              <Link
                to={`/skills/${encodeURIComponent(s.slug)}`}
                className="inline-flex items-center gap-1.5 font-medium hover:underline"
                style={{ color: 'var(--text)' }}
              >
                {s.name}
                <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
                  ({s.slug})
                </span>
              </Link>
              <p className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>
                {s.description}
              </p>
            </div>

            <div className="flex items-center gap-1 shrink-0">
              <Link
                to={`/skills/${encodeURIComponent(s.slug)}`}
                title="View skill"
                aria-label={`View ${s.name}`}
                className="h-8 w-8 flex items-center justify-center rounded-md"
                style={{
                  background: 'var(--bg)',
                  border: '1px solid var(--border)',
                  color: 'var(--text-secondary)',
                }}
              >
                <ExternalLink size={14} />
              </Link>
              <a
                href={`/api/skills/${encodeURIComponent(s.slug)}`}
                title="Download bundle (.zip)"
                aria-label={`Download ${s.name} bundle`}
                className="h-8 w-8 flex items-center justify-center rounded-md"
                style={{
                  background: 'var(--bg)',
                  border: '1px solid var(--border)',
                  color: 'var(--text-secondary)',
                }}
              >
                <Download size={14} />
              </a>
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}
