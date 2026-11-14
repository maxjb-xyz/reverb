import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

const api = vi.hoisted(() => ({
  getDiscordLink: vi.fn(),
  createDiscordLinkCode: vi.fn(),
  unlinkDiscord: vi.fn(),
}))
vi.mock('../../lib/discordApi', () => api)

import { DiscordLink } from './DiscordLink'

afterEach(() => vi.resetAllMocks())

describe('DiscordLink', () => {
  it('says so when the server has no bot', async () => {
    api.getDiscordLink.mockResolvedValue({ available: false, linked: false })
    render(<DiscordLink />)
    expect(await screen.findByText(/Not set up on this server/)).toBeTruthy()
  })

  it('gets a code to run in Discord', async () => {
    api.getDiscordLink.mockResolvedValue({ available: true, linked: false })
    api.createDiscordLinkCode.mockResolvedValue({ code: 'ABCD-EFGH', expiresAt: 0 })
    render(<DiscordLink />)
    fireEvent.click(await screen.findByText('Link Discord'))
    expect(await screen.findByText('/link code:ABCD-EFGH')).toBeTruthy()
  })

  it('shows the linked account and unlinks', async () => {
    api.getDiscordLink.mockResolvedValue({ available: true, linked: true, discordName: 'Dee' })
    api.unlinkDiscord.mockResolvedValue(undefined)
    render(<DiscordLink />)
    expect(await screen.findByText('Dee')).toBeTruthy()
    api.getDiscordLink.mockResolvedValue({ available: true, linked: false })
    fireEvent.click(screen.getByText('Unlink'))
    await waitFor(() => expect(api.unlinkDiscord).toHaveBeenCalled())
    expect(await screen.findByText('Link Discord')).toBeTruthy()
  })
})
