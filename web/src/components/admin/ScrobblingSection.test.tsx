import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { ScrobblingSection } from './ScrobblingSection'

// ── Mock scrobbleApi ──────────────────────────────────────────────────────────

const mockGetLastfmConfig = vi.fn()
const mockSetLastfmConfig = vi.fn()

vi.mock('../../lib/scrobbleApi', () => ({
  getLastfmConfig: (...args: unknown[]) => mockGetLastfmConfig(...args),
  setLastfmConfig: (...args: unknown[]) => mockSetLastfmConfig(...args),
}))

// ── Helpers ───────────────────────────────────────────────────────────────────

function wrap(ui: React.ReactNode) {
  return render(<>{ui}</>)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('ScrobblingSection', () => {
  beforeEach(() => {
    mockGetLastfmConfig.mockResolvedValue({ apiKey: '', apiSecretSet: false })
    mockSetLastfmConfig.mockResolvedValue(undefined)
  })

  afterEach(() => vi.clearAllMocks())

  it('renders a Last.fm heading', async () => {
    wrap(<ScrobblingSection />)
    expect(await screen.findByRole('heading', { name: /last\.fm/i })).toBeInTheDocument()
  })

  it('renders the API Key and API Secret fields', async () => {
    wrap(<ScrobblingSection />)
    expect(await screen.findByLabelText(/api key/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/api secret/i)).toBeInTheDocument()
  })

  it('pre-fills the API key from getLastfmConfig', async () => {
    mockGetLastfmConfig.mockResolvedValue({ apiKey: 'loaded-key', apiSecretSet: false })
    wrap(<ScrobblingSection />)
    const keyInput = await screen.findByLabelText(/api key/i)
    expect(keyInput).toHaveValue('loaded-key')
  })

  it('shows blank-means-keep hint when apiSecretSet is true and field is empty', async () => {
    mockGetLastfmConfig.mockResolvedValue({ apiKey: 'k', apiSecretSet: true })
    wrap(<ScrobblingSection />)
    // Wait for the load to complete
    await screen.findByLabelText(/api key/i)
    expect(screen.getByText(/leave blank to keep/i)).toBeInTheDocument()
  })

  it('hides blank-means-keep hint when user types in the secret field', async () => {
    mockGetLastfmConfig.mockResolvedValue({ apiKey: 'k', apiSecretSet: true })
    wrap(<ScrobblingSection />)
    await screen.findByLabelText(/api key/i)
    // Hint shows initially
    expect(screen.getByText(/leave blank to keep/i)).toBeInTheDocument()
    // Type in the secret field — hint should disappear
    fireEvent.change(screen.getByLabelText(/api secret/i), { target: { value: 'newsecret' } })
    expect(screen.queryByText(/leave blank to keep/i)).not.toBeInTheDocument()
  })

  it('clicking Save calls setLastfmConfig with the entered values', async () => {
    mockGetLastfmConfig.mockResolvedValue({ apiKey: '', apiSecretSet: false })
    wrap(<ScrobblingSection />)
    await screen.findByLabelText(/api key/i)

    fireEvent.change(screen.getByLabelText(/api key/i), { target: { value: 'my-key' } })
    fireEvent.change(screen.getByLabelText(/api secret/i), { target: { value: 'my-secret' } })
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(mockSetLastfmConfig).toHaveBeenCalledWith({ apiKey: 'my-key', apiSecret: 'my-secret' })
    })
  })

  it('shows "Configuration saved." after a successful save', async () => {
    wrap(<ScrobblingSection />)
    await screen.findByLabelText(/api key/i)

    fireEvent.change(screen.getByLabelText(/api key/i), { target: { value: 'k' } })
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(screen.getByText(/configuration saved/i)).toBeInTheDocument()
    })
  })

  it('clears the secret field after a successful save', async () => {
    wrap(<ScrobblingSection />)
    await screen.findByLabelText(/api key/i)

    fireEvent.change(screen.getByLabelText(/api secret/i), { target: { value: 'newsecret' } })
    fireEvent.click(screen.getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect((screen.getByLabelText(/api secret/i) as HTMLInputElement).value).toBe('')
    })
  })

  it('shows an error message when getLastfmConfig fails', async () => {
    mockGetLastfmConfig.mockRejectedValue(new Error('network'))
    wrap(<ScrobblingSection />)
    expect(await screen.findByText(/could not load configuration/i)).toBeInTheDocument()
  })
})
