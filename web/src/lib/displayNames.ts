// Human-facing names for identifiers that otherwise leak into the UI as raw
// keys ("spotdl", "can_manage_users").

const ADAPTER_NAMES: Record<string, string> = {
  deezer: 'Deezer',
  spotify: 'Spotify',
  spotdl: 'spotDL',
  lidarr: 'Lidarr',
  subsonic: 'Subsonic',
  navidrome: 'Navidrome',
  embedded: 'Built-in library',
  library: 'Library',
  elevenlabs: 'ElevenLabs',
  kokoro: 'Built-in voice (Kokoro)',
  anthropic: 'Claude (Anthropic)',
  'openai-chat': 'OpenAI-compatible chat',
  'audio-sidecar': 'Analysis service',
  'openai-speech': 'OpenAI-compatible speech',
}

/** Proper product name for an adapter/source key; unknown keys are capitalised. */
export function adapterDisplayName(key: string): string {
  const known = ADAPTER_NAMES[key.toLowerCase()]
  if (known) return known
  return key ? key.charAt(0).toUpperCase() + key.slice(1) : key
}

// Mirrors auth.AllCapabilities() labels on the server.
const CAPABILITY_LABELS: Record<string, string> = {
  is_admin: 'Full administrator',
  can_manage_users: 'Manage users & roles',
  can_manage_library: 'Manage library & integrations',
  request: 'Request music',
  auto_approve: 'Auto-approve music',
  manage_requests: 'Approve requests',
  can_create_playlists: 'Create & edit playlists',
}

/** Readable label for a capability key; unknown keys become spaced words. */
export function capabilityLabel(key: string): string {
  const known = CAPABILITY_LABELS[key]
  if (known) return known
  const words = key.replace(/^can_/, '').replace(/_/g, ' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}
