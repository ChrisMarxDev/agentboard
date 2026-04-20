import type { ReactNode, CSSProperties } from 'react'

interface DeckProps {
  children: ReactNode
  min?: number
  gap?: number
  columns?: number
}

export function Deck({ children, min = 280, gap = 16, columns }: DeckProps) {
  const style: CSSProperties = {
    display: 'grid',
    gap: `${gap}px`,
    gridTemplateColumns: columns
      ? `repeat(${columns}, minmax(0, 1fr))`
      : `repeat(auto-fit, minmax(${min}px, 1fr))`,
  }

  return (
    <div className="my-4 ab-deck" style={style}>
      {children}
    </div>
  )
}
