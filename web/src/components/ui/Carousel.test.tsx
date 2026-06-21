import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Carousel } from './Carousel'

describe('Carousel', () => {
  it('renders the section title as an h2', () => {
    render(<Carousel title="Jump back in"><div>Card</div></Carousel>)
    expect(screen.getByRole('heading', { level: 2, name: 'Jump back in' })).toBeInTheDocument()
  })

  it('renders children', () => {
    render(
      <Carousel title="Jump back in">
        <div>Card A</div>
        <div>Card B</div>
      </Carousel>
    )
    expect(screen.getByText('Card A')).toBeInTheDocument()
    expect(screen.getByText('Card B')).toBeInTheDocument()
  })

  it('renders "Show all" button when onShowAll is provided', () => {
    render(
      <Carousel title="Jump back in" onShowAll={vi.fn()}>
        <div>Card</div>
      </Carousel>
    )
    expect(screen.getByRole('button', { name: /show all/i })).toBeInTheDocument()
  })

  it('does not render "Show all" when onShowAll is omitted', () => {
    render(
      <Carousel title="Jump back in">
        <div>Card</div>
      </Carousel>
    )
    expect(screen.queryByRole('button', { name: /show all/i })).not.toBeInTheDocument()
  })

  it('calls onShowAll when the button is clicked', () => {
    const onShowAll = vi.fn()
    render(
      <Carousel title="Jump back in" onShowAll={onShowAll}>
        <div>Card</div>
      </Carousel>
    )
    fireEvent.click(screen.getByRole('button', { name: /show all/i }))
    expect(onShowAll).toHaveBeenCalledTimes(1)
  })

  it('scroll container has overflow-x-auto', () => {
    const { container } = render(
      <Carousel title="Jump back in">
        <div>Card</div>
      </Carousel>
    )
    const scroller = container.querySelector('[data-testid="carousel-scroll"]')
    expect(scroller?.className).toMatch(/overflow-x-auto/)
  })
})
