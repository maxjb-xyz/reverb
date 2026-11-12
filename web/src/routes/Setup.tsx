import { useState } from 'react'
import { adapterDisplayName } from '../lib/displayNames'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import { setupOwner } from '../lib/session'
import { useAuthStore } from '../lib/authStore'
import { useAvailableAdapters, useAdapters, createAdapter, updateAdapter, type AvailableAdapter } from '../lib/adaptersApi'
import { AdapterForm } from '../components/AdapterForm'
import { Button } from '../components/ui/Button'
import { Icon } from '../components/ui/Icon'
import { Logo } from '../components/ui/Logo'

type Step = 'password' | 'library' | 'search' | 'downloader' | 'done'

const PREV: Partial<Record<Step, Step>> = {
  search: 'library',
  downloader: 'search',
}

const NEXT: Record<Step, Step> = {
  password: 'library',
  library: 'search',
  search: 'downloader',
  downloader: 'done',
  done: 'done',
}

const STEP_COPY: Record<Exclude<Step, 'password' | 'done'>, { type: string; title: string; description: string }> = {
  library: { type: 'library', title: 'Choose a library', description: 'Where Reverb keeps and plays your music.' },
  search: { type: 'search', title: 'Add a search source', description: 'How Reverb finds music you do not have yet.' },
  downloader: { type: 'downloader', title: 'Downloads', description: 'How Reverb fetches the music you ask for.' },
}

// Ordered steps (excludes done) for the progress indicator
const ORDERED_STEPS: Exclude<Step, 'done'>[] = ['password', 'library', 'search', 'downloader']

function adapterDescription(type: string, name: string): string {
  if (type === 'library') return 'Connect an existing Navidrome or Subsonic-compatible server.'
  switch (name.toLowerCase()) {
    case 'deezer': return 'Search a large catalog with no account or API keys required.'
    case 'spotify': return 'Search Spotify’s catalog using your own app credentials.'
    case 'spotdl': return 'Change where the bundled downloader saves music. Spotify credentials are optional.'
    case 'lidarr': return 'Send album requests to a Lidarr server you already run.'
    default: return `Connect ${adapterDisplayName(name)}.`
  }
}

function stepIndex(step: Step): number {
  return ORDERED_STEPS.indexOf(step as Exclude<Step, 'done'>)
}

/** Shared branded shell: radial wash + wordmark + card */
function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen items-start justify-center bg-canvas pb-10 pt-[10vh]">
      <div
        className="pointer-events-none fixed inset-0"
        aria-hidden="true"
        style={{ background: 'radial-gradient(ellipse 70% 50% at 50% 0%, rgb(var(--color-accent)/0.12) 0%, transparent 70%)' }}
      />
      <div className="relative w-full max-w-md mx-4">
        {/* Wordmark */}
        <div className="mb-8 flex items-center justify-center">
          <Logo textClassName="text-2xl" />
        </div>
        <div className="rounded-2xl bg-surface shadow-pop border border-border-subtle p-8 space-y-6">
          {children}
        </div>
      </div>
    </div>
  )
}

/** Step progress dots */
function StepProgress({ current }: { current: Step }) {
  const idx = stepIndex(current)
  if (idx < 0) return null
  return (
    <div className="flex items-center gap-2" aria-label={`Step ${idx + 1} of ${ORDERED_STEPS.length}`}>
      {ORDERED_STEPS.map((s, i) => (
        <span
          key={s}
          className={[
            'h-1.5 rounded-full transition-all',
            i < idx
              ? 'bg-accent w-4'
              : i === idx
              ? 'bg-accent w-6'
              : 'bg-border-subtle w-4',
          ].join(' ')}
        />
      ))}
    </div>
  )
}

