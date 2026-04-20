import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useKeyboardShortcuts } from './useKeyboardShortcuts'

function press(key: string, modifiers: Partial<KeyboardEventInit> = {}) {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...modifiers })
  window.dispatchEvent(event)
  return event
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('useKeyboardShortcuts', () => {
  it('fires the handler for a mapped key', () => {
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    press('b')
    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('ignores unmapped keys', () => {
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    press('z')
    expect(handler).not.toHaveBeenCalled()
  })

  it('ignores keys when Ctrl is held', () => {
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    press('b', { ctrlKey: true })
    expect(handler).not.toHaveBeenCalled()
  })

  it('ignores keys when Meta is held', () => {
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    press('b', { metaKey: true })
    expect(handler).not.toHaveBeenCalled()
  })

  it('ignores keys when Alt is held', () => {
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    press('b', { altKey: true })
    expect(handler).not.toHaveBeenCalled()
  })

  it('ignores keys while focus is in an input', () => {
    const input = document.createElement('input')
    document.body.appendChild(input)
    input.focus()
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    press('b')
    expect(handler).not.toHaveBeenCalled()
  })

  it('ignores keys while focus is in a textarea', () => {
    const ta = document.createElement('textarea')
    document.body.appendChild(ta)
    ta.focus()
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    press('b')
    expect(handler).not.toHaveBeenCalled()
  })

  it('ignores keys while focus is in a contenteditable', () => {
    const div = document.createElement('div')
    div.setAttribute('contenteditable', 'true')
    document.body.appendChild(div)
    div.focus()
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    press('b')
    expect(handler).not.toHaveBeenCalled()
  })

  it('does nothing when disabled', () => {
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }, false))
    press('b')
    expect(handler).not.toHaveBeenCalled()
  })

  it('dispatches distinct keys to distinct handlers', () => {
    const b = vi.fn()
    const j = vi.fn()
    const one = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b, j, '1': one }))
    press('b')
    press('j')
    press('1')
    expect(b).toHaveBeenCalledTimes(1)
    expect(j).toHaveBeenCalledTimes(1)
    expect(one).toHaveBeenCalledTimes(1)
  })

  it('calls preventDefault on matched keys', () => {
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    const event = press('b')
    expect(event.defaultPrevented).toBe(true)
  })

  it('does not preventDefault on unmatched keys', () => {
    const handler = vi.fn()
    renderHook(() => useKeyboardShortcuts({ b: handler }))
    const event = press('z')
    expect(event.defaultPrevented).toBe(false)
  })

  it('picks up new handlers without re-subscribing', () => {
    const h1 = vi.fn()
    const h2 = vi.fn()
    const { rerender } = renderHook(({ map }) => useKeyboardShortcuts(map), {
      initialProps: { map: { b: h1 } as Record<string, () => void> },
    })
    press('b')
    rerender({ map: { b: h2 } })
    press('b')
    expect(h1).toHaveBeenCalledTimes(1)
    expect(h2).toHaveBeenCalledTimes(1)
  })
})
