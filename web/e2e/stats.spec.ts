import { test, expect } from '@playwright/test'
import { installApiMocks, installRequestWsMock } from './mocks'
import type { Route } from '@playwright/test'

// Hermetic e2e for the /stats dashboard (SP3-3a; mock-driven, no real backend).
//
// installApiMocks registers default empty /stats/* + /plays handlers (so every
// page renders). This spec OVERRIDES the /stats/* endpoints with seeded data
// (Playwright: most-recently-registered-first) and asserts the dashboard renders
// cards / top-lists / timeline / heatmap, and that switching the range refetches.
//
// JSON is PascalCase — the Go stats structs have no json tags, so they serialize
// exported field names verbatim.

type Page = Parameters<typeof installApiMocks>[0]

const summary = { Plays: 42, DistinctTracks: 30, DistinctArtists: 12, DistinctAlbums: 9, MsPlayed: 9_000_000 } // 2h 30m
const topTracks = [
  { CatalogID: 'trk_a', Title: 'Karma Police', Artist: 'Radiohead', Album: 'OK Computer', Plays: 20, MsPlayed: 4_000_000 },
  { CatalogID: 'trk_b', Title: 'No Surprises', Artist: 'Radiohead', Album: 'OK Computer', Plays: 12, MsPlayed: 2_500_000 },
]
const topArtists = [{ CatalogID: '', Title: '', Artist: 'Radiohead', Album: '', Plays: 32, MsPlayed: 6_500_000 }]
const topAlbums = [{ CatalogID: '', Title: '', Artist: 'Radiohead', Album: 'OK Computer', Plays: 32, MsPlayed: 6_500_000 }]
const timeline = [
  { Start: 1_719_000_000, Plays: 5, MsPlayed: 1_000_000 },
  { Start: 1_719_086_400, Plays: 18, MsPlayed: 3_500_000 },
  { Start: 1_719_172_800, Plays: 9, MsPlayed: 1_800_000 },
]
const clock = [
  { Weekday: 3, Hour: 14, Plays: 20, MsPlayed: 4_000_000 },
  { Weekday: 5, Hour: 20, Plays: 8, MsPlayed: 1_500_000 },
]
const recent = [
  { CatalogID: 'trk_a', Title: 'Karma Police', Artist: 'Radiohead', Album: 'OK Computer', PlayedAt: 1_719_200_000 },
]

const json = (body: unknown) => (route: Route) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })

async function bootStats(page: Page, summaryFroms: number[]) {
  const authed = { value: true } // pre-authed: single goto, no double-navigate ERR_ABORTED race
  await installApiMocks(page, authed)

  // Capture the `from` of each /stats/summary request so we can prove a refetch
  // happens on range change. Registered AFTER installApiMocks → wins.
  await page.route('**/api/v1/stats/summary**', (route: Route) => {
    const from = Number(new URL(route.request().url()).searchParams.get('from') ?? '0')
    summaryFroms.push(from)
    return json(summary)(route)
  })
  await page.route('**/api/v1/stats/top/tracks**', json(topTracks))
  await page.route('**/api/v1/stats/top/artists**', json(topArtists))
  await page.route('**/api/v1/stats/top/albums**', json(topAlbums))
  await page.route('**/api/v1/stats/timeline**', json(timeline))
  await page.route('**/api/v1/stats/clock**', json(clock))
  await page.route('**/api/v1/stats/recent**', json(recent))

  await installRequestWsMock(page)
  await page.goto('/stats')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()
}

test('stats dashboard renders cards, top tracks, timeline, heatmap; range change refetches', async ({ page }) => {
  const summaryFroms: number[] = []
  await bootStats(page, summaryFroms)

  // Summary cards: total plays + distinct counts render.
  await expect(page.getByText('42', { exact: false })).toBeVisible() // plays
  await expect(page.getByText('2h 30m', { exact: false })).toBeVisible() // listening time

  // Top tracks list rendered with a seeded title.
  await expect(page.getByText('Karma Police').first()).toBeVisible()

  // Timeline chart + heatmap rendered (sections present).
  await expect(page.locator('svg').first()).toBeVisible()

  // The initial range fetched summary once.
  expect(summaryFroms.length).toBeGreaterThanOrEqual(1)
  const initialCount = summaryFroms.length
  const initialFrom = summaryFroms[summaryFroms.length - 1]

  // Switch the range to "7 days" → a NEW summary fetch with a DIFFERENT (later) from.
  const waitSummary = page.waitForRequest('**/api/v1/stats/summary**')
  await page.getByRole('button', { name: /7 days|7d|Last 7/i }).first().click()
  await waitSummary
  await expect.poll(() => summaryFroms.length).toBeGreaterThan(initialCount)
  expect(summaryFroms[summaryFroms.length - 1]).not.toBe(initialFrom)
})
