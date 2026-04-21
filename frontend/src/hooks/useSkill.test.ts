import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { stripFrontmatter, useSkill } from './useSkill'

describe('stripFrontmatter', () => {
  it('removes a standard block', () => {
    const input = '---\nname: foo\ndescription: bar\n---\n\n# Body\n'
    expect(stripFrontmatter(input)).toBe('\n# Body\n')
  })

  it('is a no-op when there is no frontmatter', () => {
    const input = '# Just a heading\n\nprose\n'
    expect(stripFrontmatter(input)).toBe(input)
  })

  it('handles CRLF line endings', () => {
    const input = '---\r\nname: foo\r\ndescription: bar\r\n---\r\nBody line\r\n'
    expect(stripFrontmatter(input)).toBe('Body line\r\n')
  })

  it('leaves content alone when frontmatter is unclosed', () => {
    const input = '---\nname: oops\n\nno closing marker\n'
    expect(stripFrontmatter(input)).toBe(input)
  })
})

type JsonResponse<T> = { ok: boolean; status: number; json: () => Promise<T>; text: () => Promise<string> }

function mockResponse<T>(body: T, overrides: Partial<JsonResponse<T>> = {}): Response {
  return {
    ok: true,
    status: 200,
    json: async () => body,
    text: async () => (typeof body === 'string' ? body : JSON.stringify(body)),
    ...overrides,
  } as unknown as Response
}

const skillList = [
  { slug: 'demo', name: 'Demo', description: 'test skill', path: 'skills/demo', updated_at: '2026-04-20T00:00:00Z' },
]

const filesIndex = [
  { name: 'skills/demo/SKILL.md', size: 100, content_type: 'text/markdown', url: '/api/files/skills/demo/SKILL.md' },
  { name: 'skills/demo/examples.md', size: 50, content_type: 'text/markdown', url: '/api/files/skills/demo/examples.md' },
  { name: 'skills/other/SKILL.md', size: 80, content_type: 'text/markdown', url: '/api/files/skills/other/SKILL.md' },
  { name: 'banner.png', size: 4096, content_type: 'image/png', url: '/api/files/banner.png' },
]

const manifestSource = '---\nname: Demo\ndescription: test skill\n---\n\n# Demo\n\nHello body.\n'

describe('useSkill', () => {
  afterEach(() => vi.restoreAllMocks())

  it('loads metadata + body + supporting files for a slug', async () => {
    globalThis.fetch = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/skills') return mockResponse(skillList)
      if (url === '/api/files') return mockResponse(filesIndex)
      if (url.endsWith('/SKILL.md')) return mockResponse(manifestSource)
      throw new Error(`unexpected fetch: ${url}`)
    })

    const { result } = renderHook(() => useSkill('demo'))
    await waitFor(() => expect(result.current.loading).toBe(false))

    expect(result.current.error).toBeNull()
    expect(result.current.skill).not.toBeNull()
    const skill = result.current.skill!
    expect(skill.slug).toBe('demo')
    expect(skill.name).toBe('Demo')
    expect(skill.description).toBe('test skill')
    // Frontmatter stripped, body preserved.
    expect(skill.manifestBody).toContain('# Demo')
    expect(skill.manifestBody).not.toContain('---')
    // Bundle URL points at the zip endpoint.
    expect(skill.bundleUrl).toBe('/api/skills/demo')
    // Supporting files are filtered and path-trimmed, excluding SKILL.md.
    expect(skill.files).toHaveLength(1)
    expect(skill.files[0].name).toBe('examples.md')
  })

  it('reports not found when the slug is missing from /api/skills', async () => {
    globalThis.fetch = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/skills') return mockResponse(skillList)
      if (url === '/api/files') return mockResponse(filesIndex)
      if (url.endsWith('/SKILL.md')) return mockResponse(manifestSource, { ok: false, status: 404 })
      throw new Error(`unexpected fetch: ${url}`)
    })

    const { result } = renderHook(() => useSkill('missing'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.skill).toBeNull()
    expect(result.current.error).toMatch(/not found/i)
  })

  it('surfaces a fetch error for the manifest', async () => {
    globalThis.fetch = vi.fn().mockImplementation(async (url: string) => {
      if (url === '/api/skills') return mockResponse(skillList)
      if (url === '/api/files') return mockResponse(filesIndex)
      if (url.endsWith('/SKILL.md')) return mockResponse('', { ok: false, status: 500 })
      throw new Error(`unexpected fetch: ${url}`)
    })

    const { result } = renderHook(() => useSkill('demo'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.skill).toBeNull()
    expect(result.current.error).toMatch(/→ 500/)
  })
})
