import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from './AppShell'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'

// Stub the WS wiring so the shell mounts without a real socket.
vi.mock('../lib/realtimeWiring', () => ({ useRealtime: () => {} }))

function renderShell() {
  const qc = new QueryClient()
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AppShell', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
    useUI.setState({ rightPanel: 'downloads' })
  })

  it('mounts the Download Tray when the right panel is downloads', () => {
    renderShell()
    expect(screen.getByText('Download Tray')).toBeInTheDocument()
  })
})