export default function Setup() {
  const qc = useQueryClient()
  const [step, setStep] = useState<Step>('password')
  const [username, setUsername] = useState('')
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const available = useAvailableAdapters()
  const configured = useAdapters()
  const [chosen, setChosen] = useState<AvailableAdapter | null>(null)
  const [spotifyCredentials, setSpotifyCredentials] = useState<Record<string, unknown>>({})
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  async function submitPassword(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    if (!username.trim() || !pw) {
      setErr('Enter a username and password to continue.')
      return
    }
    try {
      await setupOwner(username, pw)
      // The catalog query first ran pre-auth (401). Now that /setup/admin issued a
      // session, refetch it so the library/search/downloader steps show their adapters.
      await qc.invalidateQueries({ queryKey: ['adapters', 'available'] })
      await qc.invalidateQueries({ queryKey: ['adapters', 'list'] })
      await useAuthStore.getState().refresh()
      setStep('library')
    } catch {
      setErr('Could not complete setup. Please try again.')
    }
  }

  function advance() {
    setErr('')
    setChosen(null)
    setAdvancedOpen(false)
    setStep((s) => NEXT[s])
  }

  function goBack() {
    setErr('')
    setChosen(null)
    setAdvancedOpen(false)
    setStep((s) => PREV[s] ?? s)
  }

  /** Choosing an adapter: one that needs no settings (Deezer) is added on the
   *  spot; anything else opens its form. */
  async function choose(c: AvailableAdapter) {
    if ((c.configSchema?.fields ?? []).length > 0) {
      setChosen(c)
      return
    }
    if (busy) return
    setErr('')
    setBusy(true)
    try {
      const already = configured.data?.some((a) => a.type === c.type && a.name === c.name)
      if (!already) {
        await createAdapter({ type: c.type, name: c.name, enabled: true, priority: 0, config: {} })
        await qc.invalidateQueries({ queryKey: ['adapters', 'list'] })
      }
      advance()
    } catch {
      setErr(`Couldn't add ${adapterDisplayName(c.name)}. Please try again.`)
    } finally {
      setBusy(false)
    }
  }

  if (step === 'password') {
    return (
      <Shell>
        <StepProgress current="password" />
        <div className="space-y-1">
          <h1 className="text-xl font-bold text-text-primary">Welcome to Reverb</h1>
          <p className="text-sm text-text-secondary">Create your admin account to get started.</p>
        </div>
        <form onSubmit={submitPassword} className="space-y-4">
          <div className="space-y-1">
            <label htmlFor="setup-username" className="block text-sm font-medium text-text-secondary">
              Username
            </label>
            <input
              id="setup-username"
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="Choose a username"
              autoComplete="username"
              className="w-full rounded-lg bg-input border border-border-subtle px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="setup-pw" className="block text-sm font-medium text-text-secondary">
              Password
            </label>
            <input
              id="setup-pw"
              type="password"
              value={pw}
              onChange={(e) => setPw(e.target.value)}
              placeholder="Choose a password"
              autoComplete="new-password"
              className="w-full rounded-lg bg-input border border-border-subtle px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            />
          </div>
          {err && (
            <p className="text-sm text-error flex items-center gap-1.5" role="alert">
              <Icon name="warn" className="shrink-0 text-base" aria-hidden="true" />
              {err}
            </p>
          )}
          <Button type="submit" variant="primary" size="md">
            Continue
          </Button>
        </form>
      </Shell>
    )
  }

  if (step === 'done') {
    return (
      <Shell>
        <div className="flex flex-col items-center gap-4 text-center py-2">
          <span className="flex h-12 w-12 items-center justify-center rounded-full bg-accent/10">
            <Icon name="check" className="text-accent text-2xl" aria-hidden="true" />
          </span>
          <div className="space-y-1">
            <h1 className="text-xl font-bold text-text-primary">You're all set</h1>
            <p className="text-sm text-text-secondary">
              Setup is complete. Anything you skipped or want to change is in Settings.
            </p>
          </div>
          <Button type="button" variant="primary" size="md" onClick={() => window.location.reload()}>
            Go to Reverb
          </Button>
        </div>
      </Shell>
    )
  }

  const copy = STEP_COPY[step]
  const choices = (available.data ?? []).filter((a) => a.type === copy.type)
  const seededSpotdl = configured.data?.find((a) => a.type === 'downloader' && a.name === 'spotdl')
  const downloaderInitial = step === 'downloader'
    ? { output_dir: './downloads', ...seededSpotdl?.config, ...spotifyCredentials }
    : undefined
  // The bundled downloader is seeded on first run. When it is there, the step
  // only confirms it; the adapter list sits behind "Advanced options".
  const downloaderReady = step === 'downloader' && !!seededSpotdl
  const showChoices = !downloaderReady || advancedOpen

  return (
    <Shell>
      <StepProgress current={step} />
      <div className="space-y-1">
        <h1 className="text-xl font-bold text-text-primary">{copy.title}</h1>
        <p className="text-sm text-text-secondary">{copy.description}</p>
      </div>

      {err && (
        <p className="text-sm text-error flex items-center gap-1.5" role="alert">
          <Icon name="warn" className="shrink-0 text-base" aria-hidden="true" />
          {err}
        </p>
      )}

      {!chosen && (
        <div className="space-y-4">
          {step === 'library' && (
            <button
              type="button"
              aria-label="Use built-in library"
              onClick={async () => {
                setErr('')
                try {
                  await api.put('/settings', { libraryBackendMode: 'built-in' })
                  advance()
                } catch {
                  setErr("Couldn't save your library choice. Please try again.")
                }
              }}
              className="w-full rounded-xl border border-accent/50 bg-accent/5 p-4 text-left transition-colors hover:bg-accent/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              <div className="flex items-start gap-3">
                <span className="flex h-10 w-10 flex-none items-center justify-center rounded-lg bg-accent text-on-accent">
                  <Icon name="browse" className="text-lg" aria-hidden="true" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-2">
                    <span className="font-semibold text-text-primary">Built-in library</span>
                    <span className="rounded-full bg-accent px-2 py-0.5 text-[10px] font-bold uppercase tracking-wide text-on-accent">Recommended</span>
                  </span>
                  <span className="mt-1 block text-sm text-text-secondary">Reverb manages a music server for your folder — no extra setup.</span>
                </span>
              </div>
            </button>
          )}

          {/* Downloads: the bundled downloader already works, so this step is a
              confirmation. Its settings and other downloaders are optional. */}
          {downloaderReady && (
            <div className="rounded-xl border border-border-subtle bg-raised p-4" data-testid="downloader-ready">
              <div className="flex items-start gap-3">
                <span className="flex h-10 w-10 flex-none items-center justify-center rounded-lg bg-success/15 text-success">
                  <Icon name="check" className="text-lg" aria-hidden="true" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block font-semibold text-text-primary">spotDL is ready</span>
                  <span className="mt-1 block text-sm text-text-secondary">
                    The bundled downloader needs no setup.
                    {typeof seededSpotdl?.config.output_dir === 'string' && seededSpotdl.config.output_dir
                      ? ` Music is saved to ${seededSpotdl.config.output_dir}.`
                      : ''}
                  </span>
                </span>
              </div>
            </div>
          )}

          {showChoices && (
            <div className="space-y-3">
              {choices.map((c) => {
                const oneClick = (c.configSchema?.fields ?? []).length === 0
                return (
                  <button
                    key={c.name}
                    type="button"
                    disabled={busy}
                    onClick={() => void choose(c)}
                    className="group w-full rounded-xl border border-border-subtle bg-raised p-4 text-left transition-colors hover:border-accent hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-60"
                  >
                    <div className="flex items-start gap-3">
                      <span className="flex h-10 w-10 flex-none items-center justify-center rounded-lg bg-surface text-lg font-black text-text-secondary group-hover:text-accent">
                        {adapterDisplayName(c.name).charAt(0).toUpperCase()}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="flex items-center gap-2">
                          <span className="font-semibold text-text-primary">{adapterDisplayName(c.name)}</span>
                          {oneClick && step === 'search' && (
                            <span className="rounded-full bg-accent px-2 py-0.5 text-[10px] font-bold uppercase tracking-wide text-on-accent">Recommended</span>
                          )}
                        </span>
                        <span className="mt-1 block text-sm text-text-secondary">{adapterDescription(c.type, c.name)}</span>
                      </span>
                      <Icon name={oneClick ? 'plus' : 'fwd'} className="mt-1 text-text-muted group-hover:text-accent" aria-hidden="true" />
                    </div>
                  </button>
                )
              })}
              {choices.length === 0 && step !== 'library' && (
                <p className="text-sm text-text-muted">Nothing to set up here. You can add one later in Settings.</p>
              )}
            </div>
          )}

          <div className="flex items-center justify-between gap-3 pt-1">
            <div className="flex items-center gap-4">
              {PREV[step] && (
                <button
                  type="button"
                  onClick={goBack}
                  className="inline-flex items-center gap-1 rounded text-sm font-semibold text-text-secondary transition-colors hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                >
                  <Icon name="back" className="text-base" aria-hidden="true" />
                  Back
                </button>
              )}
              {downloaderReady && (
                <button
                  type="button"
                  aria-expanded={advancedOpen}
                  onClick={() => setAdvancedOpen((o) => !o)}
                  className="rounded text-sm text-text-muted transition-colors hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                >
                  {advancedOpen ? 'Hide advanced options' : 'Advanced options'}
                </button>
              )}
            </div>
            {downloaderReady ? (
              <Button type="button" variant="primary" size="md" onClick={advance}>
                Continue
              </Button>
            ) : (
              <button
                type="button"
                onClick={advance}
                className="rounded text-sm text-text-muted transition-colors hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                Skip this step
              </button>
            )}
          </div>
        </div>
      )}

      {chosen && (
        <div className="rounded-xl border border-border-subtle bg-raised p-4 space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="font-semibold text-text-primary">{adapterDisplayName(chosen.name)}</h3>
            <button
              type="button"
              onClick={() => setChosen(null)}
              className="text-text-muted hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded"
              aria-label="Back to the list"
            >
              <Icon name="back" className="text-base" aria-hidden="true" />
            </button>
          </div>
          <AdapterForm
            name={chosen.name}
            schema={chosen.configSchema}
            initial={chosen.name === 'spotdl' ? downloaderInitial : undefined}
            submitLabel="Add"
            onSubmit={async (config) => {
              setErr('')
              try {
                // Only the library step sets external backend mode; other steps are adapter-type-only.
                if (step === 'library') {
                  await api.put('/settings', { libraryBackendMode: 'external' })
                }
                if (step === 'search' && chosen.name === 'spotify') {
                  setSpotifyCredentials({
                    client_id: config.client_id,
                    client_secret: config.client_secret,
                  })
                }
                if (step === 'downloader' && chosen.name === 'spotdl' && seededSpotdl) {
                  await updateAdapter(seededSpotdl.id, {
                    name: seededSpotdl.name,
                    enabled: true,
                    priority: seededSpotdl.priority,
                    config,
                  })
                } else {
                  await createAdapter({ type: copy.type, name: chosen.name, enabled: true, priority: 0, config })
                }
                advance()
              } catch {
                setErr("Couldn't save this step. Please check the details and try again.")
              }
            }}
          />
        </div>
      )}
    </Shell>
  )
}
