import { test, expect, type Page } from '@playwright/test'
import {
  installApiMocks,
  installRequestMocks,
  installRequestWsMock,
  installAlbumDetailMock,
  installAlbumRequestMocks,
  installAlbumRequestWsMock,
  ownerMe,
  externalTrack,
  mockAlbumId,
  mockAlbumName,
  mockAlbumArtist,
  pendingAlbumRequest,
} from './mocks'

// Hermetic e2e for the request flow (mock-driven; no real backend).
//
//  1) Request flow (request-only user):
//     - "/me" has caps ['request'] (no auto_approve / no manage_requests)
//     - A "Request" button appears on the not-in-library Everywhere result
//     - Clicking it POSTs to /requests and flips the button to "Requested"
//     - Navigating to /requests shows the item as "Pending"
//     - A mocked `request.updated` WS frame (status → fulfilled) flips it to
//       "Added" and fires a toast notification
//
//  2) Approval flow (manage_requests user):
//     - "/me" has caps ['request', 'manage_requests']
//     - /requests shows an "Approval" tab
//     - The Approval tab lists the pending item from GET /requests?status=pending
//     - Clicking "Approve" calls POST /requests/{id}/approve

// ── fixtures ─────────────────────────────────────────────────────────────────

// A request-only user: can browse + request, but not download or manage.
const requesterMe = {
  ...ownerMe,
  id: 'user-requester',
  username: 'requester',
  roleId: 'role-user',
  roleName: 'User',
  isOwner: false,
  capabilities: ['request'],
}

// A manager: can request AND approve/deny/manage requests.
const managerMe = {
  ...ownerMe,
  id: 'user-manager',
  username: 'manager',
  roleId: 'role-manager',
  roleName: 'Manager',
  isOwner: false,
  capabilities: ['request', 'manage_requests'],
}

// ── shared helpers ────────────────────────────────────────────────────────────

// Log in via the real login form and wait for the app shell. We navigate to '/'
// twice (same pattern as core-loop.spec.ts) so the authed session resolves into
// the shell.
async function loginAndLand(page: Page, authed: { value: boolean }) {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()
  await page.getByLabel('Username').fill('user')
  await page.getByLabel('Password').fill('pw12345')
  await page.getByRole('button', { name: 'Log in' }).click()
  // The login handler sets authed.value = true; navigate so the /me probe resolves to the authed user.
  await page.goto('/')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()
}

// Drive the Everywhere search so the not-in-library track renders in the results.
async function searchEverywhere(page: Page) {
  const topSearch = page.getByPlaceholder(/or everywhere/)
  await topSearch.fill(externalTrack.title)
  await topSearch.press('Enter')
  await expect(page.getByRole('tab', { name: 'Everywhere' })).toBeVisible()
  await page.getByRole('tab', { name: 'Everywhere' }).click()
  // exact:true → the track row's title span, not the results header.
  await expect(page.getByText(externalTrack.title, { exact: true })).toBeVisible()
}

// ── 1) Request flow ───────────────────────────────────────────────────────────

