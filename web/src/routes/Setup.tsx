import { useState } from 'react'
import { api } from '../lib/api'
import { useAvailableAdapters, createAdapter, type AvailableAdapter } from '../lib/adaptersApi'
import { AdapterForm } from '../components/AdapterForm'

type Step = 'password' | 'library' | 'search' | 'downloader' | 'done'

const NEXT: Record<Step, Step> = {
  password: 'library',
  library: 'search',
  search: 'downloader',
  downloader: 'done',
  done: 'done',
}

const STEP_COPY: Record<Exclude<Step, 'password' | 'done'>, { type: string; title: string }> = {
  library: { type: 'library', title: 'Add a Library' },
  search: { type: 'search', title: 'Add a Search source' },
  downloader: { type: 'downloader', title: 'Add a Downloader' },
}

export default function Setup() {
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
      <form onSubmit={submitPassword} className="max-w-sm mx-auto mt-24 space-y-4">
        <h1 className="text-2xl font-bold">Welcome to Crate</h1>
        <p className="text-neutral-400 text-sm">Set an admin password to get started.</p>
        <input
          type="password"
          value={pw}
          onChange={(e) => setPw(e.target.value)}
          className="w-full rounded bg-neutral-900 border border-neutral-700 px-3 py-2"
          placeholder="Choose a password"
        />
        {err && <p className="text-accent text-sm">{err}</p>}
        <button type="submit" className="w-full rounded bg-accent py-2 font-medium text-white">Continue</button>
      </form>
    )
  }

  if (step === 'done') {
    return (
      <div className="max-w-md mx-auto mt-24 space-y-4 text-center">
        <h1 className="text-2xl font-bold">You're all set</h1>
        <p className="text-neutral-400 text-sm">
          Setup complete. Restart Crate so your library, search, and downloader become active, then log in.
        </p>
        <button type="button" onClick={() => window.location.reload()} className="rounded bg-accent px-6 py-2 font-medium text-white">
          Go to Crate
        </button>
      </div>
    )
  }

  const copy = STEP_COPY[step]
  const choices = (available.data ?? []).filter((a) => a.type === copy.type)

  return (
    <div className="max-w-md mx-auto mt-20 space-y-4">
      <h1 className="text-2xl font-bold">{copy.title}</h1>
      {!chosen && (
        <div className="space-y-3">
          <div className="flex flex-wrap gap-2">
            {choices.map((c) => (
              <button key={c.name} type="button" onClick={() => setChosen(c)} className="rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800">
                {c.name}
              </button>
            ))}
            {choices.length === 0 && <p className="text-sm text-neutral-500">No adapters available for this step.</p>}
          </div>
          <button type="button" onClick={advance} className="text-sm text-neutral-400">Skip this step</button>
        </div>
      )}
      {chosen && (
        <div className="rounded border border-neutral-700 p-4">
          <h3 className="mb-3 font-semibold">{chosen.name}</h3>
          <AdapterForm
            name={chosen.name}
            schema={chosen.configSchema}
            submitLabel="Add"
            onSubmit={async (config) => {
              await createAdapter({ type: copy.type, name: chosen.name, enabled: true, priority: 0, config })
              advance()
            }}
          />
          <button type="button" onClick={() => setChosen(null)} className="mt-2 text-sm text-neutral-400">Back</button>
        </div>
      )}
    </div>
  )
}
