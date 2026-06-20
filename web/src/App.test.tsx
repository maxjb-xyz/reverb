import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { vi } from 'vitest'
import App from './App'

// App now gates on session status. Mock the session hook so tests don't need
// a real backend. We simulate the "authenticated" state so AppShell renders.
vi.mock('./lib/session', () => ({
  useSessionStatus: () => ({ loading: false, setupRequired: false, authenticated: true }),
}))

test('renders the Crate brand in the shell when authenticated', () => {
  render(
    <MemoryRouter initialEntries={['/search']}>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Crate')).toBeInTheDocument()
})