test('request flow: request-only user sees Request button, submits it, sees Pending, WS fulfillment flips to Added with toast', async ({ page }) => {
  const authed = { value: false }
  await installApiMocks(page, authed, { me: requesterMe })
  await installRequestMocks(page)
  const ws = await installRequestWsMock(page)

  // Wait for the initial WS resync to settle after login (mirrors core-loop pattern).
  const initialResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)

  await loginAndLand(page, authed)
  await initialResync

  // ── Step 1: Request button appears on the not-in-library Everywhere result ──
  await searchEverywhere(page)

  // The Download button must NOT appear (no auto_approve cap).
  await expect(page.getByRole('button', { name: `Download ${externalTrack.title}`, exact: true })).toHaveCount(0)

  // The Request button IS present (cap: request).
  const requestBtn = page.getByRole('button', { name: 'Request', exact: true })
  await expect(requestBtn).toBeVisible()

  // ── Step 2: Clicking Request POSTs and flips to "Requested" ─────────────────
  const requestPost = page.waitForRequest(
    (r) => r.url().includes('/api/v1/requests') && r.method() === 'POST',
  )
  await requestBtn.click()
  await requestPost

  // The button must flip to "Requested" (disabled) after the POST resolves.
  await expect(page.getByRole('button', { name: 'Requested', exact: true })).toBeVisible()
  // The original "Request" button is gone.
  await expect(page.getByRole('button', { name: 'Request', exact: true })).toHaveCount(0)

  // ── Step 3: Navigate to /requests → "My Requests" shows the item Pending ────
  await page.goto('/requests')
  await expect(page.getByRole('heading', { name: 'My Requests' })).toBeVisible()

  // The track title appears in the request list.
  await expect(page.getByText(externalTrack.title, { exact: true })).toBeVisible()
  // Its status chip shows "Pending".
  await expect(page.getByText('Pending', { exact: true })).toBeVisible()

  // ── Step 4: WS frame flips to "Added" and a toast appears ───────────────────
  await ws.fulfill()

  // Status chip flips from Pending → Added.
  await expect(page.getByText('Added', { exact: true })).toBeVisible()
  await expect(page.getByText('Pending', { exact: true })).toHaveCount(0)

  // A success toast surfaces with the expected message.
  await expect(page.getByTestId('toast')).toBeVisible()
  await expect(page.getByTestId('toast')).toContainText(externalTrack.title)
})

// ── 2) Approval flow ─────────────────────────────────────────────────────────

test('approval flow: manager sees Approval tab, pending item listed, clicking Approve calls POST /approve', async ({ page }) => {
  const authed = { value: false }
  await installApiMocks(page, authed, { me: managerMe })
  await installRequestMocks(page)
  // We don't need the WS trigger for the approval test; still need the WS interceptor
  // so the realtime connection is mocked and doesn't try to hit a real server.
  await installRequestWsMock(page)

  const initialResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)

  await loginAndLand(page, authed)
  await initialResync

  // ── Step 1: Navigate to /requests; Approval tab is visible for managers ─────
  await page.goto('/requests')
  await expect(page.getByRole('heading', { name: 'My Requests' })).toBeVisible()

  // The Approval tab only renders for manage_requests users.
  const approvalTab = page.getByRole('tab', { name: 'Approval' })
  await expect(approvalTab).toBeVisible()

  // ── Step 2: Switch to Approval tab; pending item appears ────────────────────
  // Submit a request first so the GET /requests?status=pending returns it.
  // (installRequestMocks starts with an empty list; POST /requests populates it.)
  // We POST directly via the mock to seed the approval queue without needing a UI flow.
  // Instead: call the seed endpoint by driving a short search + Request click.
  // We'll navigate back to /requests afterwards.
  await page.goto('/')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()
  await searchEverywhere(page)
  const seedPost = page.waitForRequest(
    (r) => r.url().includes('/api/v1/requests') && r.method() === 'POST',
  )
  await page.getByRole('button', { name: 'Request', exact: true }).click()
  await seedPost

  // Back to /requests.
  await page.goto('/requests')
  await expect(page.getByRole('heading', { name: 'My Requests' })).toBeVisible()

  // Switch to the Approval tab.
  await page.getByRole('tab', { name: 'Approval' }).click()

  // The pending request appears in the approval queue.
  await expect(page.getByText(externalTrack.title, { exact: true })).toBeVisible()

  // ── Step 3: Approve button is present; clicking it calls POST /approve ───────
  const approveBtn = page.getByRole('button', { name: `Approve ${externalTrack.title}`, exact: true })
  await expect(approveBtn).toBeVisible()

  const approvePost = page.waitForRequest(
    (r) => r.url().includes('/approve') && r.method() === 'POST',
  )
  await approveBtn.click()
  await approvePost
  // The POST resolved — the request is now approved in the mock.
  // The Approval row may update or be removed; the primary assertion is the POST fired.
})

