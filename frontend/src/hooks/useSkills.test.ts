import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { useSkills, type SkillSummary } from './useSkills'

const sample: SkillSummary[] = [
  { slug: 'a', name: 'Alpha', description: 'First', path: 'skills/a', updated_at: '2026-04-20T00:00:00Z' },
  { slug: 'b', name: 'Bravo', description: 'Second', path: 'skills/b', updated_at: '2026-04-20T00:00:00Z' },
]

function mockFetch(response: unknown, init: Partial<Response> = {}) {
  return vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => response,
    text: async () => JSON.stringify(response),
    ...init,
  } as Response)
}

describe('useSkills', () => {
  beforeEach(() => {
    globalThis.fetch = mockFetch(sample)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('fetches /api/skills on mount', async () => {
    const { result } = renderHook(() => useSkills())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/skills')
    expect(result.current.skills).toEqual(sample)
    expect(result.current.error).toBeNull()
  })

  it('surfaces HTTP errors', async () => {
    globalThis.fetch = mockFetch(null, { ok: false, status: 500 })
    const { result } = renderHook(() => useSkills())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.skills).toEqual([])
    expect(result.current.error).toMatch(/→ 500/)
  })

  it('refetches when a file under skills/ changes', async () => {
    const fetchSpy = mockFetch(sample)
    globalThis.fetch = fetchSpy
    const { result } = renderHook(() => useSkills())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(fetchSpy).toHaveBeenCalledTimes(1)

    act(() => {
      window.dispatchEvent(
        new CustomEvent('agentboard:file-updated', { detail: { name: 'skills/a/SKILL.md' } }),
      )
    })
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2))
  })

  it('ignores file-updated events outside skills/', async () => {
    const fetchSpy = mockFetch(sample)
    globalThis.fetch = fetchSpy
    const { result } = renderHook(() => useSkills())
    await waitFor(() => expect(result.current.loading).toBe(false))

    act(() => {
      window.dispatchEvent(
        new CustomEvent('agentboard:file-updated', { detail: { name: 'exports/q1.csv' } }),
      )
    })
    // Give the event loop a tick; no refetch should happen.
    await new Promise(r => setTimeout(r, 20))
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })

  it('coerces non-array responses to an empty list', async () => {
    globalThis.fetch = mockFetch({ oops: true })
    const { result } = renderHook(() => useSkills())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.skills).toEqual([])
  })
})
