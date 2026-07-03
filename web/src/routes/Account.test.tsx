/**
 * Account route — now just a redirect to /settings.
 * Kept as a separate test file to explicitly guard the redirect behavior.
 */
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import Account from './Account'

function renderAt(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/account" element={<Account />} />
        <Route path="/settings" element={<div>Settings page sentinel</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('Account route redirect', () => {
  it('redirects /account to /settings', () => {
    renderAt('/account')
    expect(screen.getByText('Settings page sentinel')).toBeInTheDocument()
  })

  it('does not render any Account page content at /account', () => {
    renderAt('/account')
    // No stale account heading
    expect(screen.queryByRole('heading', { name: /^account$/i })).not.toBeInTheDocument()
  })
})
