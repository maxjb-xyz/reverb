import { useState } from 'react'
import { Button, Chip } from '../components/ui'
import { useAuthStore } from '../lib/authStore'
import { changePassword, logoutAll } from '../lib/accountApi'
import { ApiError } from '../lib/api'
import { IntegrationsSection } from '../components/account/IntegrationsSection'
import { AppearanceSection } from '../components/account/AppearanceSection'

type Tab = 'profile' | 'security' | 'sessions' | 'integrations' | 'appearance'

export default function Settings() {
  const me = useAuthStore((s) => s.me)
  const logout = useAuthStore((s) => s.logout)

  const [tab, setTab] = useState<Tab>('profile')

  // ── Change password form ──────────────────────────────────────────────────
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [pwError, setPwError] = useState<string | null>(null)
  const [pwSuccess, setPwSuccess] = useState(false)
  const [pwPending, setPwPending] = useState(false)

  async function handleChangePassword(e: React.FormEvent) {
    e.preventDefault()
    setPwError(null)
    setPwSuccess(false)

    if (next !== confirm) {
      setPwError('Passwords do not match')
      return
    }

    setPwPending(true)
    try {
      await changePassword(current, next)
      setPwSuccess(true)
      setCurrent('')
      setNext('')
      setConfirm('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 400) {
        setPwError('Current password is incorrect')
      } else {
        setPwError('Failed to change password — please try again')
      }
    } finally {
      setPwPending(false)
    }
  }

  if (!me) return null

  const initial = me.username.charAt(0).toUpperCase()

  return (
    <div className="max-w-4xl space-y-6 pb-8">
      {/* Header */}
      <h1 className="text-3xl font-black tracking-tight text-text-primary">Settings</h1>

      {/* Tab bar */}
      <div className="flex gap-2 border-b border-border-subtle pb-0 flex-wrap">
        <Chip selected={tab === 'profile'} onClick={() => setTab('profile')}>
          Profile
        </Chip>
        <Chip selected={tab === 'security'} onClick={() => setTab('security')}>
          Security
        </Chip>
        <Chip selected={tab === 'sessions'} onClick={() => setTab('sessions')}>
          Sessions
        </Chip>
        <Chip selected={tab === 'integrations'} onClick={() => setTab('integrations')}>
          Integrations
        </Chip>
        <Chip selected={tab === 'appearance'} onClick={() => setTab('appearance')}>
          Appearance
        </Chip>
      </div>

      {/* ── Profile tab ─────────────────────────────────────────────────────── */}
      {tab === 'profile' && (
        <div className="space-y-0 divide-y divide-border-subtle">
          {/* Avatar + username + role */}
          <div className="flex items-center gap-5 py-5">
            <div
              className="w-12 h-12 rounded-full bg-accent flex items-center justify-center text-on-accent font-extrabold text-lg flex-none"
              aria-hidden="true"
            >
              {initial}
            </div>
            <div className="flex-1 min-w-0">
              <div className="text-sm font-bold text-text-primary">{me.username}</div>
              <div className="flex items-center gap-2 mt-1">
                <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-semibold bg-accent text-on-accent">
                  {me.roleName}
                </span>
                {!!me.createdAt && (
                  <span className="text-xs text-text-secondary">
                    Member since{' '}
                    {new Date(me.createdAt * 1000).toLocaleDateString(undefined, {
                      year: 'numeric',
                      month: 'long',
                      day: 'numeric',
                    })}
                  </span>
                )}
              </div>
            </div>
          </div>

          {/* Capabilities */}
          {me.capabilities.length > 0 && (
            <div className="flex items-start gap-5 py-5">
              <div className="flex-1 min-w-0">
                <div className="text-sm font-bold text-text-primary">Capabilities</div>
                <div className="text-xs text-text-secondary mt-0.5">
                  Permissions granted to your role.
                </div>
              </div>
              <div className="flex-none flex flex-wrap gap-1.5 max-w-xs justify-end">
                {me.capabilities.map((cap) => (
                  <span
                    key={cap}
                    className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-surface-raised text-text-secondary border border-border-subtle"
                  >
                    {cap}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* ── Security tab ─────────────────────────────────────────────────────── */}
      {tab === 'security' && (
        <div className="space-y-0 divide-y divide-border-subtle">
          <div className="py-5">
            <div className="text-sm font-bold text-text-primary mb-4">Change password</div>
            <form onSubmit={(e) => void handleChangePassword(e)} className="space-y-3 max-w-sm">
              <div>
                <label
                  htmlFor="current-password"
                  className="block text-xs font-semibold text-text-secondary mb-1"
                >
                  Current password
                </label>
                <input
                  id="current-password"
                  type="password"
                  autoComplete="current-password"
                  value={current}
                  onChange={(e) => setCurrent(e.target.value)}
                  required
                  className="w-full h-9 px-3 rounded-md bg-input text-sm text-text-primary border border-border-subtle focus:outline-none focus:ring-2 focus:ring-accent"
                />
              </div>

              <div>
                <label
                  htmlFor="new-password"
                  className="block text-xs font-semibold text-text-secondary mb-1"
                >
                  New password
                </label>
                <input
                  id="new-password"
                  type="password"
                  autoComplete="new-password"
                  value={next}
                  onChange={(e) => setNext(e.target.value)}
                  required
                  className="w-full h-9 px-3 rounded-md bg-input text-sm text-text-primary border border-border-subtle focus:outline-none focus:ring-2 focus:ring-accent"
                />
              </div>

              <div>
                <label
                  htmlFor="confirm-password"
                  className="block text-xs font-semibold text-text-secondary mb-1"
                >
                  Confirm new password
                </label>
                <input
                  id="confirm-password"
                  type="password"
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                  required
                  className="w-full h-9 px-3 rounded-md bg-input text-sm text-text-primary border border-border-subtle focus:outline-none focus:ring-2 focus:ring-accent"
                />
              </div>

              {pwError && (
                <p className="text-xs font-medium text-error" role="alert">
                  {pwError}
                </p>
              )}

              {pwSuccess && (
                <p className="text-xs font-medium text-success">
                  Password changed successfully.
                </p>
              )}

              <Button type="submit" variant="secondary" disabled={pwPending}>
                {pwPending ? 'Saving…' : 'Change password'}
              </Button>
            </form>
          </div>
        </div>
      )}

      {/* ── Sessions tab ─────────────────────────────────────────────────────── */}
      {tab === 'sessions' && (
        <div className="space-y-0 divide-y divide-border-subtle">
          <div className="flex items-center gap-5 py-5">
            <div className="flex-1 min-w-0">
              <div className="text-sm font-bold text-text-primary">Current session</div>
              <div className="text-xs text-text-secondary mt-0.5">
                Sign out of Reverb on this device.
              </div>
            </div>
            <div className="flex-none">
              <Button variant="secondary" onClick={() => void logout()}>
                Sign out
              </Button>
            </div>
          </div>

          <div className="flex items-center gap-5 py-5">
            <div className="flex-1 min-w-0">
              <div className="text-sm font-bold text-text-primary">All other sessions</div>
              <div className="text-xs text-text-secondary mt-0.5">
                Sign out of Reverb on every other device. Your current session stays active.
              </div>
            </div>
            <div className="flex-none">
              <Button variant="secondary" onClick={() => void logoutAll()}>
                Sign out everywhere
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* ── Integrations tab ─────────────────────────────────────────────────── */}
      {tab === 'integrations' && <IntegrationsSection />}

      {/* ── Appearance tab ───────────────────────────────────────────────────── */}
      {tab === 'appearance' && <AppearanceSection />}
    </div>
  )
}
