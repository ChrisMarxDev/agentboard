import type { ReactNode, CSSProperties } from 'react'

interface StackProps {
  children: ReactNode
  gap?: number
  align?: 'start' | 'center' | 'end' | 'stretch'
}

export function Stack({ children, gap = 16, align = 'stretch' }: StackProps) {
  const style: CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    gap: `${gap}px`,
    alignItems:
      align === 'start' ? 'flex-start' :
      align === 'end' ? 'flex-end' :
      align === 'center' ? 'center' :
      'stretch',
  }

  return (
    <div className="my-4 ab-stack" style={style}>
      {children}
    </div>
  )
}
