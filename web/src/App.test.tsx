import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { vi } from 'vitest'
import App from './App'
import * as session from './lib/session'
import type { SessionStatus } from './lib/session'

vi.mock('./lib/session')
vi.mock('./lib/useAlbumPalette', () => ({ useAlbumPalette: () => null }))
vi.mock('./lib/realtimeWiring', () => ({ useRealtime: () => {} }))

// Stub heavy route components so App routing tests don't need API mocks.
vi.mock('./routes/Album', () => ({ default: () => <div>Album page</div> }))
vi.mock('./routes/Artist', () => ({ default: () => <div>Artist page</div> }))
vi.mock('./routes/SyncedPlaylist', () => ({ default: () => <div>SyncedPlaylist page</div> }))

function mockStatus(s: SessionStatus) {
  vi.mocked(session.useSessionStatus).mockReturnValue(s)
}

test('authenticated renders the app shell', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: true, error: false })
  render(
    <MemoryRouter initialEntries={['/search']}>
      <App />
    </MemoryRouter>,
  )
  // AppShell is present (Sidebar removed; shell identified by its testid)
  expect(screen.getByTestId('app-shell-root')).toBeInTheDocument()
})

test('setupRequired renders the setup page', () => {
  mockStatus({ loading: false, setupRequired: true, authenticated: false, error: false })
  render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Welcome to Reverb')).toBeInTheDocument()
})

test('unauthenticated renders the login page', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: false, error: false })
  render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Welcome back')).toBeInTheDocument()
})

test('error renders the server error state with retry button', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: false, error: true })
  render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText(/can't reach the reverb server/i)).toBeInTheDocument()
  expect(screen.getByText('Retry')).toBeInTheDocument()
})

test('/album/:id redirects to /album/library/:id and renders Album page', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: true, error: false })
  render(
    <MemoryRouter initialEntries={['/album/abc123']}>
      <App />
    </MemoryRouter>,
  )
  // After redirect, the Album stub should render
  expect(screen.getByText('Album page')).toBeInTheDocument()
})

test('/artist/:id redirects to /artist/library/:id and renders Artist page', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: true, error: false })
  render(
    <MemoryRouter initialEntries={['/artist/xyz456']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Artist page')).toBeInTheDocument()
})

test('/playlist/:id renders SyncedPlaylist page directly', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: true, error: false })
  render(
    <MemoryRouter initialEntries={['/playlist/p42']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('SyncedPlaylist page')).toBeInTheDocument()
})

test('/synced-playlist/:id redirects to /playlist/:id and renders SyncedPlaylist page', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: true, error: false })
  render(
    <MemoryRouter initialEntries={['/synced-playlist/p42']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('SyncedPlaylist page')).toBeInTheDocument()
})
