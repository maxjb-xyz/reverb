import { api } from './api'

// The optional Discord bot: admin setup and each user's account link.

export interface DiscordStatus {
  state: 'off' | 'connecting' | 'online' | 'error'
  botName?: string
  inviteUrl?: string
  error?: string
  sessions: number
  fromEnv: boolean
}

export interface DiscordConfig {
  tokenSet: boolean
  status: DiscordStatus
}

export interface DiscordLink {
  available: boolean
  linked: boolean
  discordName?: string
}

/** GET /admin/integrations/discord */
export function getDiscordConfig(): Promise<DiscordConfig> {
  return api.get<DiscordConfig>('/admin/integrations/discord')
}

/** PUT /admin/integrations/discord: save a token (or reconnect / clear). */
export function setDiscordConfig(body: { token?: string; clear?: boolean }): Promise<DiscordConfig> {
  return api.put<DiscordConfig>('/admin/integrations/discord', body)
}

/** GET /discord/link */
export function getDiscordLink(): Promise<DiscordLink> {
  return api.get<DiscordLink>('/discord/link')
}

/** POST /discord/link-code */
export function createDiscordLinkCode(): Promise<{ code: string; expiresAt: number }> {
  return api.post<{ code: string; expiresAt: number }>('/discord/link-code')
}

/** DELETE /discord/link */
export function unlinkDiscord(): Promise<void> {
  return api.del<void>('/discord/link')
}
