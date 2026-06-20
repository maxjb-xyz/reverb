import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { useSessionStatus } from './lib/session'
import Search from './routes/Search'
import Library from './routes/Library'
import Settings from './routes/Settings'
import Login from './routes/Login'
import Setup from './routes/Setup'

export default function App() {
  const s = useSessionStatus()
  if (s.loading) return <div className="p-6 text-neutral-500">Loading…</div>
  if (s.setupRequired) return <Setup />
  if (!s.authenticated) return <Login />
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route path="/search" element={<Search />} />
        <Route path="/library" element={<Library />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/search" replace />} />
      </Route>
    </Routes>
  )
}
