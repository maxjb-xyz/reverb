# Task 5 Report: Remove frontend downloader picker

## Deleted files
- `web/src/components/download/DownloadPopover.tsx` — picker popover component
- `web/src/components/download/DownloadPopover.test.tsx` — 10 tests, all deleted with the component

## DownloadAction.tsx picker removal
**Removed:**
- `import { DownloadPopover }` and `import { useSettings }` (both unused after cleanup)
- `popoverOpen` state and `defaultDownloader` derived var
- `handlePick(name)` function
- `handleDownloadClick` split-button logic (≥2 downloaders → popover or default) — replaced with single-line: always pick `downloaders[0].name` (highest priority)
- Caret button (`aria-label="Choose downloader"`) and `<DownloadPopover ... />` render in branch 6
- `hasActiveDefault` computed var

**Stayed untouched:** in-library Play state, `auto_approve` Download button (just minus caret/popover), Request/Requested affordance (Task 8), queued/running/completed/failed states, failed-state link modal, Lidarr album disclosure dialog, `useDownloaders()` hook (still needed for branch 5 "No downloader" and for picking the first downloader on click).

## settingsApi.ts changes
Removed `defaultDownloader: string` from `AppSettings` interface.

## Settings.tsx changes
- Removed the "Default downloader" row (div + Select element) — ~17 lines of JSX
- Removed `downloaderOptions` array and the `downloaders` derived var
- Removed `useAdapters` import (now unused)
- Removed `Select` from the `Chip, Toggle, Select` import (now unused)

## Tests changed

### DownloadAction.test.tsx
| # | Test | Change |
|---|------|--------|
| mock | `useSettings` mock | Dropped `defaultDownloader: 'spotdl'` from mock data |
| 8 | "2 downloaders → opens popover" | Replaced: now asserts no caret button, Download click calls `postDownloadMock`, no dialog |
| 19 | "default downloader: normal click enqueues spotdl directly" | Replaced: asserts no caret, highest-priority downloader (spotdl priority 1) is picked |
| 20 | "override caret → pick Lidarr → album disclosure → confirm enqueues lidarr" | Replaced: lidarr configured as the only (priority 1) downloader, Download click routes through Lidarr album disclosure without any caret/popover step |

### Settings.test.tsx
| # | Test | Change |
|---|------|--------|
| adaptersApi mock | `vi.mock('../lib/adaptersApi', ...)` | Removed entirely (Settings no longer imports adaptersApi) |
| "Settings default downloader" describe | "shows a Default downloader select and saves the choice" | Replaced: asserts `queryByLabelText('Default downloader')` and `queryByText(/default downloader/i)` both return null |

## TDD RED/GREEN cycle
**RED:** Wrote/updated 4 tests in DownloadAction.test.tsx and 1 test in Settings.test.tsx before touching any production code. Confirmed 3 tests failing (Settings still showed the select; DownloadAction tests 8 and 19 — postDownloadMock not called since popover was still opening).

**GREEN:** Implemented changes. All 815 tests pass.

## Gate results
- `npx vitest run`: PASS (815) FAIL (0) — 10 DownloadPopover tests deleted, 822→815 net
- `npx tsc --noEmit`: No errors found
- `npm run build`: ✓ built in 512ms, 158 modules

## Self-review
- All non-picker branches of DownloadAction verified untouched: in-library Play, queued, running, completed, failed+retry, failed+link modal, Lidarr album disclosure, Request/Requested, no-downloaders badge
- The `reqFromResult` mock in the test still passes a `downloader` field through — that's fine, it's the mock helper; the actual `reqFromResult` in `downloadApi.ts` is unchanged (backend concern)
- Settings test `useAdapters` mock removed since Settings no longer imports it; no orphan mock noise

## Concerns
None — clean removal with no behavioral regressions. The Lidarr disclosure dialog still works for single-downloader setups where lidarr is the configured one. With multiple downloaders, chains (backend) now own the routing decision.
