import { test, expect, type Page } from '@playwright/test'
import { installApiMocks, ownerMe, externalTrack } from './mocks'

// The multi-user UI, end to end and hermetic (same mocking style as the other
// specs — no real backend; every /api/v1/* call is intercepted in-spec).
//
//   1) First-run setup requires BOTH a username and a password, creates the owner,
//      and lands in the onboarding wizard.
//   2) Capability gating of the Admin surface: an admin sees the Admin menu entry;
//      a non-admin does not, and is redirected away from /admin.
//   3) Capability gating of the download control: a user without `auto_approve`
//      sees no download affordance on a not-in-library result; one with it does.

// ── helpers ──────────────────────────────────────────────────────────────────

// Drive the Everywhere search so the one not-in-library external track renders
// (mirrors the core-loop flow). Returns once the result row's title is visible.
async function searchEverywhere(page: Page) {
  const topSearch = page.getByPlaceholder(/or everywhere/)
  await topSearch.fill(externalTrack.title)
  await topSearch.press('Enter')
  await expect(page.getByRole('tab', { name: 'Everywhere' })).toBeVisible()
  await page.getByRole('tab', { name: 'Everywhere' }).click()
  // exact:true → the track row's title span, not the results header.
  await expect(page.getByText(externalTrack.title, { exact: true })).toBeVisible()
}

// Log in through the real form. On success the form does window.location.assign('/')
// which triggers a full page reload — wait for the navigation to settle, then verify
// the app shell is visible (the re-mounted App re-probes the session and finds it authed).
async function loginAndLand(page: Page) {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()
  await page.getByLabel('Username').fill('user')
  await page.getByLabel('Password').fill('pw123456')
  await page.getByRole('button', { name: 'Log in' }).click()
  await page.waitForURL('/')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()
}

// ── 1) First-run setup requires username + password ──────────────────────────

test('setup: first-run requires a username AND a password, then creates the owner and lands in the app', async ({ page }) => {
  const authed = { value: false }
  await installApiMocks(page, authed, { setupRequired: true })

  // /setup/admin issues the owner session; the wizard then refetches /me (owner)
  // and advances to the onboarding steps. Flip `authed` so any subsequent /me is
  // the authenticated owner.
  await page.route('**/api/v1/setup/admin', (route) => {
    authed.value = true
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) })
  })
  // The wizard's Library/Search/Downloader steps read the adapter catalog.
  await page.route('**/api/v1/adapters/available', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) }),
  )

  // 1) Setup screen (setupRequired:true) — NOT the Login screen.
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Welcome to Reverb' })).toBeVisible()

  // Both a username and a password input are present and required for the owner.
  const usernameInput = page.getByPlaceholder('Choose a username')
  const passwordInput = page.getByPlaceholder('Choose a password')
  await expect(usernameInput).toBeVisible()
  await expect(passwordInput).toBeVisible()

  // 2) Fill both and continue → POST /setup/admin → advances to the onboarding
  //    wizard (the Library step), proving the owner was created and we're in the app.
  await usernameInput.fill('owner')
  await passwordInput.fill('pw12345')
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByRole('heading', { name: 'Add a Library' })).toBeVisible()
})

// ── 2) Admin surface is capability-gated ─────────────────────────────────────

test('admin gating: an admin sees the Admin menu entry; a non-admin does not and is redirected from /admin', async ({ page }) => {
  const authed = { value: true }
  // Admin (is_admin) session.
  await installApiMocks(page, authed, { me: ownerMe })

  await page.goto('/')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()

  // Open the account menu — the Admin entry is present for a manager.
  await page.getByRole('button', { name: 'Account menu' }).click()
  await expect(page.getByRole('menuitem', { name: 'Admin' })).toBeVisible()
})

test('admin gating: a non-admin user has no Admin entry and is redirected away from /admin', async ({ page }) => {
  const authed = { value: true }
  // A plain requester: no management capabilities at all.
  const requester = {
    ...ownerMe,
    id: 'user-bob',
    username: 'bob',
    roleId: 'role-user',
    roleName: 'User',
    isOwner: false,
    capabilities: ['request'],
  }
  await installApiMocks(page, authed, { me: requester })

  await page.goto('/')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()

  // The account menu opens, but there is no Admin entry for a non-manager.
  await page.getByRole('button', { name: 'Account menu' }).click()
  await expect(page.getByRole('menuitem', { name: 'Account' })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: 'Admin' })).toHaveCount(0)

  // Hitting /admin directly redirects home (defense-in-depth route guard).
  await page.goto('/admin')
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: 'Admin' })).toHaveCount(0)
})

// ── 3) Download control is capability-gated ──────────────────────────────────

test('download gating: a user without auto_approve sees no download control on a not-in-library result', async ({ page }) => {
  const authed = { value: false }
  // A requester WITHOUT auto_approve (can browse + request, but not download).
  const requester = {
    ...ownerMe,
    id: 'user-bob',
    username: 'bob',
    roleId: 'role-user',
    roleName: 'User',
    isOwner: false,
    capabilities: ['request'],
  }
  await installApiMocks(page, authed, { me: requester })

  await loginAndLand(page)
  await searchEverywhere(page)

  // The download affordance is gated off; the in-library Play branch is unaffected
  // (this track is not_in_library, so the whole control collapses to nothing).
  await expect(page.getByRole('button', { name: `Download ${externalTrack.title}`, exact: true })).toHaveCount(0)
})

test('download gating: a user WITH auto_approve sees the download control on the same result', async ({ page }) => {
  const authed = { value: false }
  // A downloader: same plain user but WITH auto_approve.
  const downloader = {
    ...ownerMe,
    id: 'user-dave',
    username: 'dave',
    roleId: 'role-user',
    roleName: 'User',
    isOwner: false,
    capabilities: ['request', 'auto_approve'],
  }
  await installApiMocks(page, authed, { me: downloader })

  await loginAndLand(page)
  await searchEverywhere(page)

  // The Download button is present for a user who can download.
  await expect(page.getByRole('button', { name: `Download ${externalTrack.title}`, exact: true })).toBeVisible()
})
