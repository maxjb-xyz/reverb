import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ConfirmDialog } from './ConfirmDialog'

describe('ConfirmDialog', () => {
  const base = { title: 'Remove this?', confirmLabel: 'Remove', onConfirm: () => {}, onCancel: () => {} }

  it('renders nothing while closed', () => {
    render(<ConfirmDialog {...base} open={false} />)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('names the dialog after its title and focuses the confirm button', () => {
    render(<ConfirmDialog {...base} open body="It cannot be undone." />)
    expect(screen.getByRole('alertdialog', { name: 'Remove this?' })).toBeInTheDocument()
    expect(screen.getByText('It cannot be undone.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove' })).toHaveFocus()
  })

  it('confirm and cancel call their handlers; Escape cancels', () => {
    const onConfirm = vi.fn()
    const onCancel = vi.fn()
    render(<ConfirmDialog {...base} open onConfirm={onConfirm} onCancel={onCancel} />)
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledTimes(2)
  })
})
