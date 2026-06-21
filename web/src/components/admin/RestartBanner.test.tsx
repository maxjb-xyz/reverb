import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { RestartBanner } from './RestartBanner'

describe('RestartBanner', () => {
  it('renders when show is true', () => {
    render(<RestartBanner show={true} />)
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(screen.getByText(/Changes saved/i)).toBeInTheDocument()
    expect(screen.getByText(/restart Reverb to apply/i)).toBeInTheDocument()
  })

  it('renders nothing when show is false', () => {
    const { container } = render(<RestartBanner show={false} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('uses warning tone token classes', () => {
    render(<RestartBanner show={true} />)
    const banner = screen.getByRole('alert')
    // Should use bg-warning/10 and text-warning tokens (no raw hex)
    expect(banner.className).toMatch(/warning/)
  })
})
