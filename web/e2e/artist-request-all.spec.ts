import { test, expect } from '@playwright/test'
import {
  installApiMocks,
  installArtistRequestAllMocks,
  installRequestWsMock,
  ownerMe,
  requestAllArtistId,
  requestAllArtistName,
  requestAllPartialAlbumId,
  requestAllPartialAlbumName,
  requestAllFullAlbumId,
  requestAllFullAlbumName,
} from './mocks'

// Hermetic e2e for the Artist "Request all" feature (mock-driven; no real backend).
//
//  Artist has 2 albums:
//    - Partial Album (al-partial-1): ownedCount=1 / totalCount=3  → NOT fully owned
//    - Full Album    (al-full-1):    ownedCount=2 / totalCount=2  → FULLY owned
//
//  Spec:
//    1) "Request all" button is visible for a user with `request` capability.
//    2) Clicking it opens the disclosure dialog.
//    3) Confirming fires POST /requests/batch.
//    4) The batch body carries exactly one album item (kind:'album') for the partial
//       album, and the fully-owned album is EXCLUDED.
//    5) A success toast appears after the batch resolves.
//
// Both tests use authed:true (pre-authenticated) to bypass the login double-navigate
// pattern that triggers ERR_ABORTED when vite preview hasn't warmed up. This mirrors
// the approach in requests.spec.ts "regression" test.

// A user that has the `request` cap (but NOT auto_approve, so the toast says
// "pending approval"). We base off ownerMe so all other shell caps are present.
const requesterWithRequest = {
  ...ownerMe,
  id: 'user-owner',
  capabilities: [
    'is_admin',
    'can_manage_users',
    'can_manage_library',
    'request',
    'can_create_playlists',
    // NOTE: no 'auto_approve' — toast will say "pending approval"
  ],
}

test('artist request-all: "Request all" visible for request-cap user, dialog opens, confirm POSTs batch with only the partial album (kind:album), success toast appears', async ({ page }) => {
  // Start pre-authenticated (authed:true) — eliminates the login double-navigate
  // ERR_ABORTED race that can occur when vite preview hasn't warmed up.
  const authed = { value: true }
  await installApiMocks(page, authed, { me: requesterWithRequest })
  const { getLastBatchBody } = await installArtistRequestAllMocks(page)
  // WS interceptor so the realtime socket doesn't reach a real server.
  await installRequestWsMock(page)

  // Load the app shell directly (no login needed — authed is already true).
  await page.goto('/')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()

  // ── Step 1: Navigate to the artist page ───────────────────────────────────
  await page.goto(`/artist/spotify/${requestAllArtistId}`)
  await expect(page.getByRole('heading', { name: requestAllArtistName })).toBeVisible()

  // Both album cards should render.
  await expect(page.getByRole('button', { name: requestAllPartialAlbumName, exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: requestAllFullAlbumName, exact: true })).toBeVisible()

  // ── Step 2: "Request all" button is visible (user has `request` cap) ──────
  // The button has aria-label="Request all" (set in Artist.tsx); use exact:false
  // so the visible text "Request all · N" also matches.
  const requestAllBtn = page.getByRole('button', { name: 'Request all', exact: false })
  await expect(requestAllBtn).toBeVisible()

  // The button should NOT be disabled (there is at least one not-fully-owned album).
  await expect(requestAllBtn).toBeEnabled()

  // ── Step 3: Click → disclosure dialog opens ───────────────────────────────
  await requestAllBtn.click()
  const dialog = page.getByRole('dialog', { name: 'Request all albums' })
  await expect(dialog).toBeVisible()
  // Dialog mentions the artist name.
  await expect(dialog).toContainText(requestAllArtistName)

  // ── Step 4: Confirm → POST /requests/batch ────────────────────────────────
  const batchPost = page.waitForRequest(
    (r) => r.url().includes('/api/v1/requests/batch') && r.method() === 'POST',
  )
  await page.getByRole('button', { name: 'Confirm request all', exact: true }).click()
  await batchPost

  // ── Step 5: Assert the batch body ─────────────────────────────────────────
  const body = getLastBatchBody()
  expect(body).not.toBeNull()
  const items = body?.items ?? []

  // Exactly one item: the NOT-fully-owned partial album.
  expect(items).toHaveLength(1)

  const item = items[0] as { kind: string; source: string; externalId: string }
  expect(item.kind).toBe('album')
  expect(item.source).toBe('spotify')
  expect(item.externalId).toBe(requestAllPartialAlbumId)

  // The FULLY-owned album must NOT appear in the batch.
  const fullAlbumInBatch = items.some(
    (i: unknown) => (i as { externalId?: string }).externalId === requestAllFullAlbumId,
  )
  expect(fullAlbumInBatch).toBe(false)

  // ── Step 6: Success toast appears ─────────────────────────────────────────
  await expect(page.getByTestId('toast')).toBeVisible()
  // Toast references the request count (1 album) and "pending approval" since
  // the user does NOT have auto_approve.
  await expect(page.getByTestId('toast')).toContainText('1 album')
})

test('artist request-all: "Request all" button is hidden for user without `request` capability', async ({ page }) => {
  const noRequestCap = {
    ...ownerMe,
    capabilities: ['is_admin', 'can_manage_users', 'can_manage_library', 'can_create_playlists'],
    // 'request' cap intentionally omitted
  }

  // Pre-authenticated — same pattern as above.
  const authed = { value: true }
  await installApiMocks(page, authed, { me: noRequestCap })
  await installArtistRequestAllMocks(page)
  await installRequestWsMock(page)

  await page.goto('/')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()
  await page.goto(`/artist/spotify/${requestAllArtistId}`)
  await expect(page.getByRole('heading', { name: requestAllArtistName })).toBeVisible()

  // The "Request all" button must NOT appear for a user without the `request` cap.
  await expect(page.getByRole('button', { name: 'Request all', exact: false })).toHaveCount(0)
})
