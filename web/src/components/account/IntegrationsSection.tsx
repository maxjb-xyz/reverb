import { useEffect, useState } from 'react'
import { Button } from '../ui'
import {
  getLinks,
  lastfmAuthUrl,
  lastfmComplete,
  lastfmDisconnect,
  ScrobbleError,
} from '../../lib/scrobbleApi'
import type { ScrobbleLink } from '../../lib/scrobbleApi'

// Distinct, spec-mandated copy for the two auth-url failure codes.
const MSG_UNAVAILABLE = 'Last.fm is temporarily unavailable — try again.'
const MSG_NOT_CONFIGURED =
  "Last.fm isn't set up on this server yet — ask an administrator to configure it."

// ── Per-user Last.fm connect widget ──────────────────────────────────────────

interface LastfmLinkState {
  configured: boolean
  link: ScrobbleLink | null
}

// The widget is driven ENTIRELY by `step`. The link status only seeds the
// initial step; once the user starts the connect flow, `step` is the single
// source of truth so a stale `link.status` can never short-circuit a re-render.
type ConnectStep =
  | 'idle' // not connected — show Connect (or Reconnect when link is broken)
  | 'pending' // auth-url request in flight
  | 'awaiting-approval' // window opened, waiting for the user to click "I've approved"
  | 'completing' // complete-auth request in flight
  | 'connected' // active link
  | 'error-unavailable' // auth-url failed transiently
  | 'error-not-configured' // auth-url failed because the admin hasn't configured the app
  | 'error-complete' // complete-auth rejected — recoverable

interface LastfmUserWidgetProps {
  state: LastfmLinkState
}

// initialStep maps a freshly-loaded link to the starting step.
function initialStep(link: ScrobbleLink | null): ConnectStep {
  if (link?.status === 'active') return 'connected'
  return 'idle'
}

function LastfmUserWidget({ state }: LastfmUserWidgetProps) {
  const { configured, link } = state
  const broken = link?.status === 'broken'

  const [step, setStep] = useState<ConnectStep>(() => initialStep(link))
  const [pendingToken, setPendingToken] = useState<string | null>(null)
  const [connectedUsername, setConnectedUsername] = useState<string | null>(
    link?.status === 'active' ? link.username : null,
  )

  // Re-seed when the upstream link changes (e.g. parent re-fetched after a save).
  // Only re-seed while the user is at rest (idle/connected) so an in-flight
  // connect flow is never clobbered by a background refresh.
  useEffect(() => {
    if (step === 'idle' || step === 'connected') {
      setStep(initialStep(link))
      setConnectedUsername(link?.status === 'active' ? link.username : null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [link?.status, link?.username])

  async function handleConnect() {
    setStep('pending')
    try {
      const { authUrl, token } = await lastfmAuthUrl()
      window.open(authUrl, '_blank')
      setPendingToken(token)
      setStep('awaiting-approval')
    } catch (e) {
      if (e instanceof ScrobbleError && e.code === 'lastfm_not_configured') {
        setStep('error-not-configured')
        return
      }
      setStep('error-unavailable')
    }
  }

  function handleComplete() {
    if (!pendingToken) return
    setStep('completing')
    lastfmComplete(pendingToken)
      .then((res) => {
        setConnectedUsername(res.username)
        setStep('connected')
      })
      .catch(() => {
        // Don't strand the user in 'completing' — surface a recoverable error.
        setStep('error-complete')
      })
  }

  const busy = step === 'pending' || step === 'completing'

  // ── Render by step (single source of truth) ─────────────────────────────────

  // App not configured at all → admin must set the key first.
  if (!configured) {
    return (
      <p className="text-xs text-text-secondary text-right">
        Last.fm isn&apos;t set up on this server yet. Ask an administrator to configure it.
      </p>
    )
  }

  if (step === 'connected' && connectedUsername) {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-primary">Connected as {connectedUsername}</span>
        <Button
          variant="secondary"
          onClick={() => {
            void lastfmDisconnect().then(() => {
              setConnectedUsername(null)
              setStep('idle')
            })
          }}
        >
          Disconnect
        </Button>
      </div>
    )
  }

  if (step === 'awaiting-approval') {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-secondary">
          Approve Reverb in the Last.fm tab, then click below.
        </span>
        <Button variant="secondary" disabled={busy} onClick={handleComplete}>
          I&apos;ve approved — finish connecting
        </Button>
      </div>
    )
  }

  if (step === 'completing') {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-secondary">Finishing…</span>
        <Button variant="secondary" disabled>
          I&apos;ve approved — finish connecting
        </Button>
      </div>
    )
  }

  if (step === 'error-complete') {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-secondary">
          We couldn&apos;t finish connecting — try again.
        </span>
        <Button variant="secondary" onClick={() => void handleConnect()}>
          Try again
        </Button>
      </div>
    )
  }

  if (step === 'error-unavailable') {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-secondary">{MSG_UNAVAILABLE}</span>
        <Button variant="secondary" onClick={() => void handleConnect()}>
          Try again
        </Button>
      </div>
    )
  }

  if (step === 'error-not-configured') {
    return (
      <p className="text-xs text-text-secondary text-right">{MSG_NOT_CONFIGURED}</p>
    )
  }

  // step === 'idle' — Connect, or Reconnect when the existing link is broken.
  // Guarded by step==='idle' so an in-flight flow is never short-circuited.
  if (broken) {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-secondary">
          {link?.username} — Last.fm needs reconnecting
        </span>
        <Button variant="secondary" disabled={busy} onClick={() => void handleConnect()}>
          Reconnect
        </Button>
      </div>
    )
  }

  return (
    <Button variant="secondary" disabled={busy} onClick={() => void handleConnect()}>
      Connect Last.fm
    </Button>
  )
}

// ── IntegrationsSection ───────────────────────────────────────────────────────

type LoadState =
  | { phase: 'loading' }
  | { phase: 'error' }
  | { phase: 'ready'; data: LastfmLinkState }

export function IntegrationsSection() {
  const [load, setLoad] = useState<LoadState>({ phase: 'loading' })

  function refreshLinks() {
    void getLinks()
      .then((res) => {
        const lastfmLink = res.links.find((l) => l.provider === 'lastfm') ?? null
        setLoad({ phase: 'ready', data: { configured: res.configured, link: lastfmLink } })
      })
      .catch(() => setLoad({ phase: 'error' }))
  }

  useEffect(() => {
    refreshLinks()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <section className="space-y-0 divide-y divide-border-subtle">
      <h2 className="text-base font-bold text-text-primary pb-3">Integrations</h2>

      {/* Last.fm per-user widget */}
      <div className="flex items-start gap-5 py-5">
        <div className="flex-1 min-w-0">
          <div className="text-sm font-bold text-text-primary">Last.fm</div>
          <div className="text-xs text-text-secondary mt-0.5">
            Scrobble your listening history to Last.fm.
          </div>
        </div>
        <div className="flex-none max-w-xs">
          {load.phase === 'ready' ? (
            <LastfmUserWidget key={String(load.data.configured)} state={load.data} />
          ) : load.phase === 'error' ? (
            <span className="text-xs text-text-secondary">
              Couldn&apos;t load your integrations.
            </span>
          ) : (
            <span className="text-xs text-text-secondary">Loading…</span>
          )}
        </div>
      </div>
    </section>
  )
}
