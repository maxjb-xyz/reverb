import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Button, Skeleton, EmptyState } from '../ui'
import {
  useRoles,
  useCapabilities,
  createRole,
  updateRole,
  deleteRole,
  type Role,
  type Capability,
} from '../../lib/usersApi'

// ── Capability checklist ───────────────────────────────────────────────────────

interface CapabilityChecklistProps {
  capabilities: Capability[]
  selected: string[]
  onChange: (selected: string[]) => void
}

function CapabilityChecklist({ capabilities, selected, onChange }: CapabilityChecklistProps) {
  function toggle(key: string) {
    if (selected.includes(key)) {
      onChange(selected.filter((k) => k !== key))
    } else {
      onChange([...selected, key])
    }
  }

  return (
    <div className="space-y-2">
      {capabilities.map((cap) => (
        <label key={cap.key} className="flex items-center gap-2 cursor-pointer select-none">
          <input
            type="checkbox"
            aria-label={cap.label}
            checked={selected.includes(cap.key)}
            onChange={() => toggle(cap.key)}
            className="rounded border-border-subtle bg-input accent-accent w-4 h-4"
          />
          <span className="text-sm text-text-primary">{cap.label}</span>
        </label>
      ))}
    </div>
  )
}

// ── Role editor form (create or edit) ─────────────────────────────────────────

interface RoleFormProps {
  initial?: Role
  capabilities: Capability[]
  onSave: (name: string, caps: string[]) => Promise<void>
  onCancel: () => void
}

function RoleForm({ initial, capabilities, onSave, onCancel }: RoleFormProps) {
  const [name, setName] = useState(initial?.name ?? '')
  const [selected, setSelected] = useState<string[]>(initial?.capabilities ?? [])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleSave() {
    if (!name.trim() || busy) return
    setBusy(true)
    setError(null)
    try {
      await onSave(name.trim(), selected)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save role')
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4 rounded-lg border border-border-subtle bg-raised p-4 mt-2">
      {/* Name */}
      <div className="space-y-1.5">
        <label
          htmlFor="role-name-input"
          className="block text-xs font-bold text-text-muted uppercase tracking-wide"
        >
          Role name
        </label>
        <input
          id="role-name-input"
          type="text"
          aria-label="Role name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          disabled={busy}
          className="w-full rounded-lg border border-border-subtle bg-input px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
        />
      </div>

      {/* Capabilities */}
      <div className="space-y-2">
        <p className="text-xs font-bold text-text-muted uppercase tracking-wide">Capabilities</p>
        <CapabilityChecklist
          capabilities={capabilities}
          selected={selected}
          onChange={setSelected}
        />
      </div>

      {error && (
        <p role="alert" className="text-sm text-error">
          {error}
        </p>
      )}

      <div className="flex items-center gap-2 pt-1">
        <Button variant="primary" size="sm" disabled={!name.trim() || busy} onClick={() => void handleSave()}>
          {busy ? 'Saving…' : 'Save'}
        </Button>
        <Button variant="ghost" size="sm" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
      </div>
    </div>
  )
}

// ── Role row ───────────────────────────────────────────────────────────────────

interface RoleRowProps {
  role: Role
  capabilities: Capability[]
  onRefresh: () => void
}

function RoleRow({ role, capabilities, onRefresh }: RoleRowProps) {
  const [editing, setEditing] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  async function handleDelete() {
    setDeleteError(null)
    try {
      await deleteRole(role.id)
      onRefresh()
    } catch (e) {
      const err = e as { status?: number; message?: string }
      if (err.status === 409) {
        setDeleteError('This role is in use and cannot be deleted.')
      } else {
        setDeleteError(e instanceof Error ? e.message : 'Failed to delete role')
      }
    }
  }

  async function handleSave(name: string, caps: string[]) {
    await updateRole(role.id, { name, capabilities: caps })
    setEditing(false)
    onRefresh()
  }

  const capChips = (role.capabilities ?? []).map((key) => {
    const cap = capabilities.find((c) => c.key === key)
    return cap?.label ?? key
  })

  return (
    <div className="space-y-1">
      <div className="flex items-start gap-3 py-2.5 border-b border-border-subtle last:border-0">
        {/* Name + system badge */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold text-text-primary">{role.name}</span>
            {role.isSystem && (
              <span className="text-xs font-bold uppercase tracking-wide text-text-muted bg-raised-hover rounded px-1.5 py-0.5">
                System
              </span>
            )}
          </div>
          {/* Capability chips */}
          {capChips.length > 0 && (
            <div className="flex flex-wrap gap-1.5 mt-1.5">
              {capChips.map((label) => (
                <span
                  key={label}
                  className="text-xs font-medium text-text-secondary bg-raised-hover border border-border-subtle rounded px-1.5 py-0.5"
                >
                  {label}
                </span>
              ))}
            </div>
          )}
        </div>

        {/* Actions — only for custom roles */}
        {!role.isSystem && (
          <div className="flex items-center gap-2 flex-none">
            <Button size="sm" variant="ghost" onClick={() => setEditing(!editing)}>
              Edit
            </Button>
            <Button
              size="sm"
              variant="ghost"
              aria-label="Delete"
              onClick={() => void handleDelete()}
            >
              <span className="text-error">Delete</span>
            </Button>
          </div>
        )}
      </div>

      {deleteError && (
        <p role="alert" className="text-sm text-error py-1">
          {deleteError}
        </p>
      )}

      {editing && (
        <RoleForm
          initial={role}
          capabilities={capabilities}
          onSave={handleSave}
          onCancel={() => setEditing(false)}
        />
      )}
    </div>
  )
}

// ── RolesSection (main export) ─────────────────────────────────────────────────

export function RolesSection() {
  const qc = useQueryClient()
  const roles = useRoles()
  const caps = useCapabilities()

  const [creating, setCreating] = useState(false)

  function refresh() {
    void qc.invalidateQueries({ queryKey: ['roles', 'list'] })
  }

  const roleList = roles.data ?? []
  const capList = caps.data ?? []

  async function handleCreate(name: string, capabilities: string[]) {
    await createRole({ name, capabilities })
    setCreating(false)
    refresh()
  }

  return (
    <section className="space-y-4">
      {/* Header */}
      <div className="flex items-center gap-3">
        <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Roles</h2>
        {!roles.isLoading && (
          <span className="text-xs font-bold text-text-muted">{roleList.length}</span>
        )}
        <div className="ml-auto">
          <Button size="sm" variant="secondary" onClick={() => setCreating(true)}>
            + Create role
          </Button>
        </div>
      </div>

      {/* Loading */}
      {(roles.isLoading || caps.isLoading) && (
        <div className="space-y-2" aria-label="Loading roles">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      )}

      {/* Empty state */}
      {!roles.isLoading && roleList.length === 0 && (
        <EmptyState
          icon="browse"
          title="No roles yet"
          hint="Create the first role with the button above."
        />
      )}

      {/* Role list */}
      {!roles.isLoading && roleList.length > 0 && (
        <div className="rounded-lg border border-border-subtle bg-raised px-4">
          {roleList.map((role) => (
            <RoleRow key={role.id} role={role} capabilities={capList} onRefresh={refresh} />
          ))}
        </div>
      )}

      {/* Create role form */}
      {creating && (
        <RoleForm
          capabilities={capList}
          onSave={handleCreate}
          onCancel={() => setCreating(false)}
        />
      )}
    </section>
  )
}
