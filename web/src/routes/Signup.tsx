import { useState } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { signup } from '../lib/session'
import { ApiError } from '../lib/api'
import { Button } from '../components/ui/Button'
import { Icon } from '../components/ui/Icon'
import { Logo } from '../components/ui/Logo'

function signupErrorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 403) return "Registration isn't open — ask an admin for an invite link."
    if (e.status === 409) return 'That username is already taken.'
  }
  return "Can't reach the server — try again."
}

export default function Signup() {
  const [searchParams] = useSearchParams()
  const invite = searchParams.get('invite') ?? undefined

  const [username, setUsername] = useState('')
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    setLoading(true)
    try {
      await signup(username, pw, invite)
      window.location.assign('/')
    } catch (e) {
      setErr(signupErrorMessage(e))
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
            <h1 className="text-xl font-bold text-text-primary">Create an account</h1>
            <p className="text-sm text-text-secondary">
              {invite ? 'You have an invite — choose a username and password.' : 'Choose a username and password to get started.'}
            </p>
          </div>

          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-1">
              <label htmlFor="signup-username" className="block text-sm font-medium text-text-secondary">
                Username
              </label>
              <input
                id="signup-username"
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="Choose a username"
                autoComplete="username"
                className="w-full rounded-lg bg-input border border-border-subtle px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
            </div>

            <div className="space-y-1">
              <label htmlFor="signup-pw" className="block text-sm font-medium text-text-secondary">
                Password
              </label>
              <input
                id="signup-pw"
                type="password"
                value={pw}
                onChange={(e) => setPw(e.target.value)}
                placeholder="Choose a password"
                autoComplete="new-password"
                className="w-full rounded-lg bg-input border border-border-subtle px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
            </div>

            {err && (
              <p className="text-sm text-accent flex items-center gap-1.5" role="alert">
                <Icon name="warn" className="shrink-0 text-base" aria-hidden="true" />
                {err}
              </p>
            )}

            <Button type="submit" variant="primary" size="md" disabled={loading} aria-label="Create account">
              <span className="w-full text-center">{loading ? 'Creating account…' : 'Create account'}</span>
            </Button>
          </form>

          <p className="text-center text-sm text-text-secondary">
            Already have an account?{' '}
            <Link to="/" className="text-accent hover:underline">
              Sign in
            </Link>
          </p>
        </div>
      </div>
    </div>
  )
}
