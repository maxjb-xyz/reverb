import { useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Button } from '../components/ui'
import { useAuthStore, isManagerCaps } from '../lib/authStore'
import { changePassword, changeUsername, logoutAll } from '../lib/accountApi'
import { ApiError } from '../lib/api'
import { IntegrationsSection } from '../components/account/IntegrationsSection'
import { AppearanceSection } from '../components/account/AppearanceSection'
import { PlaybackSection } from '../components/account/PlaybackSection'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { capabilityLabel } from '../lib/displayNames'
import { PageHeader } from '../components/ui/PageHeader'
import { AdminProviders } from './Admin'
import { UsersSection } from '../components/admin/UsersSection'
import { RolesSection } from '../components/admin/RolesSection'
import { RegistrationSection } from '../components/admin/RegistrationSection'

type Section =
  | 'profile'
  | 'security'
  | 'sessions'
  | 'integrations'
  | 'appearance'
  | 'playback'
  | 'providers'
  | 'users'
  | 'registration'

// One place for every setting. The first group is the signed-in person's own
// account; the second is the server, shown only to people who can manage it
// (this used to be a separate /admin page).
const ACCOUNT_SECTIONS: { key: Section; label: string }[] = [
  { key: 'profile', label: 'Profile' },
  { key: 'security', label: 'Security' },
  { key: 'sessions', label: 'Sessions' },
  { key: 'integrations', label: 'Integrations' },
  { key: 'appearance', label: 'Appearance' },
  { key: 'playback', label: 'Playback' },
]
const ADMIN_SECTIONS: { key: Section; label: string }[] = [
  { key: 'providers', label: 'Providers' },
  { key: 'users', label: 'Users and roles' },
  { key: 'registration', label: 'Registration' },
]

export default function Settings() {
  useDocumentTitle('Settings')
  const me = useAuthStore((s) => s.me)
  const logout = useAuthStore((s) => s.logout)
  const refresh = useAuthStore((s) => s.refresh)

  const isManager = useAuthStore((s) => isManagerCaps(s.me?.capabilities))
  const navigate = useNavigate()
  // The section lives in the URL (/settings/security) so it can be linked to
  // and survives a reload. Unknown or not-permitted sections fall back to Profile.
  const requested = useLocation().pathname.split('/')[2] ?? 'profile'
  const allowed = isManager ? [...ACCOUNT_SECTIONS, ...ADMIN_SECTIONS] : ACCOUNT_SECTIONS
  const tab: Section = allowed.find((x) => x.key === requested)?.key ?? 'profile'

  // ── Profile form ──────────────────────────────────────────────────────────
  const [username, setUsername] = useState(me?.username ?? '')
  const [profileError, setProfileError] = useState<string | null>(null)
  const [profileSuccess, setProfileSuccess] = useState(false)
  const [profilePending, setProfilePending] = useState(false)

  async function handleChangeUsername(e: React.FormEvent) {
    e.preventDefault()
    const nextUsername = username.trim()
    setProfileError(null)
    setProfileSuccess(false)
    if (!nextUsername) {
      setProfileError('Username is required')
      return
    }
    if (nextUsername === me?.username) return

    setProfilePending(true)
    try {
      await changeUsername(nextUsername)
      await refresh()
      setUsername(nextUsername)
      setProfileSuccess(true)
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setProfileError('That username is already in use')
      } else {
        setProfileError('Failed to update profile — please try again')
      }
    } finally {
      setProfilePending(false)
    }
  }

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

  function renderGroup(label: string, items: { key: Section; label: string }[]) {
    return (
      <>
        <div className="hidden px-3 pb-1 pt-4 text-xs font-semibold uppercase tracking-widest text-text-muted first:pt-0 md:block">
          {label}
        </div>
        {items.map((item) => {
          const selected = tab === item.key
          return (
            <button
              key={item.key}
              type="button"
              aria-current={selected ? 'page' : undefined}
              onClick={() => navigate(item.key === 'profile' ? '/settings' : `/settings/${item.key}`)}
              className={[
                'whitespace-nowrap rounded-full px-3 py-1.5 text-left text-sm font-semibold transition-colors md:rounded-md md:py-2',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
                selected
                  ? 'bg-text-primary text-on-accent md:bg-raised-hover md:text-text-primary'
                  : 'bg-raised text-text-primary hover:bg-raised-hover md:bg-transparent md:text-text-secondary md:hover:text-text-primary',
              ].join(' ')}
            >
              {item.label}
            </button>
          )
        })}
      </>
    )
  }

  const initial = me.username.charAt(0).toUpperCase()

  return (
    <div className="mx-auto max-w-5xl space-y-6 pb-8">
      <PageHeader title="Settings" />

      <div className="gap-10 md:grid md:grid-cols-[190px_minmax(0,1fr)]">
        {/* Section list: a column on desktop, a scrolling row of pills on phones */}
        <nav aria-label="Settings sections" className="mb-6 md:mb-0">
          <div className="-mx-4 flex gap-2 overflow-x-auto px-4 pb-1 md:mx-0 md:flex-col md:gap-0.5 md:overflow-visible md:px-0 md:pb-0">
            {renderGroup('Account', ACCOUNT_SECTIONS)}
            {isManager && renderGroup('Admin', ADMIN_SECTIONS)}
          </div>
        </nav>

        <div className="min-w-0">
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

          <div className="py-5">
            <div className="text-sm font-bold text-text-primary mb-1">Username</div>
            <p className="text-xs text-text-secondary mb-4">Used to sign in to Reverb.</p>
            <form onSubmit={(e) => void handleChangeUsername(e)} className="flex flex-wrap items-start gap-3 max-w-md">
              <div className="flex-1 min-w-52">
                <label htmlFor="profile-username" className="sr-only">Username</label>
                <input
                  id="profile-username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  required
                  className="w-full h-9 px-3 rounded-md bg-input text-sm text-text-primary border border-border-subtle focus:outline-none focus:ring-2 focus:ring-accent"
                />
                {profileError && <p className="mt-2 text-xs font-medium text-error" role="alert">{profileError}</p>}
                {profileSuccess && <p className="mt-2 text-xs font-medium text-success">Username updated.</p>}
              </div>
              <Button type="submit" variant="secondary" disabled={profilePending || username.trim() === me.username}>
                {profilePending ? 'Saving…' : 'Save username'}
              </Button>
            </form>
          </div>

          {/* Capabilities */}
          {me.capabilities.length > 0 && (
            <div className="flex flex-col gap-3 py-5 sm:flex-row sm:items-start sm:gap-5">
              <div className="flex-1 min-w-0">
                <div className="text-sm font-bold text-text-primary">Capabilities</div>
                <div className="text-xs text-text-secondary mt-0.5">
                  Permissions granted to your role.
                </div>
              </div>
              <div className="flex flex-wrap gap-1.5 sm:max-w-xs sm:flex-none sm:justify-end">
                {me.capabilities.map((cap) => (
                  <span
                    key={cap}
                    className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-raised text-text-secondary border border-border-subtle"
                  >
                    {capabilityLabel(cap)}
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

      {tab === 'playback' && <PlaybackSection />}

      {/* ── Admin sections ───────────────────────────────────────────────────── */}
      {tab === 'providers' && <AdminProviders />}
      {tab === 'users' && (
        <div className="space-y-10">
          <UsersSection />
          <RolesSection />
        </div>
      )}
      {tab === 'registration' && <RegistrationSection />}
        </div>
      </div>
    </div>
  )
}
