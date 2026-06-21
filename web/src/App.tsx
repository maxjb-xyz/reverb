import { Navigate, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from './components/AppShell'
import { useSessionStatus } from './lib/session'
import Search from './routes/Search'
import Library from './routes/Library'
import Settings from './routes/Settings'
import Login from './routes/Login'
import Setup from './routes/Setup'
import Album from './routes/Album'
import Artist from './routes/Artist'

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000, retry: 1 } },
})

function Routed() {
  const s = useSessionStatus()
  if (s.loading) return <div className="p-6 text-neutral-500">Loading…</div>
  if (s.error)
    return (
      <div className="p-6 text-neutral-400">
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
        <Route path="/search" element={<Search />} />
        <Route path="/library" element={<Library />} />
        <Route path="/album/:id" element={<Album />} />
        <Route path="/artist/:id" element={<Artist />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/search" replace />} />
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
