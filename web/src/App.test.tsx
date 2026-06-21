import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { vi } from 'vitest'
import App from './App'
import * as session from './lib/session'
import type { SessionStatus } from './lib/session'

vi.mock('./lib/session')
vi.mock('./lib/useAlbumPalette', () => ({ useAlbumPalette: () => null }))

function mockStatus(s: SessionStatus) {
  vi.mocked(session.useSessionStatus).mockReturnValue(s)
}

test('authenticated renders the app shell', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: true })
  render(
    <MemoryRouter initialEntries={['/search']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Reverb')).toBeInTheDocument()
})

test('setupRequired renders the setup page', () => {
  mockStatus({ loading: false, setupRequired: true, authenticated: false })
  render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Welcome to Reverb')).toBeInTheDocument()
})

test('unauthenticated renders the login page', () => {
  mockStatus({ loading: false, setupRequired: false, authenticated: false })
  render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Log in to Reverb')).toBeInTheDocument()
})
