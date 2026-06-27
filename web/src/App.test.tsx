import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { vi } from 'vitest'
import App from './App'
import * as session from './lib/session'
import type { SessionStatus } from './lib/session'
import { useAuthStore } from './lib/authStore'

vi.mock('./lib/session')
vi.mock('./lib/useAlbumPalette', () => ({ useAlbumPalette: () => null }))
vi.mock('./lib/realtimeWiring', () => ({ useRealtime: () => {} }))

// Stub heavy route components so App routing tests don't need API mocks.
vi.mock('./routes/Album', () => ({ default: () => <div>Album page</div> }))
vi.mock('./routes/Artist', () => ({ default: () => <div>Artist page</div> }))
vi.mock('./routes/SyncedPlaylist', () => ({ default: () => <div>SyncedPlaylist page</div> }))
vi.mock('./routes/Admin', () => ({ default: () => <div>Admin page</div> }))
vi.mock('./routes/Home', () => ({ default: () => <div>Home page</div> }))

function mockStatus(s: SessionStatus) {
  vi.mocked(session.useSessionStatus).mockReturnValue(s)
}

const AUTHED: SessionStatus = { loading: false, setupRequired: false, authenticated: true, error: false }

// Seed the auth store with a user holding the given capabilities, and stub
// refresh() so the boot-hydrate call is a no-op (no network in routing tests).
function seedMe(capabilities: string[]) {
  useAuthStore.setState({
    me: { id: 'u', username: 'u', roleId: 'r', roleName: 'R', isOwner: false, capabilities },
    loading: false,
    refresh: async () => {},
  })
}

beforeEach(() => {
  useAuthStore.setState({ me: null, loading: false })
})

test('authenticated renders the app shell', () => {
  mockStatus(AUTHED)
  seedMe([])
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
  mockStatus(AUTHED)
  seedMe([])
  render(
    <MemoryRouter initialEntries={['/album/abc123']}>
      <App />
    </MemoryRouter>,
  )
  // After redirect, the Album stub should render
  expect(screen.getByText('Album page')).toBeInTheDocument()
})

test('/artist/:id redirects to /artist/library/:id and renders Artist page', () => {
  mockStatus(AUTHED)
  seedMe([])
  render(
    <MemoryRouter initialEntries={['/artist/xyz456']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Artist page')).toBeInTheDocument()
})

test('/playlist/:id renders SyncedPlaylist page directly', () => {
  mockStatus(AUTHED)
  seedMe([])
  render(
    <MemoryRouter initialEntries={['/playlist/p42']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('SyncedPlaylist page')).toBeInTheDocument()
})

test('/synced-playlist/:id redirects to /playlist/:id and renders SyncedPlaylist page', () => {
  mockStatus(AUTHED)
  seedMe([])
  render(
    <MemoryRouter initialEntries={['/synced-playlist/p42']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('SyncedPlaylist page')).toBeInTheDocument()
})

test('authenticated but me still loading renders a loading state, not an ungated shell', () => {
  mockStatus(AUTHED)
  // me === null while authenticated → still hydrating
  useAuthStore.setState({ me: null, loading: true, refresh: async () => {} })
  render(
    <MemoryRouter initialEntries={['/admin']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.queryByText('Admin page')).not.toBeInTheDocument()
  expect(screen.queryByTestId('app-shell-root')).not.toBeInTheDocument()
})

test('/admin is rendered for a manager (is_admin)', () => {
  mockStatus(AUTHED)
  seedMe(['is_admin'])
  render(
    <MemoryRouter initialEntries={['/admin']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Admin page')).toBeInTheDocument()
})

test('/admin redirects a non-manager to Home', () => {
  mockStatus(AUTHED)
  seedMe(['auto_approve', 'request']) // no manager caps
  render(
    <MemoryRouter initialEntries={['/admin']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.queryByText('Admin page')).not.toBeInTheDocument()
  expect(screen.getByText('Home page')).toBeInTheDocument()
})

test('unauthenticated / resolves to the Login page (Signup "Sign in" link target)', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: false, error: false })
  render(
    <MemoryRouter initialEntries={['/']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Welcome back')).toBeInTheDocument()
})

test('unauthenticated /signup resolves to the Signup page', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: false, error: false })
  render(
    <MemoryRouter initialEntries={['/signup']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Create an account')).toBeInTheDocument()
})
