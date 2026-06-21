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
})
