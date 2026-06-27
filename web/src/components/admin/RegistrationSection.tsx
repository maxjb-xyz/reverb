import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Button, Toggle, Select, Skeleton } from '../ui'
import {
  useRoles,
  useRegistration,
  setRegistration,
  useInvites,
  createInvite,
  deleteInvite,
  type RegistrationPolicy,
  type Invite,
  type CreateInviteReq,
} from '../../lib/usersApi'

// ── Helpers ────────────────────────────────────────────────────────────────────

function formatDate(unixSec: number | null): string {
  if (unixSec == null) return '—'
  return new Date(unixSec * 1000).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

function inviteSignupUrl(code: string): string {
  return `${window.location.origin}/signup?invite=${code}`
}

// ── Copy button ────────────────────────────────────────────────────────────────

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  async function handleCopy() {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <Button size="sm" variant="ghost" onClick={() => void handleCopy()} aria-label="Copy link">
      {copied ? 'Copied!' : 'Copy'}
    </Button>
  )
}

// ── Invite row ─────────────────────────────────────────────────────────────────

interface InviteRowProps {
  invite: Invite
  onRefresh: () => void
}

function InviteRow({ invite, onRefresh }: InviteRowProps) {
  const url = inviteSignupUrl(invite.code)

  async function handleRevoke() {
    await deleteInvite(invite.id)
    onRefresh()
  }

  return (
    <div className="flex items-center gap-3 py-2.5 border-b border-border-subtle last:border-0 flex-wrap">
      <div className="flex-1 min-w-0 space-y-0.5">
        <div className="text-xs font-mono text-text-secondary truncate">{invite.code}</div>
        <div className="text-xs text-text-muted truncate">{url}</div>
        <div className="flex gap-3 text-xs text-text-muted">
          <span>Role: {invite.roleName ?? 'Default'}</span>
          {invite.expiresAt && <span>Expires: {formatDate(invite.expiresAt)}</span>}
          {invite.usedAt && <span className="text-success">Used: {formatDate(invite.usedAt)}</span>}
          {!invite.usedAt && <span className="text-success">Active</span>}
        </div>
      </div>
      <div className="flex items-center gap-2 flex-none">
        <CopyButton text={url} />
        {!invite.usedAt && (
          <Button
            size="sm"
            variant="ghost"
            aria-label="Revoke"
            onClick={() => void handleRevoke()}
          >
            <span className="text-error">Revoke</span>
          </Button>
        )}
      </div>
    </div>
  )
}

// ── Generate invite form ───────────────────────────────────────────────────────

interface GenerateInviteFormProps {
  roles: { id: string; name: string }[]
  defaultRoleId: string
  onGenerated: (code: string) => void
}

function GenerateInviteForm({ roles, defaultRoleId, onGenerated }: GenerateInviteFormProps) {
  const [roleId, setRoleId] = useState(defaultRoleId)
  const [expiresAt, setExpiresAt] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleGenerate() {
    if (busy) return
    setBusy(true)
    setError(null)
    const body: CreateInviteReq = {}
    if (roleId) body.roleId = roleId
    if (expiresAt) body.expiresAt = Math.floor(new Date(expiresAt).getTime() / 1000)
    try {
      const result = await createInvite(body)
      onGenerated(result.code)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to generate invite')
      setBusy(false)
    }
  }

  return (
    <div className="space-y-3 p-4 rounded-lg border border-border-subtle bg-raised-hover">
      <div className="flex flex-wrap items-end gap-3">
        {/* Role */}
        <div className="space-y-1">
          <label className="block text-xs font-bold text-text-muted uppercase tracking-wide">
            Role
          </label>
          <Select
            label="Invite role"
            value={roleId}
            options={[
              { value: '', label: 'Default' },
              ...roles.map((r) => ({ value: r.id, label: r.name })),
            ]}
            onChange={(v) => setRoleId(v)}
          />
        </div>

        {/* Optional expiry */}
        <div className="space-y-1">
          <label
            htmlFor="invite-expires"
            className="block text-xs font-bold text-text-muted uppercase tracking-wide"
          >
            Expires (optional)
          </label>
          <input
            id="invite-expires"
            type="date"
            value={expiresAt}
            onChange={(e) => setExpiresAt(e.target.value)}
            className="rounded-lg border border-border-subtle bg-input px-3 py-2 text-sm text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          />
        </div>

        <Button
          variant="secondary"
          size="sm"
          disabled={busy}
          onClick={() => void handleGenerate()}
          aria-label="Generate invite"
        >
          {busy ? 'Generating…' : 'Generate invite'}
        </Button>
      </div>

      {error && (
        <p role="alert" className="text-sm text-error">
          {error}
        </p>
      )}
    </div>
  )
}