// ── 3) Album request flow ─────────────────────────────────────────────────────
//
// A requester navigates to an album detail page, clicks "Request album", confirms
// the disclosure dialog, and the POST /requests body has `kind === "album"`.
// The item then appears in My Requests with the "Album" cue (badge text "ALBUM");
// a mocked `request.updated` WS frame (status → fulfilled) flips the row to "Added".

test('album request: requester sees "Request album" on album page, confirms dialog, POST has kind:album, appears in My Requests with Album cue, WS flips to Added', async ({ page }) => {
  const authed = { value: false }
  await installApiMocks(page, authed, { me: requesterMe })
  // Mount the album detail route BEFORE the request mocks (installApiMocks has already
  // registered the downloads + adapters handlers; album detail is a new route).
  await installAlbumDetailMock(page)
  const { getLastPostBody } = await installAlbumRequestMocks(page)
  const ws = await installAlbumRequestWsMock(page)

  const initialResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)

  await loginAndLand(page, authed)
  await initialResync

  // ── Step 1: Navigate to the album detail page ──────────────────────────────
  await page.goto(`/album/spotify/${mockAlbumId}`)
  // The album name is in the <h1>.
  await expect(page.getByRole('heading', { level: 1, name: mockAlbumName })).toBeVisible()

  // ── Step 2: "Request album" button is gated on `request` cap ───────────────
  const requestAlbumBtn = page.getByRole('button', { name: 'Request album', exact: true })
  await expect(requestAlbumBtn).toBeVisible()

  // ── Step 3: Click → disclosure dialog opens ────────────────────────────────
  await requestAlbumBtn.click()
  // The dialog has aria-label="Request album"
  const dialog = page.getByRole('dialog', { name: 'Request album' })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('Request the whole album?')

  // ── Step 4: Confirm → POST /requests with kind:"album" ────────────────────
  const requestPost = page.waitForRequest(
    (r) => r.url().includes('/api/v1/requests') && r.method() === 'POST',
  )
  await page.getByRole('button', { name: 'Confirm request album', exact: true }).click()
  await requestPost

  // Assert the request body sent to the server includes kind:"album"
  const body = getLastPostBody()
  expect(body?.kind).toBe('album')

  // ── Step 5: Navigate to /requests → item appears in My Requests with Album cue
  await page.goto('/requests')
  await expect(page.getByRole('heading', { name: 'My Requests' })).toBeVisible()

  // The album title appears in the request list.
  await expect(page.getByText(mockAlbumName, { exact: true })).toBeVisible()
  // Status is "Pending".
  await expect(page.getByText('Pending', { exact: true })).toBeVisible()
  // The "Album" badge cue is visible (Requests.tsx renders it when req.kind === "album").
  await expect(page.getByText('Album', { exact: true })).toBeVisible()

  // ── Step 6: WS frame flips the row to "Added" ─────────────────────────────
  await ws.fulfill()

  await expect(page.getByText('Added', { exact: true })).toBeVisible()
  await expect(page.getByText('Pending', { exact: true })).toHaveCount(0)

  // A success toast surfaces referencing the album.
  await expect(page.getByTestId('toast')).toBeVisible()
  await expect(page.getByTestId('toast')).toContainText(mockAlbumName)
})

// ── 4) Manager approval of album request ─────────────────────────────────────
//
// A manager sees the album request in the Approval queue with the "Album" cue,
// and clicking Approve fires POST /requests/{id}/approve.

