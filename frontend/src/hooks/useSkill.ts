import { useCallback, useEffect, useState } from 'react'
import type { SkillSummary } from './useSkills'

export interface SupportingFile {
  name: string
  size: number
  contentType: string
  url: string
}

export interface SkillDetail {
  slug: string
  name: string
  description: string
  manifestBody: string
  manifestUrl: string
  bundleUrl: string
  files: SupportingFile[]
  updatedAt: string
}

interface FileInfo {
  name: string
  size: number
  content_type: string
  url: string
}

/**
 * Strip a leading YAML frontmatter block so only the prose body is rendered.
 * Tolerant of CRLF and missing trailing newline.
 */
export function stripFrontmatter(source: string): string {
  const match = source.match(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/)
  return match ? source.slice(match[0].length) : source
}

/**
 * Fetch one skill's content for the detail page.
 *
 * Composition: we need name/description (via /api/skills to avoid reparsing
 * frontmatter in the browser), the SKILL.md body (via /api/files/...) with
 * frontmatter stripped, and any supporting files (via /api/files filtered by
 * the skill's prefix). All three fire in parallel.
 */
export function useSkill(slug: string) {
  const [skill, setSkill] = useState<SkillDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!slug) return
    setLoading(true)
    setError(null)
    const manifestPath = `skills/${slug}/SKILL.md`
    const manifestUrl = `/api/files/${manifestPath.split('/').map(encodeURIComponent).join('/')}`

    try {
      const [listResp, manifestResp, filesResp] = await Promise.all([
        fetch('/api/skills'),
        fetch(manifestUrl, { headers: { Accept: 'text/markdown' } }),
        fetch('/api/files'),
      ])

      if (!listResp.ok) throw new Error(`GET /api/skills → ${listResp.status}`)
      const list = (await listResp.json()) as SkillSummary[]
      const summary = Array.isArray(list) ? list.find(s => s.slug === slug) : undefined
      if (!summary) {
        setSkill(null)
        setError('Skill not found')
        return
      }

      if (!manifestResp.ok) {
        throw new Error(`GET SKILL.md → ${manifestResp.status}`)
      }
      const raw = await manifestResp.text()
      const body = stripFrontmatter(raw)

      if (!filesResp.ok) throw new Error(`GET /api/files → ${filesResp.status}`)
      const allFiles = (await filesResp.json()) as FileInfo[]
      const prefix = `skills/${slug}/`
      const supporting: SupportingFile[] = (Array.isArray(allFiles) ? allFiles : [])
        .filter(f => typeof f.name === 'string' && f.name.startsWith(prefix) && f.name !== `${prefix}SKILL.md`)
        .map(f => ({
          name: f.name.slice(prefix.length),
          size: f.size,
          contentType: f.content_type,
          url: f.url,
        }))
        .sort((a, b) => a.name.localeCompare(b.name))

      setSkill({
        slug: summary.slug,
        name: summary.name,
        description: summary.description,
        manifestBody: body,
        manifestUrl,
        bundleUrl: `/api/skills/${encodeURIComponent(slug)}`,
        files: supporting,
        updatedAt: summary.updated_at,
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load skill')
      setSkill(null)
    } finally {
      setLoading(false)
    }
  }, [slug])

  useEffect(() => {
    load()
    const onFile = (e: Event) => {
      const detail = (e as CustomEvent<{ name?: string }>).detail
      if (typeof detail?.name === 'string' && detail.name.startsWith(`skills/${slug}/`)) {
        load()
      }
    }
    window.addEventListener('agentboard:file-updated', onFile)
    return () => window.removeEventListener('agentboard:file-updated', onFile)
  }, [load, slug])

  return { skill, loading, error, reload: load }
}
