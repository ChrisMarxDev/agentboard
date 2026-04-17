import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
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
})