test('album request approval: manager sees album request in Approval queue with Album cue, Approve calls POST /approve', async ({ page }) => {
  const authed = { value: false }
  await installApiMocks(page, authed, { me: managerMe })
  await installAlbumDetailMock(page)
  const { getLastPostBody } = await installAlbumRequestMocks(page)
  // We need the WS interceptor so the socket doesn't try to reach a real server,
  // but we don't need to fire a frame.
  await installAlbumRequestWsMock(page)

  const initialResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)

  await loginAndLand(page, authed)
  await initialResync

  // ── Step 1: Navigate to the album page and submit a request to seed the queue
  await page.goto(`/album/spotify/${mockAlbumId}`)
  await expect(page.getByRole('heading', { level: 1, name: mockAlbumName })).toBeVisible()

  const requestAlbumBtn = page.getByRole('button', { name: 'Request album', exact: true })
  await expect(requestAlbumBtn).toBeVisible()
  await requestAlbumBtn.click()

  const dialog = page.getByRole('dialog', { name: 'Request album' })
  await expect(dialog).toBeVisible()

  const seedPost = page.waitForRequest(
    (r) => r.url().includes('/api/v1/requests') && r.method() === 'POST',
  )
  await page.getByRole('button', { name: 'Confirm request album', exact: true }).click()
  await seedPost

  // Confirm the body sent had kind:"album"
  expect(getLastPostBody()?.kind).toBe('album')

  // ── Step 2: Navigate to /requests → Approval tab is visible for managers ───
  await page.goto('/requests')
  await expect(page.getByRole('heading', { name: 'My Requests' })).toBeVisible()

  const approvalTab = page.getByRole('tab', { name: 'Approval' })
  await expect(approvalTab).toBeVisible()
  await approvalTab.click()

  // ── Step 3: Album request appears in Approval queue WITH the Album cue ──────
  await expect(page.getByText(mockAlbumName, { exact: true })).toBeVisible()
  // The "Album" badge (rendered when req.kind === "album") is visible.
  await expect(page.getByText('Album', { exact: true })).toBeVisible()

  // ── Step 4: Approve button fires POST /requests/{id}/approve ─────────────────
  const approveBtn = page.getByRole('button', { name: `Approve ${mockAlbumName}`, exact: true })
  await expect(approveBtn).toBeVisible()

  const approvePost = page.waitForRequest(
    (r) => r.url().includes('/approve') && r.method() === 'POST',
  )
  await approveBtn.click()
  await approvePost
  // POST resolved — primary assertion is the approve endpoint was called.
})

// ── 5) Regression: track add is unaffected — single button, no picker ─────────
//
// A full-capability (auto_approve) user on a not-in-library Everywhere result sees
// exactly ONE "Download" button and NO picker caret / popover (the picker was removed
// in the downloader-chains feature; the backend chains now handle granularity).

test('regression: track download has a single Download button and no picker popover', async ({ page }) => {
  // Start already authenticated (authed: true) so we skip the login form and the
  // double-navigate pattern in loginAndLand — eliminates the net::ERR_ABORTED race
  // that can occur when page.goto('/') fires before the prior navigation settles.
  const authed = { value: true }
  // Owner has auto_approve + all other caps — the full download path should render.
  await installApiMocks(page, authed)
  // We need a WS interceptor so the realtime socket doesn't try to reach a real server.
  await installRequestWsMock(page)

  // Load the app shell directly (no login needed — authed is already true).
  await page.goto('/')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()

  // Wait for the initial downloads resync to settle before asserting.
  await page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)

  await searchEverywhere(page)

  // Exactly ONE Download button (no picker caret splits it into two).
  const downloadBtns = page.getByRole('button', { name: `Download ${externalTrack.title}`, exact: true })
  await expect(downloadBtns).toHaveCount(1)

  // No picker popover trigger (the old caret/chevron next to the download button).
  // Absence of any button with aria-label containing "picker", "chevron", or "▾" caret.
  await expect(page.getByRole('button', { name: /picker|caret|chevron/i })).toHaveCount(0)

  // Clicking the single Download button works (enqueues a job → Queued badge).
  await downloadBtns.click()
  await expect(page.getByText('Queued')).toBeVisible()
})
