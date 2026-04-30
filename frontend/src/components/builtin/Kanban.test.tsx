import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { Kanban } from './Kanban'

// useData is keyed: the Kanban now calls it twice — once for the
// folder source (tasks) and once for the page-frontmatter `columns`
// override. The tests configure tasks via mockUseData; columns via
// the global default below (returns nothing, so the prop or the data
// derive the lanes).
vi.mock('../../hooks/useData', () => ({
  useData: vi.fn(),
}))

// Kanban now reads ctx.path off DataContext for folder auto-attach
// (`<Kanban groupBy=...>` with no source resolves to the rendering
// page's own folder). The tests use explicit `source` so null path
// is fine — but useDataContext throws without a provider, so stub it.
vi.mock('../../hooks/DataContext', () => ({
  useDataContext: () => ({ path: null, get: () => undefined, subscribe: () => () => {} }),
}))

// useMe drives the comment-author UI in the detail dialog. Tests
// don't exercise that path; null = "no me yet".
vi.mock('../../hooks/useMe', () => ({
  useMe: () => null,
}))

// Kanban renders <AssigneeStrip> which pulls useUsers → /api/users and
// useTeams → /api/teams on mount. Those fetches pollute the spy in the
// drag-drop tests below, so we stub both.
vi.mock('../../hooks/useUsers', () => ({
  useUsers: () => [],
  findUser: () => undefined,
}))
vi.mock('../../hooks/useTeams', () => ({
  useTeams: () => [],
  findTeam: () => undefined,
}))

import { useData } from '../../hooks/useData'
const mockUseData = vi.mocked(useData)

// setRowsData configures the row-shaped useData call (the kanban's
// source). `columns` always returns an empty result, leaving lane
// resolution to the explicit prop or default trio.
const EMPTY_COLUMNS = { data: undefined, loading: false, error: null }
function setRowsData(value: { data: unknown; loading: boolean; error: unknown }) {
  mockUseData.mockImplementation((key: string) => {
    if (key === 'columns') return EMPTY_COLUMNS as ReturnType<typeof useData>
    return value as ReturnType<typeof useData>
  })
}

