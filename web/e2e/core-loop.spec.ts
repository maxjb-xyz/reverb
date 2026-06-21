import { test, expect } from '@playwright/test'
import { installApiMocks, installWsMock, externalTrack } from './mocks'

test('core loop: login -> search everywhere -> download -> in-library -> play', async ({ page }) => {
  const authed = { value: false }
  // Install HTTP mocks first (GET /downloads returns [] so no pre-existing job).
  await installApiMocks(page, authed)
  // Install WS mock and get the trigger object; does NOT send any frame yet.
  const ws = await installWsMock(page)

  // 1) Load: setup not required, not authed -> Login screen.
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Log in to Crate' })).toBeVisible()

  // 2) Log in. The app reloads on success; /me now returns authed.
  await page.getByPlaceholder('Admin password').fill('correct horse')
  await page.getByRole('button', { name: 'Log in' }).click()

  // 3) After reload we land on /search (default route). Switch to Everywhere.
  await expect(page.getByRole('button', { name: 'Everywhere' })).toBeVisible()
  await page.getByRole('button', { name: 'Everywhere' }).click()

  // 4) Search; the SSE mock returns one not-in-library track.
  await page.getByPlaceholder('Search your library…').fill(externalTrack.title)
  await expect(page.getByText(externalTrack.title)).toBeVisible()

  // The Download button is present (row is NOT in library — GET /downloads was []).
  const downloadBtn = page.getByRole('button', { name: `Download ${externalTrack.title}` })
  await expect(downloadBtn).toBeVisible()

  // 5) Click Download -> POST /downloads -> queued job-1 upserted into the store.
  //    NOW send the WS completion frame to flip job-1 to completed+libraryTrackId.
  await downloadBtn.click()
  await ws.complete()

  // The Download button disappears; the in-library ✓ row appears.
  await expect(downloadBtn).toHaveCount(0)
  await expect(page.getByTitle('In library')).toBeVisible()

  // 6) Play: clicking the in-library row plays the synthesized track. The player
  //    bar shows the title and a Pause button (playing flipped true).
  //    SSE auto-reconnects cause brief DOM re-renders (deduped data, new state
  //    reference); use page.evaluate to dispatch a click synchronously in the
  //    page context, bypassing Playwright's stability check that would timeout
  //    on a rapidly-rerendering but functionally-stable element.
  await page.evaluate(() => {
    const btn = document.querySelector('button:has([title="In library"])') as HTMLButtonElement | null
    btn?.click()
  })
  await expect(page.getByTestId('player-bar').getByText(externalTrack.title)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible()
})
