import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AdapterForm } from './AdapterForm'
import type { ConfigSchema } from '../lib/adaptersApi'

vi.mock('../lib/adaptersApi', async (orig) => {
  const actual = await orig<typeof import('../lib/adaptersApi')>()
  return { ...actual, testAdapter: vi.fn() }
})
import { testAdapter } from '../lib/adaptersApi'

const schema: ConfigSchema = {
  fields: [
    { key: 'client_id', label: 'Client ID', type: 'string', required: true, secret: false },
    { key: 'client_secret', label: 'Client Secret', type: 'string', required: true, secret: true },
  ],
}

describe('AdapterForm', () => {
  beforeEach(() => vi.mocked(testAdapter).mockReset())
  afterEach(() => vi.clearAllMocks())

  it('renders one input per schema field', () => {
    render(<AdapterForm name="spotify" schema={schema} onSubmit={vi.fn()} />)
    expect(screen.getByLabelText('Client ID')).toBeInTheDocument()
    expect(screen.getByLabelText('Client Secret')).toBeInTheDocument()
  })

  it('renders secret fields as password inputs', () => {
    render(<AdapterForm name="spotify" schema={schema} onSubmit={vi.fn()} />)
    const secret = screen.getByLabelText('Client Secret') as HTMLInputElement
    expect(secret.type).toBe('password')
  })

  it('shows "set" placeholder for an already-set secret and keeps the value hidden', () => {
    render(
      <AdapterForm name="spotify" schema={schema} initial={{ client_id: 'abc', client_secret__isSet: true }} onSubmit={vi.fn()} />,
    )
    const secret = screen.getByLabelText('Client Secret') as HTMLInputElement
    expect(secret.value).toBe('') // never the real value
    expect(secret.placeholder).toMatch(/leave blank/i)
    const id = screen.getByLabelText('Client ID') as HTMLInputElement
    expect(id.value).toBe('abc')
  })

  it('Test Connection calls testAdapter and shows the result', async () => {
    vi.mocked(testAdapter).mockResolvedValue({ ok: false, error: 'connection refused' })
    render(<AdapterForm name="spotify" schema={schema} onSubmit={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Client ID'), { target: { value: 'x' } })
    fireEvent.change(screen.getByLabelText('Client Secret'), { target: { value: 'shh' } })
    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))
    await waitFor(() => expect(testAdapter).toHaveBeenCalledWith('spotify', { client_id: 'x', client_secret: 'shh' }))
    expect(await screen.findByText(/connection refused/i)).toBeInTheDocument()
  })

  it('submits the collected config', async () => {
    const onSubmit = vi.fn()
    render(<AdapterForm name="spotify" schema={schema} onSubmit={onSubmit} />)
    fireEvent.change(screen.getByLabelText('Client ID'), { target: { value: 'cid' } })
    fireEvent.change(screen.getByLabelText('Client Secret'), { target: { value: 'csec' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ client_id: 'cid', client_secret: 'csec' }))
  })

  it('shows an error message when onSubmit rejects', async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error('Server error 422'))
    render(<AdapterForm name="spotify" schema={schema} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    expect(await screen.findByText(/Server error 422/i)).toBeInTheDocument()
  })

  it('clears the submit error at the start of each submit attempt', async () => {
    let callCount = 0
    const onSubmit = vi.fn().mockImplementation(() => {
      callCount++
      if (callCount === 1) return Promise.reject(new Error('First failure'))
      return Promise.resolve()
    })
    render(<AdapterForm name="spotify" schema={schema} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    expect(await screen.findByText(/First failure/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    await waitFor(() => expect(screen.queryByText(/First failure/i)).not.toBeInTheDocument())
  })

  it('shows the "leave blank" hint only for already-set secrets', () => {
    render(
      <AdapterForm
        name="spotify"
        schema={schema}
        initial={{ client_id: 'abc', client_secret__isSet: true }}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByText(/leave blank to keep the current value/i)).toBeInTheDocument()
    // Non-secret field (client_id) should not show the hint
    const hints = screen.queryAllByText(/leave blank to keep the current value/i)
    expect(hints).toHaveLength(1)
  })

  it('does not show the "leave blank" hint when the secret is not yet set', () => {
    render(<AdapterForm name="spotify" schema={schema} onSubmit={vi.fn()} />)
    expect(screen.queryByText(/leave blank to keep the current value/i)).not.toBeInTheDocument()
  })

  it('renders and submits with an empty schema (keyless adapter)', async () => {
    const onSubmit = vi.fn()
    render(<AdapterForm name="deezer" schema={{ fields: [] }} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({}))
  })
})

describe('AdapterForm — granularity checkboxes', () => {
  const downloaderSchema: ConfigSchema = { fields: [{ key: 'api_key', label: 'API Key', type: 'string', required: true, secret: false }] }
  const supportedGranularities = ['track', 'album']
  const granularities: Record<string, number> = { track: 0, album: 0 }

  it('renders no granularity checkboxes when supportedGranularities is not provided', () => {
    render(<AdapterForm name="mydownloader" schema={downloaderSchema} onSubmit={vi.fn()} />)
    expect(screen.queryByRole('checkbox', { name: /song/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: /album/i })).not.toBeInTheDocument()
  })

  it('renders a checkbox for each supported granularity with correct labels', () => {
    render(
      <AdapterForm
        name="mydownloader"
        schema={downloaderSchema}
        supportedGranularities={supportedGranularities}
        granularities={granularities}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByRole('checkbox', { name: /song/i })).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: /album/i })).toBeInTheDocument()
  })

  it('checks granularity checkboxes that are keys in the granularities prop', () => {
    render(
      <AdapterForm
        name="mydownloader"
        schema={downloaderSchema}
        supportedGranularities={supportedGranularities}
        granularities={granularities}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByRole('checkbox', { name: /song/i })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: /album/i })).toBeChecked()
  })

  it('unchecks a granularity that is not in the granularities prop', () => {
    render(
      <AdapterForm
        name="mydownloader"
        schema={downloaderSchema}
        supportedGranularities={supportedGranularities}
        granularities={{ track: 0 }}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByRole('checkbox', { name: /song/i })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: /album/i })).not.toBeChecked()
  })

  it('submits granularities including only checked granularities alongside schema fields', async () => {
    const onSubmit = vi.fn()
    render(
      <AdapterForm
        name="mydownloader"
        schema={downloaderSchema}
        supportedGranularities={supportedGranularities}
        granularities={granularities}
        priority={5}
        onSubmit={onSubmit}
      />,
    )
    // Uncheck "Album"
    fireEvent.click(screen.getByRole('checkbox', { name: /album/i }))
    // Fill in API key
    fireEvent.change(screen.getByLabelText('API Key'), { target: { value: 'mykey' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({
          api_key: 'mykey',
          granularities: { track: 0 },
        }),
      ),
    )
    // album should not be in submitted granularities
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>
    const submittedGranularities = submitted.granularities as Record<string, number>
    expect(submittedGranularities).not.toHaveProperty('album')
  })

  it('disables a checkbox when it is the only one still checked (last-untick guard)', () => {
    render(
      <AdapterForm
        name="mydownloader"
        schema={downloaderSchema}
        supportedGranularities={supportedGranularities}
        granularities={{ track: 0 }}
        onSubmit={vi.fn()}
      />,
    )
    // Only "track" (Song) is enabled — its checkbox should be disabled
    expect(screen.getByRole('checkbox', { name: /song/i })).toBeDisabled()
    // "album" is unchecked, not disabled
    expect(screen.getByRole('checkbox', { name: /album/i })).not.toBeDisabled()
  })

  it('keeps existing order for a granularity already in granularities, uses priority for new ones', async () => {
    const onSubmit = vi.fn()
    render(
      <AdapterForm
        name="mydownloader"
        schema={downloaderSchema}
        supportedGranularities={supportedGranularities}
        granularities={{ track: 42 }}
        priority={7}
        onSubmit={onSubmit}
      />,
    )
    // Check "Album" (not previously enabled)
    fireEvent.click(screen.getByRole('checkbox', { name: /album/i }))
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    await waitFor(() => expect(onSubmit).toHaveBeenCalled())
    const submitted = onSubmit.mock.calls[0][0] as Record<string, unknown>
    const submittedGranularities = submitted.granularities as Record<string, number>
    // track preserves its order 42
    expect(submittedGranularities.track).toBe(42)
    // album gets the priority as default order
    expect(submittedGranularities.album).toBe(7)
  })
})
