import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DownloadPopover } from './DownloadPopover'

const downloaders = [
  { id: 'a1', name: 'spotDL' },
  { id: 'a2', name: 'Lidarr' },
]

describe('DownloadPopover', () => {
  // ── 1. renders downloaders + recommended label ─────────────────────────────
  it('renders all downloader options with the first labelled Recommended', () => {
    render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={vi.fn()}
        onClose={vi.fn()}
      />,
    )

    expect(screen.getByText('spotDL')).toBeInTheDocument()
    expect(screen.getByText('Lidarr')).toBeInTheDocument()
    expect(screen.getByText(/recommended/i)).toBeInTheDocument()
  })

  // ── 2. caption ────────────────────────────────────────────────────────────
  it('shows the "we\'ll fetch the closest match" caption', () => {
    render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={vi.fn()}
        onClose={vi.fn()}
      />,
    )
    expect(screen.getByText(/closest match/i)).toBeInTheDocument()
  })

  // ── 3. onPick called with the downloader name ─────────────────────────────
  it('clicking a downloader calls onPick with its name', () => {
    const onPick = vi.fn()
    render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={onPick}
        onClose={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /spotDL/i }))
    expect(onPick).toHaveBeenCalledWith('spotDL')

    fireEvent.click(screen.getByRole('button', { name: /Lidarr/i }))
    expect(onPick).toHaveBeenCalledWith('Lidarr')
  })

  // ── 4. Esc key calls onClose ──────────────────────────────────────────────
  it('pressing Escape calls onClose', () => {
    const onClose = vi.fn()
    render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={vi.fn()}
        onClose={onClose}
      />,
    )

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  // ── 5. backdrop click calls onClose ───────────────────────────────────────
  it('clicking the backdrop calls onClose', () => {
    const onClose = vi.fn()
    render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={vi.fn()}
        onClose={onClose}
      />,
    )

    const backdrop = screen.getByTestId('popover-backdrop')
    fireEvent.click(backdrop)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  // ── 6. popover panel click does NOT bubble to backdrop ────────────────────
  it('clicking inside the popover panel does NOT call onClose', () => {
    const onClose = vi.fn()
    render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={vi.fn()}
        onClose={onClose}
      />,
    )

    fireEvent.click(screen.getByRole('dialog'))
    expect(onClose).not.toHaveBeenCalled()
  })

  // ── 7. focus trap: focus starts on first option ───────────────────────────
  it('moves focus to the first downloader button when opened', () => {
    render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={vi.fn()}
        onClose={vi.fn()}
      />,
    )

    const firstButton = screen.getByRole('button', { name: /spotDL/i })
    expect(document.activeElement).toBe(firstButton)
  })

  // ── 8. focus trap: Tab on last focusable wraps to first ───────────────────
  it('wraps Tab from the last focusable element back to the first', () => {
    render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={vi.fn()}
        onClose={vi.fn()}
      />,
    )

    const buttons = screen.getAllByRole('button')
    const lastButton = buttons[buttons.length - 1]

    // Simulate focus on the last button then Tab
    lastButton.focus()
    expect(document.activeElement).toBe(lastButton)

    fireEvent.keyDown(document, { key: 'Tab', shiftKey: false })

    expect(document.activeElement).toBe(buttons[0])
  })

  // ── 9. focus trap: Shift+Tab on first focusable wraps to last ─────────────
  it('wraps Shift+Tab from the first focusable element to the last', () => {
    render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={vi.fn()}
        onClose={vi.fn()}
      />,
    )

    const buttons = screen.getAllByRole('button')
    const firstButton = buttons[0]
    const lastButton = buttons[buttons.length - 1]

    // Focus is already on firstButton after mount; fire Shift+Tab
    firstButton.focus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })

    expect(document.activeElement).toBe(lastButton)
  })

  // ── 10. focus restore: previously-focused element regains focus on close ──
  it('restores focus to the previously-focused element on unmount', () => {
    const trigger = document.createElement('button')
    trigger.textContent = 'Open'
    document.body.appendChild(trigger)
    trigger.focus()
    expect(document.activeElement).toBe(trigger)

    const { unmount } = render(
      <DownloadPopover
        downloaders={downloaders}
        trackTitle="Bones"
        onPick={vi.fn()}
        onClose={vi.fn()}
      />,
    )

    unmount()

    expect(document.activeElement).toBe(trigger)
    document.body.removeChild(trigger)
  })
})
