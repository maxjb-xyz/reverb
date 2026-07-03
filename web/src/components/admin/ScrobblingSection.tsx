import { useEffect, useState } from 'react'
import { Button } from '../ui'
import { getLastfmConfig, setLastfmConfig } from '../../lib/scrobbleApi'

// ── ScrobblingSection ─────────────────────────────────────────────────────────
// Admin-only: configure the Last.fm API key + secret for this Reverb instance.
// Lives on the /admin page (Providers tab), NOT in per-user Account settings.

export function ScrobblingSection() {
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
      if (apiSecret) {
        setApiSecretSet(true)
      }
      setApiSecret('')
    } finally {
      setSaving(false)
    }
  }

  if (loadError) {
    return (
      <section className="rounded-lg border border-border-subtle bg-raised p-6 space-y-4">
        <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Last.fm</h2>
        <p className="text-xs text-text-secondary">Could not load configuration.</p>
      </section>
    )
  }

  return (
    <section className="rounded-lg border border-border-subtle bg-raised p-6 space-y-4">
      <div>
        <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Last.fm</h2>
        <p className="text-xs text-text-secondary mt-0.5">
          API credentials for this Reverb instance. Users can connect their own Last.fm accounts
          once these are set.
        </p>
      </div>

      <form onSubmit={(e) => void handleSave(e)} className="space-y-3 max-w-sm">
        <div>
          <label
            htmlFor="lastfm-admin-api-key"
            className="block text-xs font-semibold text-text-secondary mb-1"
          >
            API Key
          </label>
          <input
            id="lastfm-admin-api-key"
            type="text"
            placeholder="API key"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            className="w-full h-9 px-3 rounded-md bg-input text-sm text-text-primary border border-border-subtle focus:outline-none focus:ring-2 focus:ring-accent"
          />
        </div>

        <div>
          <label
            htmlFor="lastfm-admin-api-secret"
            className="block text-xs font-semibold text-text-secondary mb-1"
          >
            API Secret
          </label>
          <input
            id="lastfm-admin-api-secret"
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
    </section>
  )
}
