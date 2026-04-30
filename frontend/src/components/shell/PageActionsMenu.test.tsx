import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import PageActionsMenu from './PageActionsMenu'

function Harness({ pagePath, pageTitle }: { pagePath: string; pageTitle?: string }) {
  return (
    <MemoryRouter initialEntries={[`/${pagePath === 'index' ? '' : pagePath}`]}>
      <Routes>
        <Route
          path="*"
          element={
            <div>
              <PageActionsMenu pagePath={pagePath} pageTitle={pageTitle} />
              <div data-testid="landed">landed</div>
            </div>
          }
        />
      </Routes>
    </MemoryRouter>
  )
}

describe('PageActionsMenu', () => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, text: async () => '' }) as unknown as typeof fetch
  })
  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('renders the 3-dot button on non-index pages', () => {
    render(<Harness pagePath="features/kanban" />)
    expect(screen.getByLabelText('Page actions')).toBeInTheDocument()
  })

  it('renders the 3-dot button on the index page (Export still applies)', () => {
    render(<Harness pagePath="index" />)
    expect(screen.getByLabelText('Page actions')).toBeInTheDocument()
  })

  it('shows Export but not Delete on the index page', () => {
    render(<Harness pagePath="index" />)
    fireEvent.click(screen.getByLabelText('Page actions'))
    expect(screen.getByText('Export page')).toBeInTheDocument()
    expect(screen.queryByText('Delete page')).not.toBeInTheDocument()
  })

  it('opens and closes the menu when the button is clicked', () => {
    render(<Harness pagePath="features/kanban" />)
    const btn = screen.getByLabelText('Page actions')

    fireEvent.click(btn)
    expect(screen.getByRole('menu')).toBeInTheDocument()
    expect(screen.getByText('Delete page')).toBeInTheDocument()

    fireEvent.click(btn)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('closes the menu on Escape', () => {
    render(<Harness pagePath="features/kanban" />)
    fireEvent.click(screen.getByLabelText('Page actions'))
    expect(screen.getByRole('menu')).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('closes the menu on outside click', () => {
    const { container } = render(<Harness pagePath="features/kanban" />)
    fireEvent.click(screen.getByLabelText('Page actions'))
    expect(screen.getByRole('menu')).toBeInTheDocument()
    // Click something outside the menu root.
    fireEvent.mouseDown(container)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('shows confirm dialog when Delete is clicked', () => {
    render(<Harness pagePath="features/kanban" pageTitle="Kanban" />)
    fireEvent.click(screen.getByLabelText('Page actions'))
    fireEvent.click(screen.getByText('Delete page'))

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('Delete page?')
    expect(dialog).toHaveTextContent('/features/kanban')
    expect(dialog).toHaveTextContent('Kanban')
    // Menu closes when the dialog opens.
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('cancel closes the dialog without deleting', () => {
    render(<Harness pagePath="features/kanban" />)
    fireEvent.click(screen.getByLabelText('Page actions'))
    fireEvent.click(screen.getByText('Delete page'))
    fireEvent.click(screen.getByText('Cancel'))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(globalThis.fetch).not.toHaveBeenCalled()
  })

  it('confirm fires DELETE against /api/{path} and navigates home', async () => {
    render(<Harness pagePath="features/kanban" />)
    fireEvent.click(screen.getByLabelText('Page actions'))
    fireEvent.click(screen.getByText('Delete page'))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1))
    const [url, init] = (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0]
    expect(url).toBe('/api/features/kanban')
    expect(init.method).toBe('DELETE')
  })

  describe('export', () => {
    let createObjectURL: ReturnType<typeof vi.fn>
    let revokeObjectURL: ReturnType<typeof vi.fn>
    let originalCreate: typeof URL.createObjectURL
    let originalRevoke: typeof URL.revokeObjectURL

    beforeEach(() => {
      originalCreate = URL.createObjectURL
      originalRevoke = URL.revokeObjectURL
      createObjectURL = vi.fn(() => 'blob:fake')
      revokeObjectURL = vi.fn()
      URL.createObjectURL = createObjectURL as unknown as typeof URL.createObjectURL
      URL.revokeObjectURL = revokeObjectURL as unknown as typeof URL.revokeObjectURL
    })
    afterEach(() => {
      URL.createObjectURL = originalCreate
      URL.revokeObjectURL = originalRevoke
    })

    it('fetches raw MDX with text/markdown Accept header', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        text: async () => '# hello',
      }) as unknown as typeof fetch

      render(<Harness pagePath="features/kanban" />)
      fireEvent.click(screen.getByLabelText('Page actions'))
      fireEvent.click(screen.getByText('Export page'))

      await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1))
      const [url, init] = (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0]
      expect(url).toBe('/api/features/kanban')
      // apiFetch normalises headers via `new Headers(...)`, so the init
      // carries a Headers instance rather than the plain object we passed.
      const headers = init.headers as Headers
      expect(headers.get('Accept')).toBe('text/markdown')
    })

    it('triggers a download with filename = last path segment + .md', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({
        ok: true,
        text: async () => '# hello',
      }) as unknown as typeof fetch

      // Spy on <a>.click so we can verify the download attribute.
      const clickSpy = vi.fn()
      const originalCreateElement = document.createElement.bind(document)
      const elSpy = vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
        const el = originalCreateElement(tag) as HTMLElement
        if (tag === 'a') {
          ;(el as HTMLAnchorElement).click = clickSpy
        }
        return el
      })

      render(<Harness pagePath="features/components/kanban" />)
      fireEvent.click(screen.getByLabelText('Page actions'))
      fireEvent.click(screen.getByText('Export page'))

      await waitFor(() => expect(clickSpy).toHaveBeenCalledTimes(1))
      const anchor = (elSpy.mock.results.find(r => (r.value as HTMLElement).tagName === 'A')?.value) as HTMLAnchorElement
      expect(anchor.download).toBe('kanban.md')
      expect(createObjectURL).toHaveBeenCalledTimes(1)
      expect(revokeObjectURL).toHaveBeenCalledTimes(1)

      elSpy.mockRestore()
    })

    it('closes the menu after export is clicked', async () => {
      globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, text: async () => 'x' }) as unknown as typeof fetch
      render(<Harness pagePath="features/kanban" />)
      fireEvent.click(screen.getByLabelText('Page actions'))
      fireEvent.click(screen.getByText('Export page'))
      expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    })
  })

  it('shows the server error message when delete fails', async () => {
    globalThis.fetch = vi
      .fn()
      .mockResolvedValue({ ok: false, status: 500, text: async () => 'disk full' }) as unknown as typeof fetch

    render(<Harness pagePath="features/kanban" />)
    fireEvent.click(screen.getByLabelText('Page actions'))
    fireEvent.click(screen.getByText('Delete page'))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(screen.getByRole('dialog')).toHaveTextContent('disk full'))
    // Dialog stays open so the user can retry or cancel.
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })
})
