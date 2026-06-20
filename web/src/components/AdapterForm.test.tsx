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
})
