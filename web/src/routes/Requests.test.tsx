import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useAuthStore } from '../lib/authStore'
import { useRequestStore } from '../lib/requestApi'
import type { Request as MusicRequest } from '../lib/requestApi'
import { useToastStore } from '../lib/toastStore'
import Requests from './Requests'
import { TopBar } from '../components/shell/TopBar'

// ---- Module mocks -------------------------------------------------------

vi.mock('../lib/requestApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/requestApi')>()
  return {
    ...actual,
    getMyRequests: vi.fn().mockResolvedValue([]),
    getAllRequests: vi.fn().mockResolvedValue([]),
    approveRequest: vi.fn().mockResolvedValue({ id: 'r1', status: 'approved' }),
    denyRequest: vi.fn().mockResolvedValue({ id: 'r1', status: 'denied' }),
    cancelRequest: vi.fn().mockResolvedValue({ id: 'r1', status: 'canceled' }),
  }
})

// TopBar calls useNavigate — stub it
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

// Stub fetch globally — TopBar's logout calls fetch
global.fetch = vi.fn().mockResolvedValue({ ok: true, text: async () => '' })

// ---- Helpers ------------------------------------------------------------

function makeRequest(overrides: Partial<MusicRequest> = {}): MusicRequest {
  return {
    id: 'r1',
    requestedBy: 'u1',
    source: 'spotify',
    externalId: 'ext1',
    title: 'Test Song',
    artist: 'Test Artist',
    status: 'pending',
    createdAt: 1700000000,
    ...overrides,
  }
}

function setMe(id: string, capabilities: string[]) {
  useAuthStore.setState({
    me: { id, username: 'user', roleId: 'r', roleName: 'R', isOwner: false, capabilities, createdAt: 1700000000 },
    loading: false,
  })
}

function renderRequests() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Requests />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function renderTopBar() {
  return render(
    <MemoryRouter>
      <TopBar />
    </MemoryRouter>,
  )
}

// ---- Tests --------------------------------------------------------------

describe('Requests page — My Requests tab', () => {
  beforeEach(() => {
    useRequestStore.setState({ byId: {} })
    useAuthStore.setState({ me: null, loading: false })
    mockNavigate.mockReset()
  })

  it('shows "My Requests" heading', () => {
    setMe('u1', ['request'])
    renderRequests()
    expect(screen.getByRole('heading', { name: /my requests/i })).toBeInTheDocument()
  })

  it('renders a status chip for a pending request', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({ byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending' }) } })
    renderRequests()
    await waitFor(() => {
      expect(screen.getByText('Test Song')).toBeInTheDocument()
    })
    expect(screen.getByText('Pending')).toBeInTheDocument()
  })

  it('renders "Added" chip for a fulfilled request', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({ byId: { r1: makeRequest({ requestedBy: 'u1', status: 'fulfilled' }) } })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    expect(screen.getByText('Added')).toBeInTheDocument()
  })

  it('renders "Denied" chip for a denied request', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({ byId: { r1: makeRequest({ requestedBy: 'u1', status: 'denied' }) } })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    expect(screen.getByText('Denied')).toBeInTheDocument()
  })

  it('shows Cancel button for a pending request belonging to the current user', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({ byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending' }) } })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
  })

  it('calls cancelRequest when Cancel is clicked', async () => {
    const { cancelRequest } = await import('../lib/requestApi')
    setMe('u1', ['request'])
    useRequestStore.setState({ byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending' }) } })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    await waitFor(() => {
      expect(cancelRequest).toHaveBeenCalledWith('r1')
    })
  })

  it('does NOT show the Approval tab for a user with only "request" capability', () => {
    setMe('u1', ['request'])
    renderRequests()
    expect(screen.queryByRole('tab', { name: /approval/i })).not.toBeInTheDocument()
  })
})

