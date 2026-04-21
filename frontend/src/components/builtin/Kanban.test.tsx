import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { Kanban } from './Kanban'

vi.mock('../../hooks/useData', () => ({
  useData: vi.fn(),
}))

import { useData } from '../../hooks/useData'
const mockUseData = vi.mocked(useData)

describe('Kanban', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows loading state', () => {
    mockUseData.mockReturnValue({ data: undefined, loading: true, error: null })
    render(<Kanban source="tasks" groupBy="status" />)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('renders items grouped by status', () => {
    mockUseData.mockReturnValue({
      data: [
        { id: '1', title: 'Fix bug', status: 'todo' },
        { id: '2', title: 'Deploy', status: 'in_progress' },
        { id: '3', title: 'Write docs', status: 'done' },
      ],
      loading: false,
      error: null,
    })
    render(<Kanban source="tasks" groupBy="status" columns={['todo', 'in_progress', 'done']} />)

    expect(screen.getByText('Fix bug')).toBeInTheDocument()
    expect(screen.getByText('Deploy')).toBeInTheDocument()
    expect(screen.getByText('Write docs')).toBeInTheDocument()
    expect(screen.getByText('To Do')).toBeInTheDocument()
    expect(screen.getByText('In Progress')).toBeInTheDocument()
    expect(screen.getByText('Done')).toBeInTheDocument()
  })

  it('shows empty columns', () => {
    mockUseData.mockReturnValue({
      data: [
        { id: '1', title: 'Only todo', status: 'todo' },
      ],
      loading: false,
      error: null,
    })
    render(<Kanban source="tasks" groupBy="status" columns={['todo', 'done']} />)

    expect(screen.getByText('Only todo')).toBeInTheDocument()
    expect(screen.getByText('Empty')).toBeInTheDocument()
  })

  it('returns null for non-array data', () => {
    mockUseData.mockReturnValue({ data: 'not an array', loading: false, error: null })
    const { container } = render(<Kanban source="tasks" groupBy="status" />)
    expect(container.innerHTML).toBe('')
  })

  describe('click → detail modal', () => {
    const tasks = [
      { id: '1', title: 'Fix bug', status: 'todo', assignee: 'alice', priority: 'high' },
    ]

    it('opens a modal when a card is clicked, showing non-title fields', () => {
      mockUseData.mockReturnValue({ data: tasks, loading: false, error: null })
      render(<Kanban source="tasks" groupBy="status" />)

      fireEvent.click(screen.getByText('Fix bug'))

      const dialog = screen.getByRole('dialog')
      expect(dialog).toBeInTheDocument()
      // Title shows in the modal header.
      expect(dialog).toHaveTextContent('Fix bug')
      // Non-title fields are listed.
      expect(dialog).toHaveTextContent('assignee')
      expect(dialog).toHaveTextContent('alice')
      expect(dialog).toHaveTextContent('priority')
      expect(dialog).toHaveTextContent('high')
    })

    it('closes the modal on Escape', () => {
      mockUseData.mockReturnValue({ data: tasks, loading: false, error: null })
      render(<Kanban source="tasks" groupBy="status" />)

      fireEvent.click(screen.getByText('Fix bug'))
      expect(screen.getByRole('dialog')).toBeInTheDocument()

      fireEvent.keyDown(window, { key: 'Escape' })
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    it('closes the modal on backdrop click', () => {
      mockUseData.mockReturnValue({ data: tasks, loading: false, error: null })
      render(<Kanban source="tasks" groupBy="status" />)

      fireEvent.click(screen.getByText('Fix bug'))
      const dialog = screen.getByRole('dialog')
      fireEvent.click(dialog)
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  describe('drag-drop between columns', () => {
    const originalFetch = globalThis.fetch

    beforeEach(() => {
      globalThis.fetch = vi.fn().mockResolvedValue({ ok: true }) as unknown as typeof fetch
    })
    afterEach(() => {
      globalThis.fetch = originalFetch
    })

    function dragCardToColumn(cardText: string, targetHeading: string) {
      const card = screen.getByText(cardText)
      const targetCol = screen.getByText(targetHeading).closest('div')!.parentElement!
      fireEvent.dragStart(card)
      fireEvent.dragOver(targetCol)
      fireEvent.drop(targetCol)
      fireEvent.dragEnd(card)
    }

    it('PATCHes /api/data/{source}/{id} with new group value on drop', async () => {
      mockUseData.mockReturnValue({
        data: [{ id: '1', title: 'Fix bug', status: 'todo' }],
        loading: false,
        error: null,
      })
      render(
        <Kanban source="sprint.tasks" groupBy="status" columns={['todo', 'done']} />,
      )

      dragCardToColumn('Fix bug', 'Done')

      await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1))
      const [url, init] = (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0]
      expect(url).toBe('/api/data/sprint.tasks/1')
      expect(init.method).toBe('PATCH')
      expect(JSON.parse(init.body as string)).toEqual({ status: 'done' })
    })

    it('does not PATCH when dropping on the same column', async () => {
      mockUseData.mockReturnValue({
        data: [{ id: '1', title: 'Fix bug', status: 'todo' }],
        loading: false,
        error: null,
      })
      render(
        <Kanban source="sprint.tasks" groupBy="status" columns={['todo', 'done']} />,
      )

      dragCardToColumn('Fix bug', 'To Do')

      // Give any pending promise a tick; fetch should still not have been called.
      await Promise.resolve()
      expect(globalThis.fetch).not.toHaveBeenCalled()
    })

    it('does not PATCH for cards without an id', async () => {
      mockUseData.mockReturnValue({
        data: [{ title: 'Anonymous', status: 'todo' }],
        loading: false,
        error: null,
      })
      render(
        <Kanban source="sprint.tasks" groupBy="status" columns={['todo', 'done']} />,
      )

      dragCardToColumn('Anonymous', 'Done')

      await Promise.resolve()
      expect(globalThis.fetch).not.toHaveBeenCalled()
    })
  })
})
