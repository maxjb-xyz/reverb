import { Navigate, Route, Routes, useParams } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from './components/AppShell'
import { ApiError } from './lib/api'
import { useSessionStatus } from './lib/session'
import Search from './routes/Search'
import Library from './routes/Library'
import Settings from './routes/Settings'
import Login from './routes/Login'
import Setup from './routes/Setup'
import Album from './routes/Album'
import Artist from './routes/Artist'
import Home from './routes/Home'
import Admin from './routes/Admin'
import SyncedPlaylist from './routes/SyncedPlaylist'

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
  if (!s.authenticated) return <Login />
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
        <Route path="/settings" element={<Settings />} />
        <Route path="/admin" element={<Admin />} />
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
