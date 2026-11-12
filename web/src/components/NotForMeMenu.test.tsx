import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { Track } from '../lib/types'

const hideTrack = vi.fn()
const hideArtist = vi.fn()
vi.mock('../lib/tasteApi', () => ({
  hideTrack: (...a: unknown[]) => hideTrack(...a),
  hideArtist: (...a: unknown[]) => hideArtist(...a),
}))
const next = vi.fn()
vi.mock('../lib/playerStore', () => ({
  usePlayer: (sel: (s: { next: () => void }) => unknown) => sel({ next }),
}))
const push = vi.fn()
vi.mock('../lib/toastStore', () => ({
  useToastStore: (sel: (s: { push: typeof push }) => unknown) => sel({ push }),
}))

import { NotForMeMenu } from './NotForMeMenu'

const track: Track = {
  id: 'lib-1', title: 'Song 2', artist: 'Blur', album: 'Blur', albumId: 'al', artistId: 'ar-1',
  coverArtId: '', trackNumber: 1, discNumber: 1, durationMs: 122_000, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg',
}

function renderMenu() {
  return render(<NotForMeMenu track={track} />)
}

beforeEach(() => {
  vi.clearAllMocks()
  hideTrack.mockResolvedValue({})
  hideArtist.mockResolvedValue({})
})

describe('NotForMeMenu', () => {
  it('opens a menu with both choices', () => {
    renderMenu()
    fireEvent.click(screen.getByRole('button', { name: 'Not for me' }))
    expect(screen.getByRole('menu', { name: 'Not for me' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /Not this song/ })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /Less of Blur/ })).toBeInTheDocument()
  })

  it('"Not this song" hides the track and skips to the next one', async () => {
    renderMenu()
    fireEvent.click(screen.getByRole('button', { name: 'Not for me' }))
    fireEvent.click(screen.getByRole('menuitem', { name: /Not this song/ }))
    await waitFor(() => expect(next).toHaveBeenCalled())
    expect(hideTrack).toHaveBeenCalledWith(track)
    expect(push).toHaveBeenCalledWith(expect.stringContaining('Song 2'), 'success')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('"Less of" hides the artist and keeps playing', async () => {
    renderMenu()
    fireEvent.click(screen.getByRole('button', { name: 'Not for me' }))
    fireEvent.click(screen.getByRole('menuitem', { name: /Less of Blur/ }))
    await waitFor(() => expect(hideArtist).toHaveBeenCalledWith(track))
    expect(next).not.toHaveBeenCalled()
  })

  it('shows an error toast when saving fails', async () => {
    hideTrack.mockRejectedValue(new Error('boom'))
    renderMenu()
    fireEvent.click(screen.getByRole('button', { name: 'Not for me' }))
    fireEvent.click(screen.getByRole('menuitem', { name: /Not this song/ }))
    await waitFor(() => expect(push).toHaveBeenCalledWith(expect.any(String), 'error'))
    expect(next).not.toHaveBeenCalled()
  })

  it('closes on Escape', () => {
    renderMenu()
    fireEvent.click(screen.getByRole('button', { name: 'Not for me' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })
})
