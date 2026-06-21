import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { PlayQueue } from './PlayQueue'
import { usePlayer } from '../lib/playerStore'
import { engine } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import type { Track } from '../lib/types'

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 1000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('PlayQueue', () => {
  beforeEach(() => {
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2'), track('3')], 0)
      useUI.getState().openPanel('queue')
    })
  })

  afterEach(() => {
    act(() => {
      useUI.getState().closePanel()
    })
  })

  it('renders the now-playing header and up-next items', () => {
    render(<PlayQueue />)
    expect(screen.getByText('Now Playing')).toBeInTheDocument()
    expect(screen.getByText('Song 1')).toBeInTheDocument()
    expect(screen.getByText('Song 2')).toBeInTheDocument()
  })

  it('remove drops a track from the queue', () => {
    render(<PlayQueue />)
    const removeButtons = screen.getAllByRole('button', { name: /remove/i })
    fireEvent.click(removeButtons[removeButtons.length - 1])
    expect(usePlayer.getState().queue.length).toBe(2)
  })

  it('is hidden when the panel is closed', () => {
    act(() => useUI.getState().closePanel())
    const { container } = render(<PlayQueue />)
    expect(container.firstChild).toBeNull()
  })

  it('drag-reorder calls moveItem with correct from/to indices', () => {
    render(<PlayQueue />)
    // queue = [1(current), 2, 3]; up-next rows: index 1 ('Song 2'), index 2 ('Song 3')
    const rows = screen.getAllByRole('listitem').filter((li) => li.draggable)
    const src = rows[0]  // full-queue index 1 (Song 2)
    const tgt = rows[1]  // full-queue index 2 (Song 3)
    fireEvent.dragStart(src)
    fireEvent.dragOver(tgt)
    fireEvent.drop(tgt)
    // After drop: Song 2 should have moved from index 1 to index 2
    const ids = usePlayer.getState().queue.map((t) => t.id)
    expect(ids).toEqual(['1', '3', '2'])
  })

  it('clicking a queue row jumps playback to that index', () => {
    render(<PlayQueue />)
    // up-next rows: index 1 ('Song 2'), index 2 ('Song 3')
    const rows = screen.getAllByRole('listitem').filter((li) => li.draggable)
    // Click second up-next row (full-queue index 2, Song 3)
    fireEvent.click(rows[1])
    expect(usePlayer.getState().index).toBe(2)
    expect(usePlayer.getState().current?.id).toBe('3')
  })

  it('clicking the remove button does NOT trigger the row jump', () => {
    render(<PlayQueue />)
    const initialIndex = usePlayer.getState().index // 0
    const removeButtons = screen.getAllByRole('button', { name: /remove/i })
    // Remove the first up-next item (Song 2 at index 1) — should not jump
    fireEvent.click(removeButtons[0])
    // Index stays at 0 (current track unchanged), queue shrinks
    expect(usePlayer.getState().index).toBe(initialIndex)
    expect(usePlayer.getState().queue.length).toBe(2)
  })

  it('playAt engine no-ops on out-of-range indices', () => {
    const before = usePlayer.getState().index
    act(() => { engine.playAt(99) })
    expect(usePlayer.getState().index).toBe(before)
    act(() => { engine.playAt(-1) })
    expect(usePlayer.getState().index).toBe(before)
  })

  it('is a full-screen sheet on mobile and a side panel on desktop (responsive classes)', () => {
    render(<PlayQueue />)
    const aside = screen.getByRole('complementary')
    expect(aside.className).toMatch(/inset-0/)
    expect(aside.className).toMatch(/md:w-80/)
  })
})
