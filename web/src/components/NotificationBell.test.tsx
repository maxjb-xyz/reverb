import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { NotificationBell } from './NotificationBell'
import { useNotificationStore } from '../lib/notificationApi'
import type { Notification } from '../lib/notificationApi'

// Mock postMarkRead so tests control its resolution
vi.mock('../lib/notificationApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/notificationApi')>()
  return {
    ...actual,
    postMarkRead: vi.fn().mockResolvedValue({ unread: 0 }),
  }
})

// Mock navigate
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

import { postMarkRead } from '../lib/notificationApi'

function makeNotification(overrides?: Partial<Notification>): Notification {
  return {
    id: 'n1',
    userId: 'u1',
    type: 'request_approved',
    title: 'Request approved',
    body: 'Your request for Track A was approved.',
    read: false,
    createdAt: Date.now(),
    ...overrides,
  }
}

function renderBell() {
  return render(
    <MemoryRouter>
      <NotificationBell />
    </MemoryRouter>,
  )
}

describe('NotificationBell', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    vi.mocked(postMarkRead).mockClear()
    // Reset to empty store state
    useNotificationStore.setState({ byId: {}, unread: 0 })
  })

  // --- Badge ---

  it('hides the badge when unread === 0', () => {
    renderBell()
    expect(screen.queryByTestId('notification-badge')).not.toBeInTheDocument()
  })

  it('shows the badge with the unread count when unread > 0', () => {
    const n1 = makeNotification({ id: 'n1', read: false })
    const n2 = makeNotification({ id: 'n2', read: false, title: 'Second' })
    useNotificationStore.setState({ byId: { n1, n2 }, unread: 2 })
    renderBell()
    const badge = screen.getByTestId('notification-badge')
    expect(badge).toBeInTheDocument()
    expect(badge.textContent).toBe('2')
  })

  it('caps the badge display at 9+ when unread > 9', () => {
    const byId: Record<string, Notification> = {}
    for (let i = 1; i <= 12; i++) {
      byId[`n${i}`] = makeNotification({ id: `n${i}`, read: false })
    }
    useNotificationStore.setState({ byId, unread: 12 })
    renderBell()
    const badge = screen.getByTestId('notification-badge')
    expect(badge.textContent).toBe('9+')
  })

  // --- Bell button a11y ---

  it('the bell button has a descriptive aria-label including unread count when unread > 0', () => {
    const n1 = makeNotification({ id: 'n1', read: false })
    useNotificationStore.setState({ byId: { n1 }, unread: 1 })
    renderBell()
    const btn = screen.getByRole('button', { name: /notifications.*1.*unread/i })
    expect(btn).toBeInTheDocument()
  })

  it('the bell button has a plain "Notifications" aria-label when unread === 0', () => {
    renderBell()
    expect(screen.getByRole('button', { name: /^notifications$/i })).toBeInTheDocument()
  })

  // --- Dropdown open/close ---

  it('clicking the bell opens the notification dropdown', () => {
    renderBell()
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    expect(screen.getByRole('menu')).toBeInTheDocument()
  })

  it('shows an empty state when there are no notifications', () => {
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    expect(screen.getByText(/no notifications yet/i)).toBeInTheDocument()
  })

  it('lists notifications newest-first (title + body)', () => {
    const older = makeNotification({ id: 'n-old', title: 'Older notif', body: 'Old body', createdAt: 1000 })
    const newer = makeNotification({ id: 'n-new', title: 'Newer notif', body: 'New body', createdAt: 2000 })
    useNotificationStore.setState({ byId: { 'n-old': older, 'n-new': newer }, unread: 2 })
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))

    // Notification items are buttons with the title as aria-label — query by exact title names
    const newerBtn = screen.getByRole('button', { name: 'Newer notif' })
    const olderBtn = screen.getByRole('button', { name: 'Older notif' })
    expect(newerBtn).toBeInTheDocument()
    expect(olderBtn).toBeInTheDocument()

    // Newer should appear before Older in the DOM (newest-first order)
    const menu = screen.getByRole('menu')
    const allButtons = Array.from(menu.querySelectorAll('button')) as HTMLElement[]
    const newerIdx = allButtons.indexOf(newerBtn)
    const olderIdx = allButtons.indexOf(olderBtn)
    expect(newerIdx).toBeLessThan(olderIdx)

    // Both title and body should be visible
    expect(screen.getByText('Newer notif')).toBeInTheDocument()
    expect(screen.getByText('New body')).toBeInTheDocument()
    expect(screen.getByText('Older notif')).toBeInTheDocument()
    expect(screen.getByText('Old body')).toBeInTheDocument()
  })

  it('renders the dropdown header "Notifications"', () => {
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    // heading inside the menu
    expect(screen.getByRole('menu')).toBeInTheDocument()
    expect(screen.getByText('Notifications')).toBeInTheDocument()
  })

  it('shows "Mark all read" only when unread > 0', () => {
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    expect(screen.queryByRole('button', { name: /mark all read/i })).not.toBeInTheDocument()
  })

  it('shows "Mark all read" button when unread > 0', () => {
    const n1 = makeNotification({ id: 'n1', read: false })
    useNotificationStore.setState({ byId: { n1 }, unread: 1 })
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    expect(screen.getByRole('button', { name: /mark all read/i })).toBeInTheDocument()
  })

  it('closes the dropdown when Escape is pressed', () => {
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    expect(screen.getByRole('menu')).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('closes the dropdown on backdrop click (click-outside)', () => {
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    expect(screen.getByRole('menu')).toBeInTheDocument()
    // Click the backdrop element (aria-hidden backdrop div)
    const backdrop = document.querySelector('[aria-hidden="true"]')
    expect(backdrop).not.toBeNull()
    fireEvent.click(backdrop!)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  // --- Click notification ---

  it('clicking a notification navigates to /requests', async () => {
    const n1 = makeNotification({ id: 'n1', read: false, title: 'Request approved' })
    useNotificationStore.setState({ byId: { n1 }, unread: 1 })
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))

    const notifBtn = screen.getByRole('button', { name: /request approved/i })
    fireEvent.click(notifBtn)

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/requests')
    })
  })

  it('clicking a notification calls postMarkRead with its id', async () => {
    const n1 = makeNotification({ id: 'n1', read: false, title: 'Request approved' })
    useNotificationStore.setState({ byId: { n1 }, unread: 1 })
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))

    const notifBtn = screen.getByRole('button', { name: /request approved/i })
    fireEvent.click(notifBtn)

    await waitFor(() => {
      expect(postMarkRead).toHaveBeenCalledWith(['n1'])
    })
  })

  it('clicking a notification marks it read in the store', async () => {
    const n1 = makeNotification({ id: 'n1', read: false, title: 'Request approved' })
    useNotificationStore.setState({ byId: { n1 }, unread: 1 })
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))

    fireEvent.click(screen.getByRole('button', { name: /request approved/i }))

    await waitFor(() => {
      expect(useNotificationStore.getState().unread).toBe(0)
    })
  })

  it('clicking a notification closes the dropdown', async () => {
    const n1 = makeNotification({ id: 'n1', read: false, title: 'Request approved' })
    useNotificationStore.setState({ byId: { n1 }, unread: 1 })
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))

    fireEvent.click(screen.getByRole('button', { name: /request approved/i }))

    await waitFor(() => {
      expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    })
  })

  // --- Mark all read ---

  it('"Mark all read" calls postMarkRead with no ids', async () => {
    const n1 = makeNotification({ id: 'n1', read: false })
    useNotificationStore.setState({ byId: { n1 }, unread: 1 })
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    fireEvent.click(screen.getByRole('button', { name: /mark all read/i }))

    await waitFor(() => {
      expect(postMarkRead).toHaveBeenCalledWith(undefined)
    })
  })

  it('"Mark all read" zeroes the store unread count', async () => {
    const n1 = makeNotification({ id: 'n1', read: false })
    const n2 = makeNotification({ id: 'n2', read: false })
    useNotificationStore.setState({ byId: { n1, n2 }, unread: 2 })
    renderBell()
    fireEvent.click(screen.getByRole('button', { name: /notifications/i }))
    fireEvent.click(screen.getByRole('button', { name: /mark all read/i }))

    await waitFor(() => {
      expect(useNotificationStore.getState().unread).toBe(0)
    })
  })
})
