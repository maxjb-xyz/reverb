import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import { useAvailableAdapters, createAdapter, type AvailableAdapter } from '../lib/adaptersApi'
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
  downloader: { type: 'downloader', title: 'Add a Downloader', description: 'Select a service to download tracks.' },
}

// Ordered steps (excludes done) for the progress indicator
const ORDERED_STEPS: Exclude<Step, 'done'>[] = ['password', 'library', 'search', 'downloader']

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
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const available = useAvailableAdapters()
  const [chosen, setChosen] = useState<AvailableAdapter | null>(null)

  async function submitPassword(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    try {
      await api.post('/setup/admin', { password: pw })
      // The catalog query first ran pre-auth (401). Now that /setup/admin issued a
      // session, refetch it so the library/search/downloader steps show their adapters.
      await qc.invalidateQueries({ queryKey: ['adapters', 'available'] })
      setStep('library')
    } catch {
      setErr('Could not complete setup. Please try again.')
    }
  }

  function advance() {
    setChosen(null)
    setStep((s) => NEXT[s])
  }

  if (step === 'password') {
    return (
      <Shell>
        <StepProgress current="password" />
        <div className="space-y-1">
          <h1 className="text-xl font-bold text-text-primary">Welcome to Reverb</h1>
          <p className="text-sm text-text-secondary">Set an admin password to get started.</p>
        </div>
        <form onSubmit={submitPassword} className="space-y-4">
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

  return (
    <Shell>
      <StepProgress current={step} />
      <div className="space-y-1">
        <h1 className="text-xl font-bold text-text-primary">{copy.title}</h1>
        <p className="text-sm text-text-secondary">{copy.description}</p>
      </div>

      {!chosen && (
        <div className="space-y-4">
          <div className="flex flex-wrap gap-2">
            {choices.map((c) => (
              <button
                key={c.name}
                type="button"
                onClick={() => setChosen(c)}
                className="rounded-full border border-border-subtle px-3 py-1.5 text-sm text-text-primary hover:bg-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
              >
                {c.name}
              </button>
            ))}
            {choices.length === 0 && (
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
            submitLabel="Add"
            onSubmit={async (config) => {
              await createAdapter({ type: copy.type, name: chosen.name, enabled: true, priority: 0, config })
              advance()
            }}
          />
        </div>
      )}
    </Shell>
  )
}
