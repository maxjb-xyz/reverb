import { useState } from 'react'
import { api, loginErrorMessage } from '../lib/api'
import { Button } from '../components/ui/Button'
import { Icon } from '../components/ui/Icon'

export default function Login() {
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    setLoading(true)
    try {
      await api.post('/auth/login', { password: pw })
      window.location.reload()
    } catch (err) {
      setErr(loginErrorMessage(err))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-canvas flex items-center justify-center">
      {/* Subtle radial wash behind the card */}
      <div
        className="pointer-events-none fixed inset-0"
        aria-hidden="true"
        style={{ background: 'radial-gradient(ellipse 70% 50% at 50% 0%, rgb(var(--color-accent)/0.12) 0%, transparent 70%)' }}
      />

      <div className="relative w-full max-w-sm mx-4">
        {/* Wordmark */}
        <div className="mb-8 flex items-center justify-center select-none">
          <span className="text-2xl font-bold tracking-tight text-text-primary">
            Reverb<span className="text-accent">.</span>
          </span>
        </div>

        {/* Card */}
        <div className="rounded-2xl bg-surface shadow-pop border border-border-subtle p-8 space-y-5">
          <div className="space-y-1">
            <h1 className="text-xl font-bold text-text-primary">Welcome back</h1>
            <p className="text-sm text-text-secondary">Sign in with your admin password.</p>
          </div>

          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-1">
              <label htmlFor="login-pw" className="block text-sm font-medium text-text-secondary">
                Password
              </label>
              <input
                id="login-pw"
                type="password"
                value={pw}
                onChange={(e) => setPw(e.target.value)}
                placeholder="Admin password"
                autoComplete="current-password"
                className="w-full rounded-lg bg-input border border-border-subtle px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
            </div>

            {err && (
              <p className="text-sm text-accent flex items-center gap-1.5" role="alert">
                <Icon name="warn" className="shrink-0 text-base" aria-hidden="true" />
                {err}
              </p>
            )}

            <Button type="submit" variant="primary" size="md" disabled={loading} aria-label="Log in">
              <span className="w-full text-center">{loading ? 'Logging in...' : 'Log in'}</span>
            </Button>
          </form>
        </div>
      </div>
    </div>
  )
}
