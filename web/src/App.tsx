import { useEffect, lazy } from 'react'
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

  if (s.loading) return <div className="p-6 text-text-muted">Loading…</div>
  if (s.error)
    return (
      <div className="p-6 text-text-secondary">
        Can't reach the Reverb server.{' '}
        <button onClick={() => window.location.reload()} className="underline">
          Retry
        </button>
      </div>
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
  if (!me) return <div className="p-6 text-text-muted">Loading…</div>
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route path="/" element={<Home />} />
        <Route path="/search" element={<Search />} />
        <Route path="/library" element={<Library />} />
        <Route path="/album/:source/:id" element={<Album />} />
        <Route path="/album/:id" element={<RedirectToLibrary kind="album" />} />
        <Route path="/artist/:source/:id" element={<Artist />} />
        <Route path="/artist/:id" element={<RedirectToLibrary kind="artist" />} />
        <Route path="/playlist/:id" element={<SyncedPlaylist />} />
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
