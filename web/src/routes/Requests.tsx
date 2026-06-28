import { useState, useEffect } from 'react'
import { useAuthStore } from '../lib/authStore'
import {
  useRequestStore,
  getMyRequests,
  getAllRequests,
  approveRequest,
  denyRequest,
  cancelRequest,
} from '../lib/requestApi'
import type { Request as MusicRequest, RequestStatus } from '../lib/requestApi'
import { Cover, EmptyState, Button } from '../components/ui'
import { coverUrl } from '../lib/libraryApi'

// ---- Status chip --------------------------------------------------------

type ChipVariant = 'success' | 'error' | 'muted'

function statusLabel(status: RequestStatus, downloadJobId?: string): string {
  switch (status) {
    case 'pending':
      return 'Pending'
    case 'approved':
      return downloadJobId ? 'Downloading' : 'Approved'
    case 'fulfilled':
      return 'Added'
    case 'denied':
      return 'Denied'
    case 'failed':
      return 'Failed'
    default:
      return status
  }
}

function statusVariant(status: RequestStatus): ChipVariant {
  if (status === 'fulfilled') return 'success'
  if (status === 'denied' || status === 'failed') return 'error'
  return 'muted'
}

function StatusChip({ req }: { req: MusicRequest }) {
  const variant = statusVariant(req.status)
  const label = statusLabel(req.status, req.downloadJobId)
  const cls =
    variant === 'success'
      ? 'text-success'
      : variant === 'error'
        ? 'text-error'
        : 'text-text-muted'
  return (
    <span className={`text-xs font-semibold ${cls}`}>
      {label}
    </span>
  )
}

// ---- My Requests tab ----------------------------------------------------

function MyRequestsTab({ userId }: { userId: string }) {
  const requests = useRequestStore((s) =>
    s.mine(userId).sort((a, b) => b.createdAt - a.createdAt),
  )

  useEffect(() => {
    void getMyRequests().then((reqs) => useRequestStore.getState().setMine(reqs))
  }, [])

  if (requests.length === 0) {
    return (
      <EmptyState
        icon="music"
        title="No requests yet"
        hint="Search for a track and request it — it'll appear here."
      />
    )
  }

  return (
    <ul className="space-y-1">
      {requests.map((req) => (
        <MyRequestRow key={req.id} req={req} userId={userId} />
      ))}
    </ul>
  )
}

function MyRequestRow({ req, userId }: { req: MusicRequest; userId: string }) {
  const isPendingOwn = req.status === 'pending' && req.requestedBy === userId
  const coverSrc = req.coverUrl ?? (req.coverArtId ? coverUrl(req.coverArtId) : undefined)

  async function handleCancel() {
    await cancelRequest(req.id)
    // Refetch to reflect updated state
    const reqs = await getMyRequests()
    useRequestStore.getState().setMine(reqs)
  }

  return (
    <li className="flex items-center gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-raised-hover">
      <Cover src={coverSrc} alt={req.title} size={40} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-semibold text-text-primary">{req.title}</div>
        <div className="truncate text-xs text-text-secondary">{req.artist}</div>
      </div>
      <div className="flex-none">
        <StatusChip req={req} />
      </div>
      {isPendingOwn && (
        <Button variant="ghost" size="sm" aria-label={`Cancel ${req.title}`} onClick={() => void handleCancel()}>
          Cancel
        </Button>
      )}
    </li>
  )
}

// ---- Approval tab -------------------------------------------------------

function ApprovalTab() {
  const pending = useRequestStore((s) =>
    s.pending().sort((a, b) => a.createdAt - b.createdAt),
  )

  useEffect(() => {
    void getAllRequests('pending').then((reqs) => useRequestStore.getState().setQueue(reqs))
  }, [])

  if (pending.length === 0) {
    return (
      <EmptyState
        icon="music"
        title="No pending requests"
        hint="All caught up — nothing waiting for approval."
      />
    )
  }

  return (
    <ul className="space-y-1">
      {pending.map((req) => (
        <ApprovalRow key={req.id} req={req} />
      ))}
    </ul>
  )
}

