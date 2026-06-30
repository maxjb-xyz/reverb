import { api, ApiError } from './api'

// ── Error type ────────────────────────────────────────────────────────────────

export class ScrobbleError extends Error {
  code: 'lastfm_not_configured' | 'lastfm_unavailable'

  constructor(code: 'lastfm_not_configured' | 'lastfm_unavailable', message: string) {
    super(message)
    this.name = 'ScrobbleError'
    this.code = code
  }
}

// ── Shared types ──────────────────────────────────────────────────────────────

export interface ScrobbleLink {
  provider: string
  username: string
  status: string
}

export interface LinksResult {
  configured: boolean
  links: ScrobbleLink[]
}

// ── Per-user scrobble API ─────────────────────────────────────────────────────

/** GET /api/v1/scrobble/links → { configured, links } */
export function getLinks(): Promise<LinksResult> {
  return api.get<LinksResult>('/scrobble/links')
}

/**
 * POST /api/v1/scrobble/lastfm/auth-url → { authUrl, token }
 *
 * Throws ScrobbleError with code:
 *   - 'lastfm_not_configured' on 400 { error: "lastfm_not_configured" }
 *   - 'lastfm_unavailable'    on any other error (5xx, network, etc.)
 */
export async function lastfmAuthUrl(): Promise<{ authUrl: string; token: string }> {
  try {
    return await api.post<{ authUrl: string; token: string }>('/scrobble/lastfm/auth-url')
  } catch (e) {
    if (e instanceof ApiError) {
      if (
        e.status === 400 &&
        e.body &&
        (e.body as Record<string, unknown>).error === 'lastfm_not_configured'
      ) {
        throw new ScrobbleError('lastfm_not_configured', 'Last.fm is not configured on this server')
      }
      throw new ScrobbleError('lastfm_unavailable', 'Last.fm is temporarily unavailable')
    }
    throw new ScrobbleError('lastfm_unavailable', 'Last.fm is temporarily unavailable')
  }
}

/** POST /api/v1/scrobble/lastfm/complete { token } → { username } */
export function lastfmComplete(token: string): Promise<{ username: string }> {
  return api.post<{ username: string }>('/scrobble/lastfm/complete', { token })
}

/** DELETE /api/v1/scrobble/lastfm → void */
export function lastfmDisconnect(): Promise<void> {
  return api.del<void>('/scrobble/lastfm')
}

/** POST /api/v1/scrobble/nowplaying → void (fire-and-forget) */
export function nowPlaying(track: {
  title: string
  artist: string
  album: string
  durationMs: number
}): Promise<void> {
  return api.post<void>('/scrobble/nowplaying', track)
}

// ── Admin: Last.fm app-key configuration ─────────────────────────────────────

/** GET /api/v1/admin/integrations/lastfm → { apiKey, apiSecretSet } */
export function getLastfmConfig(): Promise<{ apiKey: string; apiSecretSet: boolean }> {
  return api.get<{ apiKey: string; apiSecretSet: boolean }>('/admin/integrations/lastfm')
}

/** PUT /api/v1/admin/integrations/lastfm { apiKey, apiSecret } → void */
export function setLastfmConfig(cfg: { apiKey: string; apiSecret: string }): Promise<void> {
  return api.put<void>('/admin/integrations/lastfm', cfg)
}
