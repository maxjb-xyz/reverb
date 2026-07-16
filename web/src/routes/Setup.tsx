import { useState } from 'react'
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

const NEXT: Record<Step, Step> = {
  password: 'library',
  library: 'search',
  search: 'downloader',
  downloader: 'done',
  done: 'done',
}

const STEP_COPY: Record<Exclude<Step, 'password' | 'done'>, { type: string; title: string; description: string }> = {
  library: { type: 'library', title: 'Add a Library', description: 'Connect a source for your music collection.' },
  search: { type: 'search', title: 'Add a Search source', description: 'Choose how Reverb finds music.' },
  downloader: { type: 'downloader', title: 'Configure Downloads', description: 'spotDL is bundled and ready to use.' },
}

// Ordered steps (excludes done) for the progress indicator
const ORDERED_STEPS: Exclude<Step, 'done'>[] = ['password', 'library', 'search', 'downloader']

function adapterDescription(type: string, name: string): string {
  if (type === 'library') return 'Connect an existing Navidrome or Subsonic-compatible server.'
  switch (name.toLowerCase()) {
    case 'deezer': return 'Search a large catalog with no account or API keys required.'
    case 'spotify': return 'Search Spotify’s catalog using your own app credentials.'
    case 'spotdl': return 'Download music with the bundled downloader. Spotify credentials are optional.'
    default: return `Connect ${name} to expand your music search.`
  }
}

function stepIndex(step: Step): number {
  return ORDERED_STEPS.indexOf(step as Exclude<Step, 'done'>)
}

/** Shared branded shell: radial wash + wordmark + card */
function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-canvas flex items-center justify-center">
      <div
        className="pointer-events-none fixed inset-0"
        aria-hidden="true"
        style={{ background: 'radial-gradient(ellipse 70% 50% at 50% 0%, rgb(var(--color-accent)/0.12) 0%, transparent 70%)' }}
      />
      <div className="relative w-full max-w-md mx-4">
        {/* Wordmark */}
        <div className="mb-8 flex items-center justify-center">
          <Logo iconClassName="h-9 w-auto" textClassName="text-2xl" />
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
    setStep((s) => NEXT[s])
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
              Admin password
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
            <p className="text-sm text-accent flex items-center gap-1.5" role="alert">
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
              Setup complete. Your library, search, and downloader are ready.
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

  return (
    <Shell>
      <StepProgress current={step} />
      <div className="space-y-1">
        <h1 className="text-xl font-bold text-text-primary">{copy.title}</h1>
        <p className="text-sm text-text-secondary">{copy.description}</p>
      </div>

      {err && (
        <p className="text-sm text-accent flex items-center gap-1.5" role="alert">
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
          <div className="space-y-3">
            {choices.map((c) => (
              <button
                key={c.name}
                type="button"
                onClick={() => setChosen(c)}
                className="group w-full rounded-xl border border-border-subtle bg-raised p-4 text-left transition-colors hover:border-accent hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                <div className="flex items-start gap-3">
                  <span className="flex h-10 w-10 flex-none items-center justify-center rounded-lg bg-surface text-lg font-black text-text-secondary group-hover:text-accent">
                    {c.name.charAt(0).toUpperCase()}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block font-semibold capitalize text-text-primary">{c.name}</span>
                    <span className="mt-1 block text-sm text-text-secondary">{adapterDescription(c.type, c.name)}</span>
                  </span>
                  <Icon name="fwd" className="mt-1 text-text-muted group-hover:text-accent" aria-hidden="true" />
                </div>
              </button>
            ))}
            {choices.length === 0 && step !== 'library' && (
              <p className="text-sm text-text-muted">No adapters available for this step.</p>
            )}
          </div>
          <button
            type="button"
            onClick={advance}
            className="text-sm text-text-muted hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded transition-colors"
          >
            Skip this step
          </button>
        </div>
      )}

      {chosen && (
        <div className="rounded-xl border border-border-subtle bg-raised p-4 space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="font-semibold text-text-primary">{chosen.name}</h3>
            <button
              type="button"
              onClick={() => setChosen(null)}
              className="text-text-muted hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded"
              aria-label="Back to adapter list"
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
