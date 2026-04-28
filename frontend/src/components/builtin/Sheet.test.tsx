import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { Sheet } from './Sheet'

vi.mock('../../hooks/useData', () => ({
  useData: vi.fn(),
}))

// Sheet now reads ctx.path off DataContext for folder auto-attach
// (`<Sheet />` resolves to the rendering page's own folder). The
// tests pass an explicit `source` so a null path is fine — but
// useDataContext throws without a provider, so stub it.
vi.mock('../../hooks/DataContext', () => ({
  useDataContext: () => ({ path: null, get: () => undefined, subscribe: () => () => {} }),
}))

import { useData } from '../../hooks/useData'
const mockUseData = vi.mocked(useData)

describe('Sheet', () => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    vi.clearAllMocks()
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true }) as unknown as typeof fetch
  })
  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('shows loading state', () => {
    mockUseData.mockReturnValue({ data: undefined, loading: true, error: null })
    render(<Sheet source="team.roster" />)
    expect(screen.getByText(/Loading/i)).toBeInTheDocument()
  })

  it('renders columns + rows from array-of-objects', () => {
    mockUseData.mockReturnValue({
      data: [
        { id: '1', name: 'Alice', role: 'Engineer' },
        { id: '2', name: 'Bob', role: 'Designer' },
      ],
      loading: false,
      error: null,
    })
    render(<Sheet source="team.roster" />)
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
    expect(screen.getByText('Engineer')).toBeInTheDocument()
    // Column headers — id stays first, then the other inferred keys.
    const headerCells = screen.getAllByRole('columnheader')
    expect(headerCells.map(h => h.textContent)).toEqual(
      expect.arrayContaining(['id', 'name', 'role']),
    )
  })

  it('renders empty-state when data is an empty array', () => {
    mockUseData.mockReturnValue({ data: [], loading: false, error: null })
    render(<Sheet source="team.roster" />)
    expect(screen.getByText(/Empty/)).toBeInTheDocument()
  })

  it('refuses to render a non-array source', () => {
    mockUseData.mockReturnValue({ data: { not: 'an array' }, loading: false, error: null })
    render(<Sheet source="team.roster" />)
    expect(screen.getByText(/not an array/i)).toBeInTheDocument()
  })

  it('clicking a text cell enters edit mode', () => {
    mockUseData.mockReturnValue({
      data: [{ id: '1', name: 'Alice' }],
      loading: false,
      error: null,
    })
    render(<Sheet source="team.roster" />)
    fireEvent.click(screen.getByText('Alice'))
    const input = screen.getByDisplayValue('Alice')
    expect(input).toBeInTheDocument()
  })

  it('Enter key commits an edit via PATCH /api/data/{source}/{id}', async () => {
    mockUseData.mockReturnValue({
      data: [{ id: '1', name: 'Alice', role: 'Engineer' }],
      loading: false,
      error: null,
    })
    render(<Sheet source="team.roster" />)
    fireEvent.click(screen.getByText('Alice'))
    const input = screen.getByDisplayValue('Alice')
    fireEvent.change(input, { target: { value: 'Alicia' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1))
    const [url, init] = (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0]
    expect(url).toBe('/api/data/team.roster/1')
    expect(init.method).toBe('PATCH')
    expect(JSON.parse(init.body as string)).toEqual({ name: 'Alicia' })
  })

  it('Escape key cancels edit without calling the API', () => {
    mockUseData.mockReturnValue({
      data: [{ id: '1', name: 'Alice' }],
      loading: false,
      error: null,
    })
    render(<Sheet source="team.roster" />)
    fireEvent.click(screen.getByText('Alice'))
    const input = screen.getByDisplayValue('Alice')
    fireEvent.change(input, { target: { value: 'X' } })
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(globalThis.fetch).not.toHaveBeenCalled()
    expect(screen.getByText('Alice')).toBeInTheDocument()
  })

  it('id column is readonly', () => {
    mockUseData.mockReturnValue({
      data: [{ id: '1', name: 'Alice' }],
      loading: false,
      error: null,
    })
    render(<Sheet source="team.roster" />)
    // Clicking '1' should NOT create an input.
    fireEvent.click(screen.getByText('1'))
    expect(screen.queryByDisplayValue('1')).not.toBeInTheDocument()
  })

  it('coerces integer-looking strings to numbers on save', async () => {
    mockUseData.mockReturnValue({
      data: [{ id: '1', name: 'A', score: 5 }],
      loading: false,
      error: null,
    })
    render(<Sheet source="s" />)
    fireEvent.click(screen.getByText('5'))
    const input = screen.getByDisplayValue('5')
    fireEvent.change(input, { target: { value: '42' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled())
    const init = (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0][1]
    expect(JSON.parse(init.body as string)).toEqual({ score: 42 })
  })

  it('Add row posts to /api/data/{source} with a seeded row', async () => {
    mockUseData.mockReturnValue({
      data: [{ id: '1', name: 'Alice', role: 'Engineer' }],
      loading: false,
      error: null,
    })
    render(<Sheet source="team.roster" />)
    fireEvent.click(screen.getByRole('button', { name: /Add row/i }))
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled())
    const [url, init] = (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0]
    expect(url).toBe('/api/data/team.roster')
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body as string)
    expect(typeof body.id).toBe('string')
    expect(body.name).toBe('')
    expect(body.role).toBe('')
  })
})
