import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Toaster } from './Toaster'
import { useToastStore } from '../../lib/toastStore'

describe('Toaster', () => {
  beforeEach(() => {
    useToastStore.setState({ toasts: [] })
  })

  it('renders the aria-live region even when there are zero toasts', () => {
    render(<Toaster />)
    // The live region must be in the DOM before the first toast for screen
    // readers to announce it — it should exist even when toasts is empty.
    const region = screen.getByLabelText('Notifications')
    expect(region).toBeInTheDocument()
    expect(region).toHaveAttribute('aria-live', 'polite')
    expect(screen.queryByTestId('toast')).not.toBeInTheDocument()
  })

  it('renders toasts inside the live region when toasts are present', () => {
    useToastStore.getState().push('Something went wrong', 'error')
    render(<Toaster />)

    const region = screen.getByLabelText('Notifications')
    expect(region).toBeInTheDocument()
    expect(region).toHaveAttribute('aria-live', 'polite')
    expect(screen.getByTestId('toast')).toBeInTheDocument()
    expect(screen.getByText('Something went wrong')).toBeInTheDocument()
  })

  it('renders multiple toasts inside the single live region', () => {
    useToastStore.getState().push('First toast', 'info')
    useToastStore.getState().push('Second toast', 'success')
    render(<Toaster />)

    const toasts = screen.getAllByTestId('toast')
    expect(toasts).toHaveLength(2)
  })
})