describe('Requests page — Approval tab (manager)', () => {
  beforeEach(() => {
    useRequestStore.setState({ byId: {} })
    useAuthStore.setState({ me: null, loading: false })
    mockNavigate.mockReset()
  })

  it('shows an "Approval" tab for a user with manage_requests capability', () => {
    setMe('u2', ['request', 'manage_requests'])
    renderRequests()
    expect(screen.getByRole('tab', { name: /approval/i })).toBeInTheDocument()
  })

  it('Approval tab lists pending requests with Approve and Deny buttons', async () => {
    setMe('u2', ['request', 'manage_requests'])
    const pending = makeRequest({ id: 'r2', requestedBy: 'u3', status: 'pending', title: 'Queue Track' })
    useRequestStore.setState({ byId: { r2: pending } })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByText('Queue Track')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: /approve/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /deny/i })).toBeInTheDocument()
  })

  it('calls approveRequest when Approve button is clicked', async () => {
    const { approveRequest } = await import('../lib/requestApi')
    setMe('u2', ['request', 'manage_requests'])
    const pending = makeRequest({ id: 'r2', requestedBy: 'u3', status: 'pending', title: 'Queue Track' })
    useRequestStore.setState({ byId: { r2: pending } })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /approve/i })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /approve/i }))
    await waitFor(() => {
      expect(approveRequest).toHaveBeenCalledWith('r2')
    })
  })

  it('calls denyRequest when Deny button is clicked', async () => {
    const { denyRequest } = await import('../lib/requestApi')
    setMe('u2', ['request', 'manage_requests'])
    const pending = makeRequest({ id: 'r2', requestedBy: 'u3', status: 'pending', title: 'Queue Track' })
    useRequestStore.setState({ byId: { r2: pending } })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /deny/i })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /deny/i }))
    // Clicking Deny first opens the reason input + Confirm button
    await waitFor(() => expect(screen.getByRole('button', { name: /confirm/i })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))
    await waitFor(() => {
      expect(denyRequest).toHaveBeenCalledWith('r2', undefined)
    })
  })
})

describe('Requests page — action failure toasts', () => {
  beforeEach(() => {
    useRequestStore.setState({ byId: {} })
    useAuthStore.setState({ me: null, loading: false })
    useToastStore.setState({ toasts: [] })
    mockNavigate.mockReset()
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('shows an error toast when cancelRequest fails', async () => {
    const { cancelRequest } = await import('../lib/requestApi')
    vi.mocked(cancelRequest).mockRejectedValueOnce(new Error('boom'))
    setMe('u1', ['request'])
    useRequestStore.setState({ byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending' }) } })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    await waitFor(() => {
      expect(useToastStore.getState().toasts.some((t) => t.kind === 'error')).toBe(true)
    })
  })

  it('shows an error toast when approveRequest fails', async () => {
    const { approveRequest } = await import('../lib/requestApi')
    vi.mocked(approveRequest).mockRejectedValueOnce(new Error('boom'))
    setMe('u2', ['request', 'manage_requests'])
    useRequestStore.setState({ byId: { r2: makeRequest({ id: 'r2', requestedBy: 'u3', status: 'pending', title: 'Queue Track' }) } })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /approve/i })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /approve/i }))
    await waitFor(() => {
      expect(useToastStore.getState().toasts.some((t) => t.kind === 'error')).toBe(true)
    })
  })

  it('shows an error toast when denyRequest fails', async () => {
    const { denyRequest } = await import('../lib/requestApi')
    vi.mocked(denyRequest).mockRejectedValueOnce(new Error('boom'))
    setMe('u2', ['request', 'manage_requests'])
    useRequestStore.setState({ byId: { r2: makeRequest({ id: 'r2', requestedBy: 'u3', status: 'pending', title: 'Queue Track' }) } })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /deny/i })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /deny/i }))
    await waitFor(() => expect(screen.getByRole('button', { name: /confirm/i })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))
    await waitFor(() => {
      expect(useToastStore.getState().toasts.some((t) => t.kind === 'error')).toBe(true)
    })
  })
})