describe('Kanban', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows loading state', () => {
    setRowsData({ data: undefined, loading: true, error: null })
    render(<Kanban source="tasks" groupBy="status" />)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('renders items grouped by status', () => {
    setRowsData({
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
    setRowsData({
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
    setRowsData({ data: 'not an array', loading: false, error: null })
    const { container } = render(<Kanban source="tasks" groupBy="status" />)
    expect(container.innerHTML).toBe('')
  })

  describe('click → detail modal', () => {
    // `customField` is intentionally non-canonical — `assignee`,
    // `priority`, `due`, `labels`, `parent`, etc. now have dedicated
    // dialog widgets, so only unknown fields fall through to the
    // "Other fields" section. We assert the fallback path renders.
    const tasks = [
      { id: '1', title: 'Fix bug', status: 'todo', customField: 'arbitrary-value' },
    ]

    it('opens a modal when a card is clicked, showing non-title fields', () => {
      setRowsData({ data: tasks, loading: false, error: null })
      render(<Kanban source="tasks" groupBy="status" />)

      fireEvent.click(screen.getByText('Fix bug'))

      const dialog = screen.getByRole('dialog')
      expect(dialog).toBeInTheDocument()
      // Title shows in the modal header.
      expect(dialog).toHaveTextContent('Fix bug')
      // Non-title, non-canonical field falls through to Other fields.
      expect(dialog).toHaveTextContent('customField')
      expect(dialog).toHaveTextContent('arbitrary-value')
    })

    it('closes the modal on Escape', () => {
      setRowsData({ data: tasks, loading: false, error: null })
      render(<Kanban source="tasks" groupBy="status" />)

      fireEvent.click(screen.getByText('Fix bug'))
      expect(screen.getByRole('dialog')).toBeInTheDocument()

      fireEvent.keyDown(window, { key: 'Escape' })
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    it('closes the modal on backdrop click', () => {
      setRowsData({ data: tasks, loading: false, error: null })
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

    it('PATCHes /api/{source}/{id} with new group value on drop', async () => {
      setRowsData({
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
      expect(url).toBe('/api/sprint.tasks/1')
      expect(init.method).toBe('PATCH')
      expect(JSON.parse(init.body as string)).toEqual({ status: 'done' })
    })

    it('does not PATCH when dropping on the same column', async () => {
      setRowsData({
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
      setRowsData({
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

  describe('ordering', () => {
    it('sorts cards within a column by numeric order field', () => {
      setRowsData({
        data: [
          { id: '1', title: 'Gamma', status: 'todo', order: 3 },
          { id: '2', title: 'Alpha', status: 'todo', order: 1 },
          { id: '3', title: 'Beta', status: 'todo', order: 2 },
        ],
        loading: false,
        error: null,
      })
      render(<Kanban source="tasks" groupBy="status" columns={['todo']} />)

      const cards = screen.getAllByRole('button').filter(el => el.getAttribute('draggable') === 'true')
      expect(cards.map(c => c.textContent)).toEqual(['Alpha', 'Beta', 'Gamma'])
    })

    it('keeps array order when no order fields are present', () => {
      setRowsData({
        data: [
          { id: '1', title: 'First', status: 'todo' },
          { id: '2', title: 'Second', status: 'todo' },
          { id: '3', title: 'Third', status: 'todo' },
        ],
        loading: false,
        error: null,
      })
      render(<Kanban source="tasks" groupBy="status" columns={['todo']} />)

      const cards = screen.getAllByRole('button').filter(el => el.getAttribute('draggable') === 'true')
      expect(cards.map(c => c.textContent)).toEqual(['First', 'Second', 'Third'])
    })
  })

  describe('delete', () => {
    const originalFetch = globalThis.fetch

    beforeEach(() => {
      globalThis.fetch = vi.fn().mockResolvedValue({ ok: true }) as unknown as typeof fetch
    })
    afterEach(() => {
      globalThis.fetch = originalFetch
    })

    it('opens a confirm dialog when the trash icon is clicked', () => {
      setRowsData({
        data: [{ id: '1', title: 'Fix bug', status: 'todo' }],
        loading: false,
        error: null,
      })
      render(<Kanban source="tasks" groupBy="status" columns={['todo']} />)

      fireEvent.click(screen.getByLabelText('Delete card'))

      const dialog = screen.getByRole('dialog', { name: /confirm delete card/i })
      expect(dialog).toBeInTheDocument()
      expect(dialog).toHaveTextContent('Fix bug')
    })

    it('does not open the card modal when the trash icon is clicked', () => {
      setRowsData({
        data: [{ id: '1', title: 'Fix bug', status: 'todo' }],
        loading: false,
        error: null,
      })
      render(<Kanban source="tasks" groupBy="status" columns={['todo']} />)

      fireEvent.click(screen.getByLabelText('Delete card'))

      // The details modal ("Card details") must not appear — only the confirm dialog.
      const dialogs = screen.getAllByRole('dialog')
      expect(dialogs).toHaveLength(1)
      expect(dialogs[0]).toHaveAccessibleName(/confirm delete card/i)
    })

    it('DELETEs /api/{source}/{id} when confirmed', async () => {
      setRowsData({
        data: [{ id: '1', title: 'Fix bug', status: 'todo' }],
        loading: false,
        error: null,
      })
      render(<Kanban source="sprint.tasks" groupBy="status" columns={['todo']} />)

      fireEvent.click(screen.getByLabelText('Delete card'))
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

      await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1))
      const [url, init] = (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0]
      expect(url).toBe('/api/sprint.tasks/1')
      expect(init.method).toBe('DELETE')
    })

    it('does nothing on Cancel', async () => {
      setRowsData({
        data: [{ id: '1', title: 'Fix bug', status: 'todo' }],
        loading: false,
        error: null,
      })
      render(<Kanban source="sprint.tasks" groupBy="status" columns={['todo']} />)

      fireEvent.click(screen.getByLabelText('Delete card'))
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

      await Promise.resolve()
      expect(globalThis.fetch).not.toHaveBeenCalled()
      expect(screen.queryByRole('dialog', { name: /confirm delete card/i })).not.toBeInTheDocument()
    })

    it('closes the confirm dialog on Escape', () => {
      setRowsData({
        data: [{ id: '1', title: 'Fix bug', status: 'todo' }],
        loading: false,
        error: null,
      })
      render(<Kanban source="tasks" groupBy="status" columns={['todo']} />)

      fireEvent.click(screen.getByLabelText('Delete card'))
      expect(screen.getByRole('dialog', { name: /confirm delete card/i })).toBeInTheDocument()

      fireEvent.keyDown(window, { key: 'Escape' })
      expect(screen.queryByRole('dialog', { name: /confirm delete card/i })).not.toBeInTheDocument()
    })

    it('does not render a trash icon on cards without an id', () => {
      setRowsData({
        data: [{ title: 'Anonymous', status: 'todo' }],
        loading: false,
        error: null,
      })
      render(<Kanban source="tasks" groupBy="status" columns={['todo']} />)

      expect(screen.queryByLabelText('Delete card')).not.toBeInTheDocument()
    })
  })
})
