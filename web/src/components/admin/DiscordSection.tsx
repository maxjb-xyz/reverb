import { useEffect, useState } from 'react'
import { Button } from '../ui'
import { getDiscordConfig, setDiscordConfig, type DiscordConfig } from '../../lib/discordApi'

// Admin: the Discord bot. Paste a bot token and Reverb logs in; linked users
// can then play their session in a voice channel with /join.

const STATE_LABEL: Record<DiscordConfig['status']['state'], string> = {
  off: 'Off',
  connecting: 'Connecting…',
  online: 'Online',
  error: 'Not connected',
}

export function DiscordSection() {
  const [cfg, setCfg] = useState<DiscordConfig | null>(null)
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [loadError, setLoadError] = useState(false)

  useEffect(() => {
    void getDiscordConfig()
      .then(setCfg)
      .catch(() => setLoadError(true))
  }, [])

  async function save(body: { token?: string; clear?: boolean }) {
    setBusy(true)
    try {
      setCfg(await setDiscordConfig(body))
      setToken('')
    } catch {
      setLoadError(true)
    } finally {
      setBusy(false)
    }
  }

  const st = cfg?.status
  const online = st?.state === 'online'

  return (
    <section data-testid="discord-section" className="rounded-lg border border-border-subtle bg-raised p-6 space-y-4">
      <div>
        <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Discord</h2>
        <p className="text-xs text-text-secondary mt-0.5">
          A bot that plays people&apos;s Reverb sessions in a voice channel. Each person links their Discord account
          in Settings, then uses <code>/join</code> in a server the bot is in.
        </p>
      </div>

      {loadError && !cfg && <p className="text-xs text-text-secondary">Could not load the Discord settings.</p>}

      {cfg && (
        <>
          <p className="text-sm" data-testid="discord-status">
            <span
              className={[
                'inline-block h-2 w-2 rounded-full mr-2 align-middle',
                online ? 'bg-success' : st?.state === 'error' ? 'bg-error' : 'bg-text-muted',
              ].join(' ')}
            />
            <span className="font-semibold text-text-primary">{STATE_LABEL[st?.state ?? 'off']}</span>
            {online && st?.botName && <span className="text-text-secondary"> as {st.botName}</span>}
            {online && st && st.sessions > 0 && (
              <span className="text-text-secondary"> · playing in {st.sessions} channel{st.sessions === 1 ? '' : 's'}</span>
            )}
          </p>
          {st?.error && <p className="text-xs text-error">{st.error}</p>}

          {online && st?.inviteUrl && (
            <p className="text-sm">
              <a href={st.inviteUrl} target="_blank" rel="noreferrer" className="font-semibold text-accent hover:underline">
                Add the bot to a server
              </a>
              <span className="text-xs text-text-secondary"> (needs Manage Server there)</span>
            </p>
          )}

          {st?.fromEnv ? (
            <p className="text-xs text-text-secondary">The token comes from REVERB_DISCORD_TOKEN on the server.</p>
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault()
                void save({ token })
              }}
              className="space-y-3 max-w-sm"
            >
              <div>
                <label htmlFor="discord-token" className="block text-xs font-semibold text-text-secondary mb-1">
                  Bot token
                </label>
                <input
                  id="discord-token"
                  type="password"
                  autoComplete="off"
                  placeholder={cfg.tokenSet ? 'saved' : 'Paste the bot token'}
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  className="w-full h-9 px-3 rounded-md bg-input text-sm text-text-primary border border-border-subtle focus:outline-none focus:ring-2 focus:ring-accent"
                />
                {!cfg.tokenSet && (
                  <p className="text-xs text-text-secondary mt-1 leading-relaxed">
                    Create an application at{' '}
                    <a href="https://discord.com/developers/applications" target="_blank" rel="noreferrer" className="text-accent hover:underline">
                      discord.com/developers
                    </a>
                    , open Bot, press Reset Token and paste it here. No privileged intents are needed.
                  </p>
                )}
              </div>
              <div className="flex gap-2">
                <Button type="submit" variant="secondary" disabled={busy || (!token && !cfg.tokenSet)}>
                  {busy ? 'Connecting…' : token || !cfg.tokenSet ? 'Save' : 'Reconnect'}
                </Button>
                {cfg.tokenSet && (
                  <Button type="button" variant="ghost" disabled={busy} onClick={() => void save({ clear: true })}>
                    Turn off
                  </Button>
                )}
              </div>
            </form>
          )}
        </>
      )}
    </section>
  )
}
