import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Metric } from './Metric'

// Mock useData hook
vi.mock('../../hooks/useData', () => ({
  useData: vi.fn(),
}))

import { useData } from '../../hooks/useData'

const mockUseData = vi.mocked(useData)

describe('Metric', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows loading state', () => {
    mockUseData.mockReturnValue({ data: undefined, loading: true, error: null })
    render(<Metric source="test" />)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('displays a plain number', () => {
    mockUseData.mockReturnValue({ data: 42, loading: false, error: null })
    render(<Metric source="test" />)
    expect(screen.getByText('42')).toBeInTheDocument()
  })

  it('displays a number with label', () => {
    mockUseData.mockReturnValue({ data: 1420, loading: false, error: null })
    render(<Metric source="test" label="Daily Active Users" />)
    expect(screen.getByText('1,420')).toBeInTheDocument()
    expect(screen.getByText('Daily Active Users')).toBeInTheDocument()
  })

  it('displays an object with value and label', () => {
    mockUseData.mockReturnValue({
      data: { value: 99, label: 'Score', trend: 5, comparison: 'last week' },
      loading: false,
      error: null,
    })
    render(<Metric source="test" />)
    expect(screen.getByText('99')).toBeInTheDocument()
    expect(screen.getByText('Score')).toBeInTheDocument()
  })

  it('formats currency', () => {
    mockUseData.mockReturnValue({ data: 1234.5, loading: false, error: null })
    render(<Metric source="test" format="currency" />)
    expect(screen.getByText('$1,234.50')).toBeInTheDocument()
  })

  it('formats percent', () => {
    mockUseData.mockReturnValue({ data: 75, loading: false, error: null })
    render(<Metric source="test" format="percent" />)
    expect(screen.getByText('75.0%')).toBeInTheDocument()
  })

  it('shows dash for null value', () => {
    mockUseData.mockReturnValue({ data: null, loading: false, error: null })
    render(<Metric source="test" />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('overrides data label with prop label', () => {
    mockUseData.mockReturnValue({
      data: { value: 10, label: 'From Data' },
      loading: false,
      error: null,
    })
    render(<Metric source="test" label="From Prop" />)
    expect(screen.getByText('From Prop')).toBeInTheDocument()
    expect(screen.queryByText('From Data')).not.toBeInTheDocument()
  })
})
