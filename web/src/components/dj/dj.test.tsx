import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

const get = vi.fn()
const post = vi.fn()
vi.mock('../../lib/api', async (orig) => ({
  ...(await orig<typeof import('../../lib/api')>()),
  api: { get: (p: string) => get(p), post: (p: string, b?: unknown) => post(p, b) },
}))
const playTrackList = vi.fn()
vi.mock('../../lib/playerStore', () => ({
  usePlayer: (sel: (s: { playTrackList: typeof playTrackList }) => unknown) => sel({ playTrackList }),
}))
const navigate = vi.fn()
vi.mock('react-router-dom', async (orig) => ({ ...(await orig<typeof import('react-router-dom')>()), useNavigate: () => navigate }))
const push = vi.fn()
vi.mock('../../lib/toastStore', () => ({
  useToastStore: (sel: (s: { push: typeof push }) => unknown) => sel({ push }),
}))

import { DJTile } from './DJTile'
import { DJControl } from './DJControl'
import { _resetDJ, useDJ } from '../../lib/dj'
import { useAuthStore } from '../../lib/authStore'
import { ApiError } from '../../lib/api'

function signIn(capabilities: string[]) {
  useAuthStore.setState({ me: { id: 'u1', username: 'max', roleId: 'r', roleName: 'Owner', isOwner: true, capabilities, createdAt: 0 } })
}

const track = (id: string) => ({ id, title: `Song ${id}`, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Bonobo', coverArtId: 'c', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg' })
const lane = (name: string, ids: string[]) => ({
  station: { id: `lane:${name}:taste`, kind: 'taste', lane: name, label: name === 'on-repeat' ? 'On repeat' : 'Sounds like Bonobo' },
  items: ids.map((id) => ({ owned: true, track: track(id), score: 1, reason: { kind: 'on_repeat', playCount: 9 } })),
})

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  get.mockImplementation(async (p: string) =>
    p === '/dj/status' ? { voice: { available: false, name: '' }, writer: { available: false, name: '' } } : null,
  )
  post.mockImplementation(async (p: string, b: { lane?: string }) => {
    if (p === '/engine/next') return b.lane === 'on-repeat' ? lane('on-repeat', ['a1', 'a2', 'a3', 'a4']) : lane(b.lane ?? '', ['b1', 'b2', 'b3', 'b4'])
    if (p === '/dj/line') return { text: 'You have had this on repeat.', source: 'template' }
    if (p === '/dj/request') return { understood: true, seed: { kind: 'artist', id: 'bonobo', artist: 'Bonobo' }, lane: 'sounds-like', source: 'rules' }
    throw new Error(`unexpected ${p}`)
  })
})

afterEach(() => {
  act(() => _resetDJ())
  useAuthStore.setState({ me: null })
  localStorage.clear()
})

describe('DJTile', () => {
  it('starts the DJ on the first lane', async () => {
    wrap(<DJTile />)
    fireEvent.click(screen.getByRole('button', { name: 'Start the DJ' }))
    await waitFor(() =>
      expect(playTrackList).toHaveBeenCalledWith([track('a1'), track('a2'), track('a3'), track('a4')], 0, { kind: 'dj', id: 'dj' }),
    )
    expect(post).toHaveBeenCalledWith('/engine/next', { lane: 'on-repeat', n: 4, exclude: [] })
    expect(useDJ.getState().active).toBe(true)
  })

  it('explains when there is nothing to play yet', async () => {
    post.mockImplementation(async () => ({ station: { id: 'x', kind: 'taste', label: 'x' }, items: [] }))
    wrap(<DJTile />)
    fireEvent.click(screen.getByRole('button', { name: 'Start the DJ' }))
    await waitFor(() => expect(push).toHaveBeenCalledWith(expect.stringMatching(/Not enough in your library for the DJ/), 'error'))
    expect(playTrackList).not.toHaveBeenCalled()
  })
})

/** Start the DJ from its tile and open its controls in the player. */
async function startAndOpen() {
  fireEvent.click(screen.getByRole('button', { name: 'Start the DJ' }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'DJ' })).toHaveAttribute('aria-expanded', 'false'))
  fireEvent.click(screen.getByRole('button', { name: 'DJ' }))
  await screen.findByTestId('dj-panel')
}

