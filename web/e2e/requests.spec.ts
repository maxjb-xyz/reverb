import { test, expect, type Page } from '@playwright/test'
import {
  installApiMocks,
  installRequestMocks,
  installRequestWsMock,
  ownerMe,
  externalTrack,
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
