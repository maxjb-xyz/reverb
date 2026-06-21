import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { vi } from 'vitest'
import App from './App'
import * as session from './lib/session'
import type { SessionStatus } from './lib/session'

vi.mock('./lib/session')
vi.mock('./lib/useAlbumPalette', () => ({ useAlbumPalette: () => null }))
vi.mock('./lib/realtimeWiring', () => ({ useRealtime: () => {} }))

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
  expect(screen.getByText('Log in to Reverb')).toBeInTheDocument()
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
