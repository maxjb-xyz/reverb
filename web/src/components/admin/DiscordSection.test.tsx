import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

const api = vi.hoisted(() => ({ getDiscordConfig: vi.fn(), setDiscordConfig: vi.fn() }))
vi.mock('../../lib/discordApi', () => api)

import { DiscordSection } from './DiscordSection'

afterEach(() => vi.resetAllMocks())

describe('DiscordSection', () => {
  it('saves a token and shows the bot online with an invite link', async () => {
    api.getDiscordConfig.mockResolvedValue({ tokenSet: false, status: { state: 'off', sessions: 0, fromEnv: false } })
    api.setDiscordConfig.mockResolvedValue({
      tokenSet: true,
      status: { state: 'online', botName: 'Reverb', inviteUrl: 'https://discord.example/invite', sessions: 0, fromEnv: false },
    })
    render(<DiscordSection />)
    fireEvent.change(await screen.findByLabelText('Bot token'), { target: { value: 'tok' } })
    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(api.setDiscordConfig).toHaveBeenCalledWith({ token: 'tok' }))
    expect(await screen.findByText('Online')).toBeTruthy()
    expect(screen.getByText('Add the bot to a server').getAttribute('href')).toBe('https://discord.example/invite')
  })

  it('shows why the bot is not connected', async () => {
    api.getDiscordConfig.mockResolvedValue({
      tokenSet: true,
      status: { state: 'error', error: 'Discord rejected the token.', sessions: 0, fromEnv: false },
    })
    render(<DiscordSection />)
    expect(await screen.findByText('Discord rejected the token.')).toBeTruthy()
    expect(screen.getByText('Reconnect')).toBeTruthy()
  })
})
