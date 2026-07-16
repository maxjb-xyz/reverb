import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { CommandPalette } from './CommandPalette'
import { useAuthStore } from '../lib/authStore'
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { useSearch } from '../lib/searchStore'

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

function renderPalette() {
  return render(
    <MemoryRouter>
      <CommandPalette />
    </MemoryRouter>,
  )
}

describe('CommandPalette', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    useAuthStore.setState({ me: null, loading: false })
    usePlayer.setState({ current: null })
    useUI.setState({ rightPanel: null })
    useSearch.setState({ query: '' })
  })

  it('closes by default and opens on Cmd+K / Ctrl+K keydown', () => {
    renderPalette()
    // Should not render dialog initially
    expect(screen.queryByRole('dialog', { name: /command palette/i })).not.toBeInTheDocument()

    // Press Cmd+K
    fireEvent.keyDown(window, { key: 'k', metaKey: true })

    // Dialog should be visible with "Go to Home" command
    expect(screen.getByRole('dialog', { name: /command palette/i })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /go to home/i })).toBeInTheDocument()
  })

  it('filters commands on typing', () => {
    renderPalette()
    fireEvent.keyDown(window, { key: 'k', metaKey: true })

    const input = screen.getByRole('textbox', { name: /type a command/i })
    fireEvent.change(input, { target: { value: 'stats' } })

    // "Stats" should be visible
    expect(screen.getByRole('option', { name: /stats/i })).toBeInTheDocument()
    // "Go to Home" should not match
    expect(screen.queryByRole('option', { name: /go to home/i })).not.toBeInTheDocument()
  })

  it('closes on Escape key', () => {
    renderPalette()
    fireEvent.keyDown(window, { key: 'k', metaKey: true })
    expect(screen.getByRole('dialog', { name: /command palette/i })).toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: /command palette/i })).not.toBeInTheDocument()
  })

  it('shows fallthrough search row for non-matching query', () => {
    renderPalette()
    fireEvent.keyDown(window, { key: 'k', metaKey: true })

    const input = screen.getByRole('textbox', { name: /type a command/i })
    fireEvent.change(input, { target: { value: 'garbage query xyzabc' } })

    // Fallthrough row should appear
    expect(screen.getByRole('button', { name: /search for.*garbage query xyzabc/i })).toBeInTheDocument()
  })
})
