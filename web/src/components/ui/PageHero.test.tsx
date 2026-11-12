import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup } from '@testing-library/react'
import { PageHero, StickyHeroBar, STICKY_SLOT_ID } from './PageHero'

function withSlot(ui: React.ReactNode) {
  const slot = document.createElement('div')
  slot.id = STICKY_SLOT_ID
  document.body.appendChild(slot)
  const r = render(<>{ui}</>)
  return { ...r, slot }
}

afterEach(() => {
  cleanup()
  document.getElementById(STICKY_SLOT_ID)?.remove()
})

describe('PageHero', () => {
  it('washes the band with the cover colour', () => {
    render(<PageHero rgb={[10, 20, 30]}><h1>Title</h1></PageHero>)
    expect(screen.getByTestId('page-hero').style.background).toMatch(/10, 20, 30/)
  })
})

describe('StickyHeroBar', () => {
  it('renders into the sticky slot', () => {
    const { slot } = withSlot(<StickyHeroBar visible title="Kid A" onPlay={() => {}} />)
    expect(slot.querySelector('[data-testid="sticky-hero-bar"]')).toBeTruthy()
  })

  it('is hidden from assistive tech and not focusable until visible', () => {
    withSlot(<StickyHeroBar visible={false} title="Kid A" onPlay={() => {}} />)
    expect(screen.queryByRole('button', { name: 'Play Kid A' })).toBeNull()
    expect(screen.getByTestId('sticky-hero-bar').getAttribute('aria-hidden')).toBe('true')
  })

  it('plays when its button is pressed', () => {
    const onPlay = vi.fn()
    withSlot(<StickyHeroBar visible title="Kid A" onPlay={onPlay} />)
    fireEvent.click(screen.getByRole('button', { name: 'Play Kid A' }))
    expect(onPlay).toHaveBeenCalled()
  })

  it('renders nothing when the shell has no slot', () => {
    render(<StickyHeroBar visible title="Kid A" />)
    expect(screen.queryByTestId('sticky-hero-bar')).toBeNull()
  })
})