// ── Invites section ────────────────────────────────────────────────────────────

interface InvitesSectionProps {
  roles: { id: string; name: string }[]
  defaultRoleId: string
  onRefresh: () => void
}

function InvitesArea({ roles, defaultRoleId, onRefresh }: InvitesSectionProps) {
  const invites = useInvites()
  const [lastCode, setLastCode] = useState<string | null>(null)

  function handleGenerated(code: string) {
    setLastCode(code)
    onRefresh()
  }

  const inviteList = invites.data ?? []

  return (
    <div className="space-y-4">
      <h3 className="text-base font-extrabold tracking-tight text-text-primary">Invites</h3>

      <GenerateInviteForm
        roles={roles}
        defaultRoleId={defaultRoleId}
        onGenerated={handleGenerated}
      />

      {/* Show newly generated link */}
      {lastCode && (
        <div className="flex items-center gap-3 rounded-lg border border-border-subtle bg-raised p-3">
          <div className="flex-1 min-w-0">
            <p className="text-xs font-bold text-text-muted uppercase tracking-wide mb-1">
              Invite link
            </p>
            <p className="text-sm text-text-primary truncate font-mono">
              {inviteSignupUrl(lastCode)}
            </p>
          </div>
          <CopyButton text={inviteSignupUrl(lastCode)} />
          <Button size="sm" variant="ghost" onClick={() => setLastCode(null)}>
            Dismiss
          </Button>
        </div>
      )}

      {/* Existing invites */}
      {invites.isLoading && <Skeleton className="h-10 w-full" />}
      {!invites.isLoading && inviteList.length > 0 && (
        <div className="rounded-lg border border-border-subtle bg-raised px-4">
          {inviteList.map((inv) => (
            <InviteRow key={inv.id} invite={inv} onRefresh={onRefresh} />
          ))}
        </div>
      )}
      {!invites.isLoading && inviteList.length === 0 && (
        <p className="text-sm text-text-muted">No invites yet.</p>
      )}
    </div>
  )
}

// ── RegistrationSection (main export) ─────────────────────────────────────────

export function RegistrationSection() {
  const qc = useQueryClient()
  const reg = useRegistration()
  const roles = useRoles()

  function refreshReg() {
    void qc.invalidateQueries({ queryKey: ['registration', 'policy'] })
  }

  function refreshInvites() {
    void qc.invalidateQueries({ queryKey: ['invites', 'list'] })
  }

  const policy: RegistrationPolicy | undefined = reg.data
  const roleList = (roles.data ?? []).map((r) => ({ id: r.id, name: r.name }))

  async function patch(updates: Partial<RegistrationPolicy>) {
    if (!policy) return
    await setRegistration({ ...policy, ...updates })
    refreshReg()
  }

  if (reg.isLoading) {
    return (
      <section className="space-y-4">
        <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Registration</h2>
        <Skeleton className="h-24 w-full" />
      </section>
    )
  }

  return (
    <section className="space-y-6">
      {/* Policy card */}
      <div className="rounded-lg border border-border-subtle bg-raised p-6 space-y-5">
        <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Registration</h2>

        {/* Signup toggle */}
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-semibold text-text-primary">Open signup</p>
            <p className="text-xs text-text-secondary">Allow anyone to create an account.</p>
          </div>
          <Toggle
            label="Enable signup"
            checked={policy?.signupEnabled ?? false}
            onChange={(v) => void patch({ signupEnabled: v })}
          />
        </div>

        {/* Invites toggle */}
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-semibold text-text-primary">Invite-only signup</p>
            <p className="text-xs text-text-secondary">
              Require an invite link to register.
            </p>
          </div>
          <Toggle
            label="Enable invites"
            checked={policy?.invitesEnabled ?? false}
            onChange={(v) => void patch({ invitesEnabled: v })}
          />
        </div>

        {/* Default role */}
        <div className="flex items-center justify-between gap-4">
          <div>
            <p className="text-sm font-semibold text-text-primary">Default role</p>
            <p className="text-xs text-text-secondary">Role assigned to new registrants.</p>
          </div>
          {roles.isLoading ? (
            <Skeleton className="h-9 w-36" />
          ) : (
            <Select
              label="Default role"
              value={policy?.defaultRoleId ?? ''}
              options={roleList.map((r) => ({ value: r.id, label: r.name }))}
              onChange={(v) => void patch({ defaultRoleId: v })}
            />
          )}
        </div>
      </div>

      {/* Invites area — only when invitesEnabled */}
      {policy?.invitesEnabled && (
        <InvitesArea
          roles={roleList}
          defaultRoleId={policy.defaultRoleId}
          onRefresh={refreshInvites}
        />
      )}
    </section>
  )
}
