import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { MobileTabNav } from './MobileTabNav'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'

function renderNav() {
  return render(
    <MemoryRouter initialEntries={['/search']}>
      <MobileTabNav />
    </MemoryRouter>,
  )
}

describe('MobileTabNav', () => {
  beforeEach(() => {
    useUI.setState({ rightPanel: null })
    useDownloads.setState({ jobs: {} })
  })

  it('renders Search, Library, and Settings tabs', () => {
    renderNav()
    expect(screen.getByRole('link', { name: /search/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /library/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /settings/i })).toBeInTheDocument()
  })

  it('includes a dedicated Search tab pointing at /search', () => {
    renderNav()
    expect(screen.getByRole('link', { name: /search/i })).toHaveAttribute('href', '/search')
  })

  it('the Downloads tab toggles the downloads panel', () => {
    renderNav()
    fireEvent.click(screen.getByRole('button', { name: /downloads/i }))
    expect(useUI.getState().rightPanel).toBe('downloads')
  })

  it('tap targets are at least 44px', () => {
    renderNav()
    const search = screen.getByRole('link', { name: /search/i })
    expect(search.className).toMatch(/min-h-\[44px\]/)
  })
})
