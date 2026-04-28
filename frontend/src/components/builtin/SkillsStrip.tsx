import { useEffect, useMemo, useState, type CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { Sparkles, ArrowRight } from 'lucide-react'
import { apiFetch } from '../../lib/session'
import { useData } from '../../hooks/useData'

// <SkillsStrip limit={6} /> — a row of skill cards. Two ways to source:
//
//   1. Curated — the home page provides `featured_skills: [slug, slug]`
//      in its frontmatter; pass `source="featured_skills"`.
//   2. Auto — no source given; we fetch /api/skills and take the first
//      `limit` entries.
//
// Each card links to /skills/<slug>. The Install affordance lives on
// that subpage (the existing <SkillInstall> component) — keep this
// strip compact.

interface SkillsStripProps {
  source?: string
  limit?: number
}

interface ManifestSkill {
  slug: string
  name?: string
  description?: string
}

const ROW: CSSProperties = {
  display: 'flex',
  gap: '0.625rem',
  flexWrap: 'wrap',
}
const CARD: CSSProperties = {
  flex: '1 1 220px',
  minWidth: '180px',
  padding: '0.85rem 1rem',
  borderRadius: '0.625rem',
  border: '1px solid var(--border)',
  background: 'var(--bg)',
  textDecoration: 'none',
  color: 'var(--text)',
  display: 'flex',
  flexDirection: 'column',
  gap: '0.35rem',
}
const NAME: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: '0.4rem',
  fontWeight: 600,
}
const DESC: CSSProperties = {
  fontSize: '0.8125rem',
  color: 'var(--text-secondary)',
  margin: 0,
  display: '-webkit-box',
  WebkitLineClamp: 2,
  WebkitBoxOrient: 'vertical' as const,
  overflow: 'hidden',
}

export function SkillsStrip({ source, limit = 6 }: SkillsStripProps) {
  const { data: featured } = useData(source ?? '')
  const [allSkills, setAllSkills] = useState<ManifestSkill[] | null>(null)

  useEffect(() => {
    let cancelled = false
    apiFetch('/api/skills')
      .then(r => r.ok ? r.json() : Promise.reject(r.status))
      .then(d => {
        if (cancelled) return
        const list: ManifestSkill[] = Array.isArray(d) ? d : (d.skills ?? [])
        setAllSkills(list)
      })
      .catch(() => {
        if (!cancelled) setAllSkills([])
      })
    return () => { cancelled = true }
  }, [])

  const skills = useMemo<ManifestSkill[]>(() => {
    // Curated path — author gave a slug list; map onto the manifest.
    if (Array.isArray(featured) && featured.length > 0 && allSkills) {
      const bySlug = new Map(allSkills.map(s => [s.slug, s]))
      return featured
        .map(entry => {
          const slug = typeof entry === 'string' ? entry : (entry as { slug?: string }).slug
          return slug ? bySlug.get(slug) ?? { slug } : null
        })
        .filter((s): s is ManifestSkill => s !== null)
        .slice(0, limit)
    }
    // Auto path — first N skills alphabetically by slug.
    if (allSkills) {
      return [...allSkills].sort((a, b) => a.slug.localeCompare(b.slug)).slice(0, limit)
    }
    return []
  }, [featured, allSkills, limit])

  if (allSkills === null) {
    return (
      <div style={{ color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
        Loading skills…
      </div>
    )
  }

  if (skills.length === 0) {
    return (
      <div style={{ color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
        No skills installed yet. Add one under <code>skills/&lt;slug&gt;/SKILL.md</code>.
      </div>
    )
  }

  return (
    <div style={ROW}>
      {skills.map(s => (
        <Link key={s.slug} to={`/skills/${s.slug}`} style={CARD}>
          <div style={NAME}>
            <Sparkles size={13} strokeWidth={2.5} style={{ color: 'var(--accent)' }} />
            {s.name ?? s.slug}
          </div>
          {s.description && <p style={DESC}>{s.description}</p>}
          <div
            style={{
              fontSize: '0.75rem',
              color: 'var(--accent)',
              display: 'inline-flex',
              alignItems: 'center',
              gap: '0.25rem',
              marginTop: 'auto',
              fontWeight: 500,
            }}
          >
            Open <ArrowRight size={11} />
          </div>
        </Link>
      ))}
    </div>
  )
}
