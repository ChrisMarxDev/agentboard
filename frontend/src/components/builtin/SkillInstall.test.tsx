import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { SkillInstall } from './SkillInstall'

describe('SkillInstall', () => {
  it('renders the slug and an agent-install prompt', () => {
    render(<SkillInstall slug="deploy-helper" />)
    expect(screen.getByText('deploy-helper')).toBeInTheDocument()
    // Prompt should instruct an agent to fetch, verify, and install.
    expect(
      screen.getByText(/Install the "deploy-helper" skill from this AgentBoard/),
    ).toBeInTheDocument()
    expect(screen.getByText(/Verify it's not harmful/)).toBeInTheDocument()
  })

  it('returns null without a slug', () => {
    const { container } = render(<SkillInstall slug="" />)
    expect(container.innerHTML).toBe('')
  })

  it('references the bundle, manifest, and page URLs using the current origin', () => {
    // jsdom sets origin to http://localhost:3000 by default.
    render(<SkillInstall slug="my-skill" />)
    // Bundle (zip) URL
    expect(
      screen.getByText(/http:\/\/localhost:3000\/api\/skills\/my-skill/),
    ).toBeInTheDocument()
    // Raw manifest URL
    expect(
      screen.getByText(
        /http:\/\/localhost:3000\/api\/content\/skills\/my-skill\/SKILL\.md/,
      ),
    ).toBeInTheDocument()
    // Human-readable page URL
    expect(
      screen.getByText(/http:\/\/localhost:3000\/skills\/my-skill/),
    ).toBeInTheDocument()
  })

  it('does not advertise an install.sh curl-bash line', () => {
    render(<SkillInstall slug="deploy-helper" />)
    expect(screen.queryByText(/install\.sh \| bash/)).toBeNull()
  })

  it('exposes a plain zip download link as a fallback', () => {
    render(<SkillInstall slug="deploy-helper" />)
    const link = screen.getByRole('link', { name: /download zip/i })
    expect(link).toHaveAttribute(
      'href',
      'http://localhost:3000/api/skills/deploy-helper',
    )
  })
})

// --- path-detection helper (lives in PageRenderer, re-implemented here
// for an inline sanity check so the contract is pinned in tests close
// to the component that reacts to it).
function skillSlugFromPath(path: string): string | null {
  const parts = path.split('/').filter(Boolean)
  for (let i = 0; i < parts.length - 1; i++) {
    if (parts[i] === 'skills') return parts[i + 1]
  }
  return null
}

describe('skillSlugFromPath', () => {
  it('extracts slug at top level', () => {
    expect(skillSlugFromPath('skills/my-skill')).toBe('my-skill')
    expect(skillSlugFromPath('/skills/deploy-helper')).toBe('deploy-helper')
  })
  it('extracts slug at any depth', () => {
    expect(skillSlugFromPath('/team/skills/alpha')).toBe('alpha')
    expect(skillSlugFromPath('/catalog/skills/beta/docs')).toBe('beta')
  })
  it('returns null if `skills` is the trailing segment (no slug follows)', () => {
    expect(skillSlugFromPath('/skills')).toBe(null)
    expect(skillSlugFromPath('/team/skills/')).toBe(null)
  })
  it('returns null when no `skills` segment is present', () => {
    expect(skillSlugFromPath('/')).toBe(null)
    expect(skillSlugFromPath('/features/x')).toBe(null)
  })
})