function ApprovalRow({ req }: { req: MusicRequest }) {
  const [denying, setDenying] = useState(false)
  const [reason, setReason] = useState('')
  const coverSrc = req.coverUrl ?? (req.coverArtId ? coverUrl(req.coverArtId) : undefined)

  async function handleApprove() {
    const updated = await approveRequest(req.id)
    useRequestStore.getState().upsert(updated)
  }

  async function handleDeny() {
    const updated = await denyRequest(req.id, reason.trim() || undefined)
    useRequestStore.getState().upsert(updated)
    setDenying(false)
    setReason('')
  }

  return (
    <li className="flex flex-col gap-2 rounded-lg px-2 py-2 transition-colors hover:bg-raised-hover">
      <div className="flex items-center gap-3">
        <Cover src={coverSrc} alt={req.title} size={40} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-text-primary">{req.title}</div>
          <div className="truncate text-xs text-text-secondary">
            {req.artist} · <span className="text-text-muted">by {req.requestedBy}</span>
          </div>
        </div>
        <div className="flex flex-none items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            aria-label={`Approve ${req.title}`}
            onClick={() => void handleApprove()}
          >
            Approve
          </Button>
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Deny ${req.title}`}
            onClick={() => setDenying((d) => !d)}
          >
            Deny
          </Button>
        </div>
      </div>

      {denying && (
        <div className="flex items-center gap-2 pl-[52px]">
          <input
            type="text"
            placeholder="Reason (optional)"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            className="flex-1 rounded-md bg-input px-3 py-1.5 text-sm text-text-primary placeholder:text-text-muted outline-none border border-border-subtle focus:border-accent"
            aria-label="Deny reason"
          />
          <Button variant="ghost" size="sm" aria-label="Confirm deny" onClick={() => void handleDeny()}>
            Confirm
          </Button>
          <Button variant="ghost" size="sm" onClick={() => { setDenying(false); setReason('') }}>
            Cancel
          </Button>
        </div>
      )}
    </li>
  )
}

// ---- Page ---------------------------------------------------------------

type TabKey = 'mine' | 'approval'

export default function Requests() {
  const me = useAuthStore((s) => s.me)
  const can = useAuthStore((s) => s.can)
  const [tab, setTab] = useState<TabKey>('mine')

  const isManager = can('manage_requests')

  if (!me) return null

  return (
    <div className="space-y-4">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-text-primary" role="heading">
          My Requests
        </h1>
        <p className="mt-0.5 text-xs text-text-secondary">
          Track the status of your song requests
        </p>
      </div>

      {/* Tabs — only render tab row when there's more than one tab */}
      {isManager && (
        <div role="tablist" className="flex gap-1 border-b border-border-subtle">
          <button
            role="tab"
            aria-selected={tab === 'mine'}
            onClick={() => setTab('mine')}
            className={[
              'px-4 py-2 text-sm font-semibold transition-colors',
              tab === 'mine'
                ? 'border-b-2 border-accent text-accent'
                : 'text-text-secondary hover:text-text-primary',
            ].join(' ')}
          >
            My Requests
          </button>
          <button
            role="tab"
            aria-selected={tab === 'approval'}
            onClick={() => setTab('approval')}
            className={[
              'px-4 py-2 text-sm font-semibold transition-colors',
              tab === 'approval'
                ? 'border-b-2 border-accent text-accent'
                : 'text-text-secondary hover:text-text-primary',
            ].join(' ')}
          >
            Approval
          </button>
        </div>
      )}

      {/* Body */}
      {tab === 'mine' && <MyRequestsTab userId={me.id} />}
      {tab === 'approval' && isManager && <ApprovalTab />}
    </div>
  )
}
