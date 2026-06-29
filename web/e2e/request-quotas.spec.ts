import { test, expect } from '@playwright/test'
import {
  installApiMocks,
  installRequestQuotaCapMocks,
  installRequestWsMock,
  ownerMe,
  requestAllArtistId,
  requestAllArtistName,
  requestAllPartialAlbumName,
  requestAllFullAlbumName,
} from './mocks'

// Hermetic e2e for the request-quotas feature — Case B (batch quotaCapped).
//
//  Scenario: Artist page "Request all" flow where the server responds with
//  { created: 1, skipped: 0, quotaCapped: 2 }.
//
//  Spec:
//   1) A user with the `request` cap visits the artist page.
//   2) They click "Request all" → disclosure dialog opens.
//   3) They confirm → POST /requests/batch fires.
//   4) The batch mock responds with quotaCapped: 2.
//   5) The toast appears and contains "2 not requested (limit reached)".
//
// Pre-authenticated (authed:true) pattern to avoid the login double-navigate
// ERR_ABORTED race (mirrors artist-request-all.spec.ts).

// A user with the `request` cap but NOT auto_approve — toast says "pending approval".
const requesterWithRequest = {
  ...ownerMe,
  id: 'user-owner',
  capabilities: [
    'is_admin',
    'can_manage_users',
    'can_manage_library',
    'request',
    'can_create_playlists',
    // NOTE: no 'auto_approve'
  ],
}

test('request-quotas: "Request all" with quotaCapped:2 — toast notes "2 not requested (limit reached)"', async ({
  page,
}) => {
  // Pre-authenticated so we skip the login double-navigate race.
  const authed = { value: true }
  await installApiMocks(page, authed, { me: requesterWithRequest })
  await installRequestQuotaCapMocks(page)
  // WS interceptor so the realtime socket doesn't try a real server.
  await installRequestWsMock(page)

  // Load the app shell directly (no login needed — authed is already true).
  await page.goto('/')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()

  // Wait for the initial downloads resync to settle before navigating.
  await page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)

  // ── Step 1: Navigate to the artist page ───────────────────────────────────
  await page.goto(`/artist/spotify/${requestAllArtistId}`)
  await expect(page.getByRole('heading', { name: requestAllArtistName })).toBeVisible()

  // Both album cards should render.
  await expect(page.getByRole('button', { name: requestAllPartialAlbumName, exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: requestAllFullAlbumName, exact: true })).toBeVisible()

  // ── Step 2: "Request all" button is visible (user has `request` cap) ──────
  const requestAllBtn = page.getByRole('button', { name: 'Request all', exact: false })
  await expect(requestAllBtn).toBeVisible()
  await expect(requestAllBtn).toBeEnabled()

  // ── Step 3: Click → disclosure dialog opens ───────────────────────────────
  await requestAllBtn.click()
  const dialog = page.getByRole('dialog', { name: 'Request all albums' })
  await expect(dialog).toBeVisible()

  // ── Step 4: Confirm → POST /requests/batch with quotaCapped:2 response ────
  const batchPost = page.waitForRequest(
    (r) => r.url().includes('/api/v1/requests/batch') && r.method() === 'POST',
  )
  await page.getByRole('button', { name: 'Confirm request all', exact: true }).click()
  await batchPost

  // ── Step 5: Toast shows quota-cap note ────────────────────────────────────
  const toast = page.getByTestId('toast')
  await expect(toast).toBeVisible()
  // The toast must mention the quota cap count and the server-defined phrase.
  await expect(toast).toContainText('2 not requested (limit reached)')
})
