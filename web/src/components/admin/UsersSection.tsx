import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Button, EmptyState, Select, Skeleton } from '../ui'
import {
  useUsers,
  useRoles,
  createUser,
  updateUser,
  deleteUser,
  resetPassword,
  type User,
  type CreateUserReq,
} from '../../lib/usersApi'

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatDate(unixSec: number | null): string {
  if (unixSec == null) return '—'
  return new Date(unixSec * 1000).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

// ── Lock icon (inline SVG, no dependency on Icon registry) ───────────────────

function LockIcon() {
  return (
    <svg
      data-testid="owner-lock"
      aria-label="Owner — protected"
      viewBox="0 0 24 24"
      width="14"
      height="14"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="text-text-muted shrink-0"
    >
      <rect x="3" y="11" width="18" height="11" rx="2" />
      <path d="M7 11V7a5 5 0 0 1 10 0v4" />
    </svg>
  )
}

// ── Create User Modal ─────────────────────────────────────────────────────────

const FOCUSABLE = 'button, input, select, [tabindex]:not([tabindex="-1"])'

interface CreateUserModalProps {
  roles: { id: string; name: string }[]
  onClose: () => void
  onCreated: () => void
}

function CreateUserModal({ roles, onClose, onCreated }: CreateUserModalProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [roleId, setRoleId] = useState(roles[0]?.id ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Focus trap + Esc
  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null
    const panel = panelRef.current
    if (panel) {
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE))
      focusable[0]?.focus()
    }

    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (e.key === 'Tab' && panelRef.current) {
        const focusable = Array.from(
          panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE),
        ).filter((el) => !el.hasAttribute('disabled'))
        if (focusable.length === 0) return
        const first = focusable[0]
        const last = focusable[focusable.length - 1]
        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault()
            last.focus()
          }
        } else if (document.activeElement === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }

    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('keydown', handleKey)
      previouslyFocused?.focus()
    }
  }, [onClose])

  async function handleSubmit() {
    const trimmedUsername = username.trim()
    const trimmedPassword = password.trim()
    if (!trimmedUsername || !trimmedPassword || !roleId || busy) return
    setBusy(true)
    setError(null)
    const body: CreateUserReq = { username: trimmedUsername, password: trimmedPassword, roleId }
    try {
      await createUser(body)
      onCreated()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create user')
      setBusy(false)
    }
  }

  const canSubmit = username.trim() !== '' && password.trim() !== '' && roleId !== '' && !busy

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 z-40 bg-canvas/80 backdrop-blur-sm"
        aria-hidden="true"
        onClick={onClose}
      />

      {/* Dialog panel */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="create-user-dialog-title"
        className="fixed inset-0 z-50 flex items-center justify-center p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="w-full max-w-sm rounded-xl border border-border-subtle bg-raised shadow-pop animate-scale-in">
          <div className="space-y-5 p-6">
            <h2
              id="create-user-dialog-title"
              className="text-lg font-extrabold tracking-tight text-text-primary"
            >
              Create user
            </h2>

            {/* Username */}
            <div className="space-y-1.5">
              <label htmlFor="new-username" className="block text-sm font-semibold text-text-primary">
                Username
              </label>
              <input
                id="new-username"
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                disabled={busy}
                autoComplete="off"
                className="w-full rounded-lg border border-border-subtle bg-input px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
              />
            </div>

            {/* Password */}
            <div className="space-y-1.5">
              <label htmlFor="new-password" className="block text-sm font-semibold text-text-primary">
                Password
              </label>
              <input
                id="new-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') void handleSubmit()
                }}
                disabled={busy}
                autoComplete="new-password"
                className="w-full rounded-lg border border-border-subtle bg-input px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
              />
            </div>

            {/* Role */}
            <div className="space-y-1.5">
              <label htmlFor="new-role" className="block text-sm font-semibold text-text-primary">
                Role
              </label>
              <Select
                label="Role"
                value={roleId}
                options={roles.map((r) => ({ value: r.id, label: r.name }))}
                onChange={(v) => setRoleId(v)}
              />
            </div>

            {/* Inline error */}
            {error && (
              <p role="alert" className="text-sm text-error">
                {error}
              </p>
            )}

            {/* Actions */}
            <div className="flex items-center justify-end gap-3 pt-1">
              <Button variant="ghost" onClick={onClose}>
                Cancel
              </Button>
              <Button
                variant="primary"
                disabled={!canSubmit}
                onClick={() => void handleSubmit()}
                aria-label={busy ? 'Creating…' : 'Create'}
              >
                {busy ? 'Creating…' : 'Create'}
              </Button>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}

