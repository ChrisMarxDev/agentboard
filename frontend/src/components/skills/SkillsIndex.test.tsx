import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import SkillsIndex from './SkillsIndex'

function mockFetch(body: unknown, ok = true, status = 200): typeof fetch {
  return vi.fn().mockResolvedValue({
    ok,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response)
}

function renderIndex() {
  return render(
    <MemoryRouter initialEntries={['/skills']}>
      <SkillsIndex />
    </MemoryRouter>,
  )
}

describe('SkillsIndex', () => {
  afterEach(() => vi.restoreAllMocks())

  it('shows the empty state when no skills exist', async () => {
    globalThis.fetch = mockFetch([])
    renderIndex()
    await waitFor(() => expect(screen.getByText(/no skills hosted yet/i)).toBeInTheDocument())
    expect(screen.getByText(/files\/skills\//)).toBeInTheDocument()
  })

  it('renders each skill with a view and download control', async () => {
    globalThis.fetch = mockFetch([
      { slug: 'alpha', name: 'Alpha', description: 'A', path: 'skills/alpha', updated_at: '2026-04-20T00:00:00Z' },
      { slug: 'bravo', name: 'Bravo', description: 'B', path: 'skills/bravo', updated_at: '2026-04-20T00:00:00Z' },
    ])
    renderIndex()
    await waitFor(() => expect(screen.getByText('Alpha')).toBeInTheDocument())
    expect(screen.getByText('Bravo')).toBeInTheDocument()

    // View links point to /skills/:slug
    const viewAlpha = screen.getByRole('link', { name: /view Alpha/i })
    expect(viewAlpha).toHaveAttribute('href', '/skills/alpha')

    // Download links hit the zip endpoint
    const dlBravo = screen.getByRole('link', { name: /download bravo bundle/i })
    expect(dlBravo).toHaveAttribute('href', '/api/skills/bravo')
  })

  it('surfaces an error banner on HTTP failure', async () => {
    globalThis.fetch = mockFetch(null, false, 500)
    renderIndex()
    await waitFor(() => expect(screen.getByText(/→ 500/)).toBeInTheDocument())
  })
})
