import { useEffect, useState } from 'react'
import { Button } from '../ui'
import type { Me } from '../../lib/authStore'
import {
  getLinks,
  lastfmAuthUrl,
  lastfmComplete,
  lastfmDisconnect,
  getLastfmConfig,
  setLastfmConfig,
  ScrobbleError,
} from '../../lib/scrobbleApi'
import type { ScrobbleLink } from '../../lib/scrobbleApi'

interface Props {
  me: Me
}

// Sentinel used by the backend — returning it from the secret field means "keep".
const SECRET_SENTINEL = '••••••••'

function isAdmin(me: Me): boolean {
  return (
    me.capabilities.includes('can_manage_library') ||
    me.capabilities.includes('is_admin')
  )
}

// ── Per-user Last.fm connect widget ──────────────────────────────────────────

interface LastfmLinkState {
  configured: boolean
  link: ScrobbleLink | null
}

type ConnectStep = 'idle' | 'pending' | 'awaiting-approval' | 'completing' | 'connected' | 'error-unavailable'

interface LastfmUserWidgetProps {
  initialState: LastfmLinkState
}

function LastfmUserWidget({ initialState }: LastfmUserWidgetProps) {
  const [configured] = useState(initialState.configured)
  const [link, setLink] = useState<ScrobbleLink | null>(initialState.link)
  const [step, setStep] = useState<ConnectStep>('idle')
  const [pendingToken, setPendingToken] = useState<string | null>(null)
  const [connectedUsername, setConnectedUsername] = useState<string | null>(link?.username ?? null)

  // Resolve initial connected state from link.
  useEffect(() => {
    if (link?.status === 'active') {
      setStep('connected')
      setConnectedUsername(link.username)
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  if (!configured) {
    return (
      <p className="text-xs text-text-secondary">
        Last.fm is not set up on this server yet. Ask your admin to configure the app API key.
      </p>
    )
  }

  // Active link.
  if (step === 'connected' && connectedUsername) {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-primary">Connected as {connectedUsername}</span>
        <Button
          variant="secondary"
          onClick={() => {
            void lastfmDisconnect().then(() => {
              setLink(null)
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

  // Broken link.
  if (link?.status === 'broken') {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-secondary">{link.username} — needs reconnecting</span>
        <Button
          variant="secondary"
          onClick={() => {
            void handleConnect()
          }}
        >
          Reconnect
        </Button>
      </div>
    )
  }

  // Step: awaiting approval (window opened, user must click "I've approved").
  if (step === 'awaiting-approval') {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-secondary">
          Approve Reverb in the Last.fm tab, then click below.
        </span>
        <Button
          variant="secondary"
          onClick={() => {
            if (!pendingToken) return
            setStep('completing')
            void lastfmComplete(pendingToken).then((res) => {
              setConnectedUsername(res.username)
              setStep('connected')
            })
          }}
        >
          I&apos;ve approved
        </Button>
      </div>
    )
  }

  // Error: temporarily unavailable.
  if (step === 'error-unavailable') {
    return (
      <div className="flex items-center gap-3">
        <span className="text-xs text-text-secondary">Last.fm is temporarily unavailable — try again later.</span>
        <Button variant="secondary" onClick={() => setStep('idle')}>
          Retry
        </Button>
      </div>
    )
  }

  // Default: not connected, show Connect button.
  async function handleConnect() {
    setStep('pending')
    try {
      const { authUrl, token } = await lastfmAuthUrl()
      window.open(authUrl, '_blank')
      setPendingToken(token)
      setStep('awaiting-approval')
    } catch (e) {
      if (e instanceof ScrobbleError) {
        if (e.code === 'lastfm_unavailable') {
          setStep('error-unavailable')
          return
        }
        // lastfm_not_configured — shouldn't happen if configured===true, but handle gracefully.
        setStep('error-unavailable')
        return
      }
      setStep('error-unavailable')
    }
  }

  return (
    <Button
      variant="secondary"
      disabled={step === 'pending' || step === 'completing'}
      onClick={() => void handleConnect()}
    >
      Connect Last.fm
    </Button>
  )
}

// ── Admin: Last.fm app configuration ─────────────────────────────────────────

function LastfmAdminConfig() {
  const [apiKey, setApiKey] = useState('')
  const [apiSecret, setApiSecret] = useState('')
  const [apiSecretSet, setApiSecretSet] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [loadError, setLoadError] = useState(false)

  useEffect(() => {
    void getLastfmConfig()
      .then((cfg) => {
        setApiKey(cfg.apiKey)
        setApiSecretSet(cfg.apiSecretSet)
      })
      .catch(() => setLoadError(true))
  }, [])

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setSaved(false)
    try {
      await setLastfmConfig({ apiKey, apiSecret })
      setSaved(true)
      // Update local knowledge of secret state.
      if (apiSecret && apiSecret !== SECRET_SENTINEL) {
        setApiSecretSet(true)
      }
      setApiSecret('')
    } finally {
      setSaving(false)
    }
  }

  if (loadError) {
    return <p className="text-xs text-text-secondary">Could not load configuration.</p>
  }

  return (
    <form onSubmit={(e) => void handleSave(e)} className="space-y-3 max-w-sm">
      <div>
        <label
          htmlFor="lastfm-api-key"
          className="block text-xs font-semibold text-text-secondary mb-1"
        >
          API Key
        </label>
        <input
          id="lastfm-api-key"
          type="text"
          placeholder="API key"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          className="w-full h-9 px-3 rounded-md bg-input text-sm text-text-primary border border-border-subtle focus:outline-none focus:ring-2 focus:ring-accent"
        />
      </div>
      <div>
        <label
          htmlFor="lastfm-api-secret"
          className="block text-xs font-semibold text-text-secondary mb-1"
        >
          API Secret
        </label>
        <input
          id="lastfm-api-secret"
          type="password"
          placeholder={apiSecretSet ? 'saved' : 'API secret'}
          value={apiSecret}
          onChange={(e) => setApiSecret(e.target.value)}
          className="w-full h-9 px-3 rounded-md bg-input text-sm text-text-primary border border-border-subtle focus:outline-none focus:ring-2 focus:ring-accent"
        />
        {apiSecretSet && !apiSecret && (
          <p className="text-xs text-text-secondary mt-1">
            A secret is already saved. Leave blank to keep it.
          </p>
        )}
      </div>
      {saved && (
        <p className="text-xs font-medium text-success">Configuration saved.</p>
      )}
      <Button type="submit" variant="secondary" disabled={saving}>
        {saving ? 'Saving…' : 'Save'}
      </Button>
    </form>
  )
}

// ── IntegrationsSection ───────────────────────────────────────────────────────

export function IntegrationsSection({ me }: Props) {
  const [linkState, setLinkState] = useState<LastfmLinkState | null>(null)

  useEffect(() => {
    void getLinks().then((res) => {
      const lastfmLink = res.links.find((l) => l.provider === 'lastfm') ?? null
      setLinkState({ configured: res.configured, link: lastfmLink })
    })
  }, [])

  const showAdmin = isAdmin(me)

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
        <div className="flex-none">
          {linkState !== null ? (
            <LastfmUserWidget initialState={linkState} />
          ) : (
            <span className="text-xs text-text-secondary">Loading…</span>
          )}
        </div>
      </div>

      {/* Admin: app-level key configuration */}
      {showAdmin && (
        <div className="py-5">
          <div className="text-sm font-bold text-text-primary mb-1">App configuration</div>
          <div className="text-xs text-text-secondary mb-4">
            Set the Last.fm API key and secret for this Reverb instance.
          </div>
          <LastfmAdminConfig />
        </div>
      )}
    </section>
  )
}
