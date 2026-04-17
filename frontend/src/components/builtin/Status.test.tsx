import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Status } from './Status'

vi.mock('../../hooks/useData', () => ({
  useData: vi.fn(),
}))

import { useData } from '../../hooks/useData'
const mockUseData = vi.mocked(useData)

describe('Status', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows loading state', () => {
    mockUseData.mockReturnValue({ data: undefined, loading: true, error: null })
    render(<Status source="test" />)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('renders running state', () => {
    mockUseData.mockReturnValue({
      data: { state: 'running', label: 'CI Pipeline', detail: 'Build #42' },
      loading: false,
      error: null,
    })
    render(<Status source="test" />)
    expect(screen.getByText('CI Pipeline')).toBeInTheDocument()
    expect(screen.getByText(/Build #42/)).toBeInTheDocument()
  })

  it('renders passing state', () => {
    mockUseData.mockReturnValue({
      data: { state: 'passing', label: 'Tests' },
      loading: false,
      error: null,
    })
    render(<Status source="test" />)
    expect(screen.getByText('Tests')).toBeInTheDocument()
  })

  it('renders null data gracefully', () => {
    mockUseData.mockReturnValue({ data: null, loading: false, error: null })
    const { container } = render(<Status source="test" />)
    expect(container.innerHTML).toBe('')
  })
})