describe('Requests page — cover art rendering', () => {
  beforeEach(() => {
    useRequestStore.setState({ byId: {} })
    useAuthStore.setState({ me: null, loading: false })
    mockNavigate.mockReset()
  })

  it('My Requests row renders an img with the coverUrl when a request has a coverUrl', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({
      byId: {
        r1: makeRequest({ requestedBy: 'u1', status: 'pending', coverUrl: 'https://i.scdn.co/image/abc123' }),
      },
    })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    const img = screen.getAllByRole('img').find((el) => el.getAttribute('src') === 'https://i.scdn.co/image/abc123')
    expect(img).toBeDefined()
  })

  it('My Requests row builds /api/v1/cover/... URL when only coverArtId is present', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({
      byId: {
        r1: makeRequest({ requestedBy: 'u1', status: 'pending', coverArtId: 'lib-id-42' }),
      },
    })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    const img = screen.getAllByRole('img').find((el) =>
      (el.getAttribute('src') ?? '').includes('/api/v1/cover/lib-id-42'),
    )
    expect(img).toBeDefined()
  })

  it('Approval row renders an img with the coverUrl when a pending request has a coverUrl', async () => {
    setMe('u2', ['request', 'manage_requests'])
    useRequestStore.setState({
      byId: {
        r2: makeRequest({
          id: 'r2',
          requestedBy: 'u3',
          status: 'pending',
          title: 'Queue Track',
          coverUrl: 'https://i.scdn.co/image/xyz999',
        }),
      },
    })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByText('Queue Track')).toBeInTheDocument())
    const img = screen.getAllByRole('img').find((el) => el.getAttribute('src') === 'https://i.scdn.co/image/xyz999')
    expect(img).toBeDefined()
  })
})

describe('Requests page — album kind cue', () => {
  beforeEach(() => {
    useRequestStore.setState({ byId: {} })
    useAuthStore.setState({ me: null, loading: false })
    mockNavigate.mockReset()
  })

  it('MyRequestRow shows "Album" cue when kind is "album"', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({
      byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending', kind: 'album' }) },
    })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    expect(screen.getByText('Album')).toBeInTheDocument()
  })

  it('MyRequestRow shows NO "Album" cue when kind is "track"', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({
      byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending', kind: 'track' }) },
    })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    expect(screen.queryByText('Album')).not.toBeInTheDocument()
  })

  it('MyRequestRow shows NO "Album" cue when kind is undefined', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({
      byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending' }) },
    })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    expect(screen.queryByText('Album')).not.toBeInTheDocument()
  })

  it('ApprovalRow shows "Album" cue when kind is "album"', async () => {
    setMe('u2', ['request', 'manage_requests'])
    useRequestStore.setState({
      byId: {
        r2: makeRequest({ id: 'r2', requestedBy: 'u3', status: 'pending', title: 'Queue Album', kind: 'album' }),
      },
    })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByText('Queue Album')).toBeInTheDocument())
    expect(screen.getByText('Album')).toBeInTheDocument()
  })

  it('ApprovalRow shows NO "Album" cue when kind is "track"', async () => {
    setMe('u2', ['request', 'manage_requests'])
    useRequestStore.setState({
      byId: {
        r2: makeRequest({ id: 'r2', requestedBy: 'u3', status: 'pending', title: 'Queue Track', kind: 'track' }),
      },
    })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByText('Queue Track')).toBeInTheDocument())
    expect(screen.queryByText('Album')).not.toBeInTheDocument()
  })
})

