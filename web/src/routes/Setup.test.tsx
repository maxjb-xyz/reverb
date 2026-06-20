import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Setup from './Setup'

test('setup page prompts for an admin password', () => {
  render(
    <MemoryRouter>
      <Setup />
    </MemoryRouter>,
  )
  expect(screen.getByText('Welcome to Crate')).toBeInTheDocument()
  expect(screen.getByPlaceholderText('Choose a password')).toBeInTheDocument()
})
