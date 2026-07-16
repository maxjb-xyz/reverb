import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { loginErrorMessage, api } from '../lib/api'
import { login } from '../lib/session'
import { Button } from '../components/ui/Button'
import { Icon } from '../components/ui/Icon'
import { Logo } from '../components/ui/Logo'

export default function Login() {
  const [username, setUsername] = useState('')
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)
  const [canSignup, setCanSignup] = useState(false)

  useEffect(() => {
    api.get<{ signupEnabled: boolean; invitesEnabled: boolean }>('/auth/registration-status')
      .then((s) => setCanSignup(s.signupEnabled || s.invitesEnabled))
      .catch(() => { /* silently ignore — no link shown on failure */ })
  }, [])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    setLoading(true)
    try {
      await login(username, pw)
      window.location.assign('/')
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
        <div className="mb-8 flex items-center justify-center">
          <Logo iconClassName="h-9 w-auto" textClassName="text-2xl" />
        </div>

        {/* Card */}
        <div className="rounded-2xl bg-surface shadow-pop border border-border-subtle p-8 space-y-5">
          <div className="space-y-1">
            <h1 className="text-xl font-bold text-text-primary">Welcome back</h1>
            <p className="text-sm text-text-secondary">Sign in with your username and password.</p>
          </div>

          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-1">
              <label htmlFor="login-username" className="block text-sm font-medium text-text-secondary">
                Username
              </label>
              <input
                id="login-username"
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="Username"
                autoComplete="username"
                required
                className="w-full rounded-lg bg-input border border-border-subtle px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
            </div>

            <div className="space-y-1">
              <label htmlFor="login-pw" className="block text-sm font-medium text-text-secondary">
                Password
              </label>
              <input
                id="login-pw"
                type="password"
                value={pw}
                onChange={(e) => setPw(e.target.value)}
                placeholder="Password"
                autoComplete="current-password"
                required
                className="w-full rounded-lg bg-input border border-border-subtle px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
            </div>

            {err && (
              <p className="text-sm text-error flex items-center gap-1.5" role="alert">
                <Icon name="warn" className="shrink-0 text-base" aria-hidden="true" />
                {err}
              </p>
            )}

            <Button type="submit" variant="primary" size="md" disabled={loading} aria-label="Log in" className="w-full">
              {loading ? 'Logging in...' : 'Log in'}
            </Button>
          </form>

          {canSignup && (
            <p className="text-center text-sm text-text-secondary">
              Don't have an account?{' '}
              <Link to="/signup" className="text-accent hover:underline">
                Create an account
              </Link>
            </p>
          )}
        </div>
      </div>
    </div>
  )
}