describe('Requests page — album track count cue', () => {
  beforeEach(() => {
    useRequestStore.setState({ byId: {} })
    useAuthStore.setState({ me: null, loading: false })
    mockNavigate.mockReset()
  })

  it('MyRequestRow shows "Album · 12 tracks" when kind=album and trackCount=12', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({
      byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending', kind: 'album', trackCount: 12 }) },
    })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    expect(screen.getByText('Album · 12 tracks')).toBeInTheDocument()
  })

  it('MyRequestRow shows just "Album" when kind=album and trackCount=0', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({
      byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending', kind: 'album', trackCount: 0 }) },
    })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    expect(screen.getByText('Album')).toBeInTheDocument()
    expect(screen.queryByText(/tracks/i)).not.toBeInTheDocument()
  })

  it('MyRequestRow shows just "Album" when kind=album and trackCount is absent', async () => {
    setMe('u1', ['request'])
    useRequestStore.setState({
      byId: { r1: makeRequest({ requestedBy: 'u1', status: 'pending', kind: 'album' }) },
    })
    renderRequests()
    await waitFor(() => expect(screen.getByText('Test Song')).toBeInTheDocument())
    expect(screen.getByText('Album')).toBeInTheDocument()
    expect(screen.queryByText(/tracks/i)).not.toBeInTheDocument()
  })

  it('ApprovalRow shows "Album · 12 tracks" when kind=album and trackCount=12', async () => {
    setMe('u2', ['request', 'manage_requests'])
    useRequestStore.setState({
      byId: {
        r2: makeRequest({ id: 'r2', requestedBy: 'u3', status: 'pending', title: 'Queue Album', kind: 'album', trackCount: 12 }),
      },
    })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByText('Queue Album')).toBeInTheDocument())
    expect(screen.getByText('Album · 12 tracks')).toBeInTheDocument()
  })

  it('ApprovalRow shows just "Album" when kind=album and trackCount=0', async () => {
    setMe('u2', ['request', 'manage_requests'])
    useRequestStore.setState({
      byId: {
        r2: makeRequest({ id: 'r2', requestedBy: 'u3', status: 'pending', title: 'Queue Album', kind: 'album', trackCount: 0 }),
      },
    })
    renderRequests()
    fireEvent.click(screen.getByRole('tab', { name: /approval/i }))
    await waitFor(() => expect(screen.getByText('Queue Album')).toBeInTheDocument())
    expect(screen.getByText('Album')).toBeInTheDocument()
    expect(screen.queryByText(/tracks/i)).not.toBeInTheDocument()
  })
})

describe('TopBar — Requests nav entry', () => {
  beforeEach(() => {
    useAuthStore.setState({ me: null, loading: false })
    useRequestStore.setState({ byId: {} })
    mockNavigate.mockReset()
    vi.mocked(global.fetch).mockClear()
  })

  it('shows Requests nav entry for a user with "request" capability', () => {
    setMe('u1', ['request'])
    renderTopBar()
    expect(screen.getByRole('button', { name: /requests/i })).toBeInTheDocument()
  })

  it('does NOT show Requests nav entry for a user without "request" capability', () => {
    setMe('u1', []) // no capabilities at all
    renderTopBar()
    expect(screen.queryByRole('button', { name: /requests/i })).not.toBeInTheDocument()
  })

  it('shows pending-count badge on Requests button for a manager with pending items', () => {
    setMe('u2', ['request', 'manage_requests'])
    useRequestStore.setState({
      byId: {
        r1: makeRequest({ id: 'r1', requestedBy: 'u3', status: 'pending' }),
        r2: makeRequest({ id: 'r2', requestedBy: 'u4', status: 'pending' }),
      },
    })
    renderTopBar()
    const badge = screen.getByTestId('requests-badge')
    expect(badge).toBeInTheDocument()
    expect(badge.textContent).toBe('2')
  })

  it('does NOT show pending-count badge when user is not a manager', () => {
    setMe('u1', ['request'])
    useRequestStore.setState({
      byId: { r1: makeRequest({ id: 'r1', requestedBy: 'u3', status: 'pending' }) },
    })
    renderTopBar()
    expect(screen.queryByTestId('requests-badge')).not.toBeInTheDocument()
  })
})