// ── Delete User Modal ─────────────────────────────────────────────────────────

interface DeleteUserModalProps {
  user: User
  onClose: () => void
  onConfirm: () => void
}

function DeleteUserModal({ user, onClose, onConfirm }: DeleteUserModalProps) {
  return (
    <>
      <div className="fixed inset-0 z-40 bg-canvas/80 backdrop-blur-sm" aria-hidden="true" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="delete-user-title"
        className="fixed inset-0 z-50 flex items-center justify-center p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="w-full max-w-sm rounded-xl border border-border-subtle bg-raised shadow-pop animate-scale-in">
          <div className="space-y-5 p-6">
            <h2 id="delete-user-title" className="text-lg font-extrabold tracking-tight text-text-primary">
              Delete user — {user.username}
            </h2>
            <p className="text-sm text-text-secondary">
              Are you sure you want to delete <span className="font-semibold text-text-primary">{user.username}</span>? This cannot be undone.
            </p>
            <div className="flex items-center justify-end gap-3 pt-1">
              <Button variant="ghost" onClick={onClose}>Cancel</Button>
              <Button
                variant="ghost"
                onClick={onConfirm}
                aria-label="Delete"
              >
                <span className="text-error">Delete</span>
              </Button>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}

// ── Reset Password Modal ──────────────────────────────────────────────────────

interface ResetPasswordModalProps {
  user: User
  onClose: () => void
  onReset: () => void
}

function ResetPasswordModal({ user, onClose, onReset }: ResetPasswordModalProps) {
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleSubmit() {
    const trimmed = password.trim()
    if (!trimmed || busy) return
    setBusy(true)
    setError(null)
    try {
      await resetPassword(user.id, trimmed)
      onReset()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to reset password')
      setBusy(false)
    }
  }

  return (
    <>
      <div className="fixed inset-0 z-40 bg-canvas/80 backdrop-blur-sm" aria-hidden="true" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="reset-pwd-title"
        className="fixed inset-0 z-50 flex items-center justify-center p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="w-full max-w-sm rounded-xl border border-border-subtle bg-raised shadow-pop animate-scale-in">
          <div className="space-y-5 p-6">
            <h2 id="reset-pwd-title" className="text-lg font-extrabold tracking-tight text-text-primary">
              Reset password — {user.username}
            </h2>
            <div className="space-y-1.5">
              <label htmlFor="reset-password" className="block text-sm font-semibold text-text-primary">
                New password
              </label>
              <input
                id="reset-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') void handleSubmit() }}
                disabled={busy}
                autoComplete="new-password"
                className="w-full rounded-lg border border-border-subtle bg-input px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
              />
            </div>
            {error && <p role="alert" className="text-sm text-error">{error}</p>}
            <div className="flex items-center justify-end gap-3 pt-1">
              <Button variant="ghost" onClick={onClose}>Cancel</Button>
              <Button
                variant="primary"
                disabled={password.trim() === '' || busy}
                onClick={() => void handleSubmit()}
              >
                {busy ? 'Resetting…' : 'Reset'}
              </Button>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}

// ── User Row ──────────────────────────────────────────────────────────────────

interface UserRowProps {
  user: User
  roles: { id: string; name: string }[]
  onRefresh: () => void
}

function UserRow({ user, roles, onRefresh }: UserRowProps) {
  const [resetTarget, setResetTarget] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState(false)

  async function handleRoleChange(roleId: string) {
    await updateUser(user.id, { roleId })
    onRefresh()
  }

  async function handleToggleDisabled() {
    await updateUser(user.id, { disabled: !user.disabled })
    onRefresh()
  }

  async function handleDeleteConfirm() {
    setDeleteTarget(false)
    await deleteUser(user.id)
    onRefresh()
  }

  return (
    <>
      <tr className="border-b border-border-subtle last:border-0">
        {/* Username + owner lock */}
        <td className="py-3 pr-4">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-semibold text-text-primary">{user.username}</span>
            {user.isOwner && <LockIcon />}
          </div>
        </td>

        {/* Role */}
        <td className="py-3 pr-4">
          {user.isOwner ? (
            <span className="text-sm text-text-secondary">{user.roleName}</span>
          ) : (
            <Select
              label="Role"
              value={user.roleId}
              options={roles.map((r) => ({ value: r.id, label: r.name }))}
              onChange={(v) => void handleRoleChange(v)}
            />
          )}
        </td>

        {/* Status */}
        <td className="py-3 pr-4">
          <span
            className={[
              'text-xs font-semibold',
              user.disabled ? 'text-error' : 'text-success',
            ].join(' ')}
          >
            {user.disabled ? 'Disabled' : 'Active'}
          </span>
        </td>

        {/* Created */}
        <td className="py-3 pr-4 text-sm text-text-secondary whitespace-nowrap">
          {formatDate(user.createdAt)}
        </td>

        {/* Last seen */}
        <td className="py-3 pr-4 text-sm text-text-secondary whitespace-nowrap">
          {formatDate(user.lastSeen)}
        </td>

        {/* Actions */}
        <td className="py-3">
          <div className="flex items-center gap-2">
            <Button size="sm" variant="ghost" onClick={() => setResetTarget(true)}>
              Reset pwd
            </Button>
            {!user.isOwner && (
              <>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={user.disabled ? 'Enable' : 'Disable'}
                  onClick={() => void handleToggleDisabled()}
                >
                  {user.disabled ? 'Enable' : 'Disable'}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label="Delete"
                  onClick={() => setDeleteTarget(true)}
                >
                  <span className="text-error">Delete</span>
                </Button>
              </>
            )}
          </div>
        </td>
      </tr>

      {resetTarget && (
        <ResetPasswordModal
          user={user}
          onClose={() => setResetTarget(false)}
          onReset={() => {
            setResetTarget(false)
            onRefresh()
          }}
        />
      )}

      {deleteTarget && (
        <DeleteUserModal
          user={user}
          onClose={() => setDeleteTarget(false)}
          onConfirm={() => void handleDeleteConfirm()}
        />
      )}
    </>
  )
}

// ── UsersSection (main export) ────────────────────────────────────────────────

export function UsersSection() {
  const qc = useQueryClient()
  const users = useUsers()
  const roles = useRoles()

  const [creating, setCreating] = useState(false)

  function refresh() {
    void qc.invalidateQueries({ queryKey: ['users', 'list'] })
  }

  const userList = users.data ?? []
  const roleList = roles.data ?? []

  return (
    <section className="space-y-4">
      {/* Header */}
      <div className="flex items-center gap-3">
        <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Users</h2>
        {!users.isLoading && (
          <span className="text-xs font-bold text-text-muted">{userList.length}</span>
        )}
        <div className="ml-auto">
          <Button size="sm" variant="secondary" onClick={() => setCreating(true)}>
            + Create user
          </Button>
        </div>
      </div>

      {/* Loading */}
      {users.isLoading && (
        <div className="space-y-2" aria-label="Loading users">
          <Skeleton data-testid="user-skeleton" className="h-10 w-full" />
          <Skeleton data-testid="user-skeleton" className="h-10 w-full" />
          <Skeleton data-testid="user-skeleton" className="h-10 w-full" />
        </div>
      )}

      {/* Empty state */}
      {!users.isLoading && userList.length === 0 && (
        <EmptyState
          icon="browse"
          title="No users yet"
          hint="Create the first user with the button above."
        />
      )}

      {/* Table */}
      {!users.isLoading && userList.length > 0 && (
        <div className="rounded-lg border border-border-subtle bg-raised overflow-x-auto">
          <table className="w-full text-left">
            <thead>
              <tr className="border-b border-border-subtle">
                <th className="px-0 py-2.5 text-xs font-bold text-text-muted uppercase tracking-wide">
                  Username
                </th>
                <th className="py-2.5 pr-4 text-xs font-bold text-text-muted uppercase tracking-wide">
                  Role
                </th>
                <th className="py-2.5 pr-4 text-xs font-bold text-text-muted uppercase tracking-wide">
                  Status
                </th>
                <th className="py-2.5 pr-4 text-xs font-bold text-text-muted uppercase tracking-wide">
                  Created
                </th>
                <th className="py-2.5 pr-4 text-xs font-bold text-text-muted uppercase tracking-wide">
                  Last seen
                </th>
                <th className="py-2.5 text-xs font-bold text-text-muted uppercase tracking-wide">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {userList.map((u) => (
                <UserRow key={u.id} user={u} roles={roleList} onRefresh={refresh} />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Create user modal */}
      {creating && (
        <CreateUserModal
          roles={roleList}
          onClose={() => setCreating(false)}
          onCreated={() => {
            setCreating(false)
            refresh()
          }}
        />
      )}
    </section>
  )
}