/** Ask the DJ for something through its panel. */
function askFor(text: string) {
  fireEvent.change(screen.getByRole('textbox', { name: 'Ask the DJ' }), { target: { value: text } })
  fireEvent.submit(screen.getByRole('textbox', { name: 'Ask the DJ' }))
}

describe('DJControl', () => {
  it('starts the DJ from the player while it is off', async () => {
    wrap(<DJControl />)
    expect(screen.getByRole('button', { name: 'DJ' })).toHaveAttribute('title', 'Start the DJ')
    expect(screen.queryByTestId('dj-panel')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'DJ' }))
    await waitFor(() => expect(useDJ.getState().active).toBe(true))
  })

  it('stays out of the phone player while the DJ is off', () => {
    wrap(<DJControl look="mini" />)
    expect(screen.queryByTestId('dj-button')).toBeNull()
  })

  it('shows the latest line in a bubble, opens the controls and turns the DJ off', () => {
    act(() => useDJ.setState({ active: true, label: 'On repeat', card: { key: 1, label: 'On repeat', text: 'Nine plays this week.' } }))
    wrap(<DJControl />)
    expect(screen.getByTestId('dj-line')).toHaveTextContent('Nine plays this week.')
    expect(screen.queryByTestId('dj-panel')).toBeNull()
    fireEvent.click(screen.getByTestId('dj-line'))
    expect(screen.getByTestId('dj-lane')).toHaveTextContent('On repeat')
    expect(screen.getAllByTestId('dj-line')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: 'Turn off the DJ' }))
    expect(useDJ.getState().active).toBe(false)
    expect(screen.queryByTestId('dj-panel')).toBeNull()
  })

  it('closes the controls on Escape and on a click outside', () => {
    act(() => useDJ.setState({ active: true, label: 'On repeat' }))
    wrap(<><p>page</p><DJControl /></>)
    fireEvent.click(screen.getByRole('button', { name: 'DJ' }))
    expect(screen.getByTestId('dj-panel')).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByTestId('dj-panel')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'DJ' }))
    fireEvent.mouseDown(screen.getByText('page'))
    expect(screen.queryByTestId('dj-panel')).toBeNull()
  })

  it('changes it up to the next lane', async () => {
    wrap(<><DJTile /><DJControl /></>)
    await startAndOpen()
    fireEvent.click(screen.getByRole('button', { name: 'Change it up' }))
    await waitFor(() => expect(screen.getByTestId('dj-lane')).toHaveTextContent('Sounds like Bonobo'))
    expect(playTrackList).toHaveBeenLastCalledWith([track('b1'), track('b2'), track('b3'), track('b4')], 0, { kind: 'dj', id: 'dj' })
  })

  it('takes a request and plays it', async () => {
    wrap(<><DJTile /><DJControl /></>)
    await startAndOpen()
    fireEvent.change(screen.getByRole('textbox', { name: 'Ask the DJ' }), { target: { value: 'more like Bonobo' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send to the DJ' }))
    await waitFor(() => expect(screen.getByTestId('dj-lane')).toHaveTextContent('More like Bonobo'))
    expect(post).toHaveBeenCalledWith('/dj/request', { text: 'more like Bonobo' })
    expect(post).toHaveBeenCalledWith('/engine/next', expect.objectContaining({ seed: { kind: 'artist', id: 'bonobo', artist: 'Bonobo' }, lane: 'sounds-like' }))
    expect(screen.getByRole('textbox', { name: 'Ask the DJ' })).toHaveValue('')
  })

  it('offers to find an artist the library lacks', async () => {
    post.mockImplementation(async (p: string, b: { lane?: string }) => {
      if (p === '/engine/next') return lane(b.lane ?? 'on-repeat', ['a1'])
      if (p === '/dj/line') return { text: 'Hi.', source: 'template' }
      if (p === '/dj/request') return { understood: false, missing: 'Radiohead', source: 'llm' }
      throw new Error(p)
    })
    wrap(<><DJTile /><DJControl /></>)
    await startAndOpen()
    askFor('radiohed')
    await screen.findByTestId('dj-missing')
    expect(screen.getByTestId('dj-missing')).toHaveTextContent("Radiohead isn't in your library yet.")
    expect(push).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Find it' }))
    expect(navigate).toHaveBeenCalledWith('/search?q=Radiohead')
  })

  it("says so when it doesn't understand, with library names to try", async () => {
    post.mockImplementation(async (p: string, b: { lane?: string }) => {
      if (p === '/engine/next') return lane(b.lane ?? 'on-repeat', ['a1'])
      if (p === '/dj/line') return { text: 'Hi.', source: 'template' }
      if (p === '/dj/request') return { understood: false, source: 'rules', try: ['A$AP Rocky', 'Hip hop rap'] }
      throw new Error(p)
    })
    wrap(<><DJTile /><DJControl /></>)
    await startAndOpen()
    askFor('blorp')
    await waitFor(() =>
      expect(push).toHaveBeenCalledWith("The DJ didn't catch that. Try “A$AP Rocky” or “Hip hop rap”, a decade or a mood.", 'info'),
    )
  })

  it("says the DJ couldn't be reached rather than that it didn't understand", async () => {
    post.mockImplementation(async (p: string, b: { lane?: string }) => {
      if (p === '/engine/next') return lane(b.lane ?? 'on-repeat', ['a1'])
      if (p === '/dj/line') return { text: 'Hi.', source: 'template' }
      if (p === '/dj/request') throw new ApiError('POST', '/dj/request', 429)
      throw new Error(p)
    })
    wrap(<><DJTile /><DJControl /></>)
    await startAndOpen()
    askFor('some rap')
    await waitFor(() => expect(push).toHaveBeenCalledWith('Give the DJ a moment, then ask again.', 'error'))
    expect(push).not.toHaveBeenCalledWith(expect.stringMatching(/didn't catch/), 'info')
  })
})

describe('VoiceNudge', () => {
  it('offers a manager the built-in voice and turns it on', async () => {
    signIn(['can_manage_library'])
    get.mockImplementation(async (p: string) => (p === '/adapters' ? [{ id: 'a1', type: 'llm', name: 'openai-chat' }] : null))
    post.mockImplementation(async (p: string) => (p === '/adapters' ? { data: { id: 'v1' } } : null))
    act(() => useDJ.setState({ active: true, label: 'On repeat', voice: false }))
    wrap(<DJControl />)
    fireEvent.click(screen.getByRole('button', { name: 'DJ' }))
    fireEvent.click(screen.getByRole('button', { name: 'Turn on built-in voice' }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/adapters', { type: 'voice', name: 'kokoro', enabled: true, priority: 0, config: {} }),
    )
    await waitFor(() => expect(useDJ.getState().voice).toBe(true))
    expect(screen.queryByTestId('dj-voice-nudge')).toBeNull()
    expect(push).toHaveBeenCalledWith(expect.stringMatching(/Setting up the built-in voice/), 'info')
  })

  it('sends a manager to Admin when a voice exists but is not working', async () => {
    signIn(['can_manage_library'])
    get.mockImplementation(async (p: string) => (p === '/adapters' ? [{ id: 'v0', type: 'voice', name: 'elevenlabs' }] : null))
    act(() => useDJ.setState({ active: true, label: 'On repeat', voice: false }))
    wrap(<DJControl />)
    fireEvent.click(screen.getByRole('button', { name: 'DJ' }))
    fireEvent.click(screen.getByRole('button', { name: 'Turn on built-in voice' }))
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/admin'))
    expect(post).not.toHaveBeenCalledWith('/adapters', expect.anything())
  })

  it('stays hidden for listeners who cannot set up providers, and once dismissed', () => {
    signIn([])
    act(() => useDJ.setState({ active: true, label: 'On repeat', voice: false }))
    const { unmount } = wrap(<DJControl />)
    fireEvent.click(screen.getByRole('button', { name: 'DJ' }))
    expect(screen.queryByTestId('dj-voice-nudge')).toBeNull()
    unmount()
    signIn(['can_manage_library'])
    wrap(<DJControl />)
    fireEvent.click(screen.getByRole('button', { name: 'DJ' }))
    fireEvent.click(screen.getByRole('button', { name: 'Not now' }))
    expect(screen.queryByTestId('dj-voice-nudge')).toBeNull()
    expect(localStorage.getItem('reverb.dj.voiceNudge.dismissed')).toBe('1')
  })
})
