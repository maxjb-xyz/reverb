import { useEffect, useState } from 'react'
import { Button } from '../ui'
import { createDiscordLinkCode, getDiscordLink, unlinkDiscord, type DiscordLink as Link } from '../../lib/discordApi'

// Settings > Integrations: link this Reverb account to Discord, so the bot
// can play this account's session in a voice channel.
export function DiscordLink() {
  const [link, setLink] = useState<Link | null>(null)
  const [code, setCode] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)
  const [copied, setCopied] = useState(false)

  function refresh() {
    void getDiscordLink()
      .then((l) => {
        setLink(l)
        if (l.linked) setCode(null)
      })
      .catch(() => setError(true))
  }
  useEffect(refresh, [])

  // While a code is showing, notice when it gets used.
  useEffect(() => {
    if (!code) return
    const t = setInterval(refresh, 4000)
    return () => clearInterval(t)
  }, [code])

  async function getCode() {
    setBusy(true)
    setError(false)
    try {
      setCode((await createDiscordLinkCode()).code)
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  async function unlink() {
    setBusy(true)
    try {
      await unlinkDiscord()
      refresh()
    } finally {
      setBusy(false)
    }
  }

  if (!link) {
    return <span className="text-xs text-text-secondary">{error ? "Couldn't load." : 'Loading…'}</span>
  }
  if (link.linked) {
    return (
      <div className="flex items-center gap-3" data-testid="discord-linked">
        <span className="text-xs text-text-secondary">
          Linked as <span className="font-semibold text-text-primary">{link.discordName}</span>
        </span>
        <Button variant="ghost" disabled={busy} onClick={() => void unlink()}>
          Unlink
        </Button>
      </div>
    )
  }
  if (!link.available) {
    return <span className="text-xs text-text-secondary">Not set up on this server. Ask an administrator.</span>
  }
  if (code) {
    const cmd = `/link code:${code}`
    return (
      <div className="space-y-1.5 text-right" data-testid="discord-code">
        <p className="text-xs text-text-secondary">Run this in any server with the Reverb bot:</p>
        <button
          type="button"
          onClick={() => {
            void navigator.clipboard?.writeText(cmd).then(() => setCopied(true))
          }}
          className="rounded-md bg-input px-3 py-1.5 font-mono text-sm font-semibold text-text-primary hover:bg-raised-hover"
          title="Copy"
        >
          {cmd}
        </button>
        <p className="text-[11px] text-text-muted">{copied ? 'Copied. ' : ''}Good for 10 minutes.</p>
      </div>
    )
  }
  return (
    <Button variant="secondary" disabled={busy} onClick={() => void getCode()}>
      {busy ? 'Getting a code…' : 'Link Discord'}
    </Button>
  )
}
