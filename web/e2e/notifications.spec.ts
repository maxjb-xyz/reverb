import { test, expect } from '@playwright/test'
import { installApiMocks, installRequestWsMock, ownerMe } from './mocks'
import type { Route } from '@playwright/test'

// Hermetic e2e for the NotificationBell (mock-driven; no real backend).
//
// The NotificationBell lives in TopBar and is present on EVERY page. It hydrates
// via GET /api/v1/notifications on WS open. installApiMocks registers a default
// empty handler so existing specs are unaffected; this spec OVERRIDES that handler
// with seeded data (Playwright: most-recently-registered-first).
//
// Cases covered:
//   1) Badge shows unread count (1) when notifications hydrate with unread:1.
//   2) Opening the bell shows the notification center with both notifications,
//      newest-first.
//   3) "Mark all read" fires POST /notifications/read with empty ids array, and
//      the badge disappears.
//   4) Clicking a notification fires POST /notifications/read with that id and
//      navigates to /requests.

// ── fixtures ─────────────────────────────────────────────────────────────────

// Two notifications: notifUnread is newer (higher createdAt) and unread;
// notifRead is older and read. This lets us assert newest-first ordering and
// badge count of 1.
const nowSec = Math.floor(Date.now() / 1000)

const notifUnread = {
  id: 'n2',
  userId: ownerMe.id,
  type: 'request_pending',
  title: 'New request',
  body: 'alice requested Album X',
  read: false,
  createdAt: nowSec - 60, // 1 minute ago
}

const notifRead = {
  id: 'n1',
  userId: ownerMe.id,
  type: 'request_pending',
  title: 'Old request',
  body: 'bob requested Album Y',
  read: true,
  createdAt: nowSec - 3600, // 1 hour ago
}

// Server response: unread=1
const seededNotifications = {
  notifications: [notifUnread, notifRead],
  unread: 1,
}

// ── helpers ───────────────────────────────────────────────────────────────────

type Page = Parameters<typeof installApiMocks>[0]

async function bootAuthed(page: Page) {
  // Pre-authenticated so we skip the login double-navigate ERR_ABORTED race.
  const authed = { value: true }
  await installApiMocks(page, authed)

  // Override the default empty notifications handler with seeded data.
  // Must be registered AFTER installApiMocks so it wins (most-recently-registered-first).
  await page.route('**/api/v1/notifications', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(seededNotifications),
    }),
  )

  // Capture POST /notifications/read bodies for later assertions.
  // This overrides the default from installApiMocks (most-recently-registered-first).
  const readPosts: Array<{ ids: string[] }> = []
  await page.route('**/api/v1/notifications/read', async (route: Route) => {
    if (route.request().method() === 'POST') {
      const raw = route.request().postData() ?? '{}'
      try {
        readPosts.push(JSON.parse(raw) as { ids: string[] })
      } catch {
        readPosts.push({ ids: [] })
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ unread: 0 }),
      })
    }
    return route.continue()
  })

  // WS interceptor so the socket doesn't try to reach a real server.
  // On WS open the app calls GET /api/v1/notifications — the seeded handler above
  // responds with unread:1, which setAll feeds into the store.
  await installRequestWsMock(page)

  // Set up response waiters BEFORE page.goto so we don't miss responses that
  // arrive before the await is registered. Mirrors the core-loop.spec.ts pattern
  // for the initialResync — responses can arrive very quickly in parallel runs.
  const notificationsResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/notifications') && r.request().method() === 'GET')
    .catch(() => undefined)

  const downloadsResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)

  await page.goto('/')
  // Wait for the app shell to be visible
  await expect(page.getByTestId('app-shell-root')).toBeVisible()

  // Wait for both hydration responses to settle before asserting on the badge.
  await notificationsResync
  await downloadsResync

  return { readPosts }
}

// ── 1+2) Badge visible; notification center lists both items newest-first ─────

test('notifications: badge shows unread count; center lists notifications newest-first', async ({ page }) => {
  await bootAuthed(page)

  // ── Case 1: badge is visible with count 1 ──────────────────────────────────
  const badge = page.getByTestId('notification-badge')
  await expect(badge).toBeVisible()
  await expect(badge).toHaveText('1')

  // ── Case 2: open the notification center ───────────────────────────────────
  const bell = page.getByRole('button', { name: /Notifications/ })
  await expect(bell).toBeVisible()
  await bell.click()

  // The panel should be open (role="menu" aria-label="Notifications")
  const center = page.getByRole('menu', { name: 'Notifications' })
  await expect(center).toBeVisible()

  // Both notification titles should be visible
  await expect(center.getByRole('button', { name: notifUnread.title })).toBeVisible()
  await expect(center.getByRole('button', { name: notifRead.title })).toBeVisible()

  // Newest-first order: notifUnread (n2, newer) must appear before notifRead (n1, older).
  // The notification list items are <li> elements; the first li button should be the newer one.
  const notifButtons = center.locator('li button')
  const firstText = await notifButtons.first().textContent()
  expect(firstText).toContain(notifUnread.title)
})

// ── 3) "Mark all read" fires POST /notifications/read + badge clears ──────────

test('notifications: "Mark all read" fires POST with empty ids and clears the badge', async ({ page }) => {
  const { readPosts } = await bootAuthed(page)

  // Badge is visible with unread:1
  const badge = page.getByTestId('notification-badge')
  await expect(badge).toBeVisible()

  // Open the notification center
  await page.getByRole('button', { name: /Notifications/ }).click()
  const center = page.getByRole('menu', { name: 'Notifications' })
  await expect(center).toBeVisible()

  // "Mark all read" button is present (only shown when unread > 0)
  const markAllBtn = center.getByRole('button', { name: 'Mark all read' })
  await expect(markAllBtn).toBeVisible()

  // Click "Mark all read" and wait for the POST to fire
  const readPost = page.waitForRequest(
    (r) => r.url().includes('/api/v1/notifications/read') && r.method() === 'POST',
  )
  await markAllBtn.click()
  await readPost

  // The POST body should have empty ids (postMarkRead(undefined) sends { ids: [] })
  expect(readPosts.length).toBeGreaterThanOrEqual(1)
  const lastPost = readPosts[readPosts.length - 1]
  expect(Array.isArray(lastPost.ids)).toBe(true)
  expect(lastPost.ids.length).toBe(0)

  // The badge should disappear (unread is now 0)
  await expect(page.getByTestId('notification-badge')).toHaveCount(0)
})

// ── 4) Clicking a notification navigates to /requests + fires POST with id ────

test('notifications: clicking a notification fires POST /notifications/read with its id and navigates to /requests', async ({ page }) => {
  const { readPosts } = await bootAuthed(page)

  // Open the notification center
  await page.getByRole('button', { name: /Notifications/ }).click()
  const center = page.getByRole('menu', { name: 'Notifications' })
  await expect(center).toBeVisible()

  // Click the unread notification
  const notifBtn = center.getByRole('button', { name: notifUnread.title })
  await expect(notifBtn).toBeVisible()

  const readPost = page.waitForRequest(
    (r) => r.url().includes('/api/v1/notifications/read') && r.method() === 'POST',
  )
  await notifBtn.click()
  await readPost

  // The POST body should contain the notification's id
  expect(readPosts.length).toBeGreaterThanOrEqual(1)
  const lastPost = readPosts[readPosts.length - 1]
  expect(Array.isArray(lastPost.ids)).toBe(true)
  expect(lastPost.ids).toContain(notifUnread.id)

  // Navigation to /requests should have occurred
  await expect(page).toHaveURL(/\/requests/)
})
