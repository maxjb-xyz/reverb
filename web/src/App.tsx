import { useEffect, lazy, type ReactNode } from 'react'
import { Navigate, Route, Routes, useParams } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from './components/AppShell'
import { ApiError } from './lib/api'
import { useSessionStatus } from './lib/session'
import { useAuthStore, isManagerCaps } from './lib/authStore'
// Eager: the auth-gate screens (rendered before the shell / before auth), so
// there's no benefit to code-splitting them behind a Suspense fallback.
import Login from './routes/Login'
import Setup from './routes/Setup'
import Signup from './routes/Signup'
import { Button } from './components/ui/Button'
import { Logo } from './components/ui/Logo'

/** Full-page state shown before the app shell exists: the wordmark, centred,
 *  with either a quiet loading line or a message and action. */
function BootScreen({ children }: { children?: ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-canvas p-6 text-center">
      <Logo textClassName="text-3xl" />
      {children ?? (
        <p className="text-sm text-text-muted" role="status">
          Loading…
        </p>
      )}
    </div>
  )
}

// Lazy: the heavy authenticated routes. Splitting these out of the main bundle
// keeps the initial (login) payload small and clears the build size warning.
const Home = lazy(() => import('./routes/Home'))
const Search = lazy(() => import('./routes/Search'))
const Library = lazy(() => import('./routes/Library'))
const Settings = lazy(() => import('./routes/Settings'))
const Album = lazy(() => import('./routes/Album'))
const Artist = lazy(() => import('./routes/Artist'))
const Admin = lazy(() => import('./routes/Admin'))
const Downloads = lazy(() => import('./routes/Downloads'))
const Requests = lazy(() => import('./routes/Requests'))
const SyncedPlaylist = lazy(() => import('./routes/SyncedPlaylist'))
const Account = lazy(() => import('./routes/Account'))
const Stats = lazy(() => import('./routes/Stats'))
const ExternalPlaylist = lazy(() => import('./routes/ExternalPlaylist'))
const Collection = lazy(() => import('./routes/Collection'))

/** Redirect bare `/album/:id` or `/artist/:id` URLs to the source-qualified form
 *  `/album/library/:id` / `/artist/library/:id`. These old URLs may exist in
 *  bookmarks or nav links written before the source segment was introduced. */
function RedirectToLibrary({ kind }: { kind: 'album' | 'artist' }) {
  const { id = '' } = useParams()
  return <Navigate to={`/${kind}/library/${id}`} replace />
}

/** Redirect legacy `/synced-playlist/:id` URLs to the canonical `/playlist/:id` form. */
function RedirectToPlaylist() {
  const { id = '' } = useParams()
  return <Navigate to={`/playlist/${id}`} replace />
}

/** Route guard: renders children only for a "manager" (any management capability),
 *  otherwise redirects to Home. Defense-in-depth — the backend enforces this too. */
function RequireManager({ children }: { children: React.ReactNode }) {
  const isManager = useAuthStore((s) => isManagerCaps(s.me?.capabilities))
  if (!isManager) return <Navigate to="/" replace />
  return <>{children}</>
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      // Don't hammer endpoints that fail deterministically: 4xx (e.g. 401) and
      // the library's 503 "no library configured" won't change on retry. Other
      // 5xx may be transient, so retry those once.
      retry: (failureCount, error) => {
        if (error instanceof ApiError && (error.status === 503 || (error.status >= 400 && error.status < 500))) {
          return false
        }
        return failureCount < 1
      },
    },
  },
})

function Routed() {
  const s = useSessionStatus()
  const me = useAuthStore((st) => st.me)
  const refresh = useAuthStore((st) => st.refresh)

  // Boot-hydrate the auth store once we know the session is authenticated, so
  // `can()` is populated app-wide. useSessionStatus remains the top-level
  // routing decision (it correctly handles the 5xx-vs-401 distinction); this
  // only fills in the capability detail for gating.
  useEffect(() => {
    if (s.authenticated && !me) void refresh()
  }, [s.authenticated, me, refresh])

  if (s.loading) return <BootScreen />
  if (s.error)
    return (
      <BootScreen>
        <p className="text-base font-semibold text-text-primary">Can't reach the Reverb server</p>
        <p className="max-w-xs text-sm text-text-secondary">
          It may still be starting up. Check that it is running, then try again.
        </p>
        <Button variant="secondary" onClick={() => window.location.reload()}>
          Retry
        </Button>
      </BootScreen>
    )
  if (s.setupRequired) return <Setup />
  if (!s.authenticated)
    return (
      <Routes>
        <Route path="/signup" element={<Signup />} />
        <Route path="*" element={<Login />} />
      </Routes>
    )
  // Authenticated, but capabilities not yet loaded — render a brief loading
  // state rather than flashing an ungated shell (the gates depend on `me`).
  if (!me) return <BootScreen />
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route path="/" element={<Home />} />
        <Route path="/search" element={<Search />} />
        <Route path="/library" element={<Library />} />
        <Route path="/collection" element={<Collection />} />
        <Route path="/album/:source/:id" element={<Album />} />
        <Route path="/album/:id" element={<RedirectToLibrary kind="album" />} />
        <Route path="/artist/:source/:id" element={<Artist />} />
        <Route path="/artist/:id" element={<RedirectToLibrary kind="artist" />} />
        <Route path="/playlist/:id" element={<SyncedPlaylist />} />
        <Route path="/playlist/:source/:id" element={<ExternalPlaylist />} />
        <Route path="/synced-playlist/:id" element={<RedirectToPlaylist />} />
        <Route path="/stats" element={<Stats />} />
        <Route path="/account" element={<Account />} />
        <Route path="/settings" element={<Settings />} />
        <Route
          path="/admin"
          element={
            <RequireManager>
              <Admin />
            </RequireManager>
          }
        />
        <Route path="/downloads" element={<Downloads />} />
        <Route path="/requests" element={<Requests />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <Routed />
    </QueryClientProvider>
  )
}
