import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { Skeleton } from './Skeleton'

describe('Skeleton', () => {
  it('renders a div with bg-raised class', () => {
    const { container } = render(<Skeleton />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/bg-raised/)
  })

  it('has animate-pulse class for the loading animation', () => {
    const { container } = render(<Skeleton />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/animate-pulse/)
  })

  it('accepts a custom className', () => {
    const { container } = render(<Skeleton className="w-12 h-12" />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/w-12/)
    expect(el.className).toMatch(/h-12/)
  })

  it('applies rounded-full when rounded="full"', () => {
    const { container } = render(<Skeleton rounded="full" />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/rounded-full/)
  })

  it('applies rounded-md by default', () => {
    const { container } = render(<Skeleton />)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/rounded-md/)
  })
})
