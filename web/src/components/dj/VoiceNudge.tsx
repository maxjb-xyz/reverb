import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { createAdapter, listAdapters } from '../../lib/adaptersApi'
import { useAuthStore } from '../../lib/authStore'
import { setDJVoice, useDJ } from '../../lib/dj'
import { useToastStore } from '../../lib/toastStore'

const DISMISSED_KEY = 'reverb.dj.voiceNudge.dismissed'

function dismissedBefore(): boolean {
  try {
    return localStorage.getItem(DISMISSED_KEY) === '1'
  } catch {
    return false
  }
}

/**
 * When the DJ has no voice, offer the people who manage the server a one-tap
 * built-in voice. It runs on the server's CPU and needs no account; setting it
 * up downloads the voice model once.
 */
export function VoiceNudge() {
  const voice = useDJ((s) => s.voice)
  const canManage = useAuthStore((s) => s.can('can_manage_library'))
  const push = useToastStore((s) => s.push)
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [hidden, setHidden] = useState(dismissedBefore)
  const [working, setWorking] = useState(false)
  if (voice || !canManage || hidden) return null

  async function turnOn() {
    setWorking(true)
    try {
      // A voice that exists but isn't working is better looked at than duplicated.
      if ((await listAdapters()).some((a) => a.type === 'voice')) {
        navigate('/admin')
        return
      }
      await createAdapter({ type: 'voice', name: 'kokoro', enabled: true, priority: 0, config: {} })
      void qc.invalidateQueries({ queryKey: ['adapters'] })
      setDJVoice(true)
      push('Setting up the built-in voice. The first time takes a minute or two while it downloads.', 'info')
    } catch {
      push("Couldn't turn on the voice. You can set one up under Admin, Providers.", 'error')
    } finally {
      setWorking(false)
    }
  }

  function dismiss() {
    try {
      localStorage.setItem(DISMISSED_KEY, '1')
    } catch {
      /* remembering is a nicety */
    }
    setHidden(true)
  }

  return (
    <div data-testid="dj-voice-nudge" className="mx-3 mb-2 flex items-center gap-2 rounded-lg bg-white/[.04] px-3 py-2 text-sm">
      <span className="min-w-0 flex-1 text-text-secondary">The DJ has no voice yet.</span>
      <button
        type="button"
        onClick={() => void turnOn()}
        disabled={working}
        className="flex-none rounded-full px-3 py-1 text-xs font-semibold bg-accent text-on-accent hover:brightness-110 disabled:opacity-60 transition"
      >
        {working ? 'Turning on…' : 'Turn on built-in voice'}
      </button>
      <button type="button" onClick={dismiss} aria-label="Not now" className="flex-none text-xs text-text-muted hover:text-text-primary">
        Not now
      </button>
    </div>
  )
}
