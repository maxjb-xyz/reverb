import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AccentSwatches } from './AccentSwatches'

const mockMutate = vi.fn()

vi.mock('../lib/settingsApi', () => ({
  useSettings: vi.fn(() => ({ data: { accentColor: '#F0354B', dynamicBackground: true } })),
  useUpdateSettings: vi.fn(() => ({ mutate: mockMutate })),
  applyAccent: vi.fn(),
}))

import { useSettings, applyAccent } from '../lib/settingsApi'

function wrap(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('AccentSwatches', () => {
  beforeEach(() => {
    vi.mocked(useSettings).mockReturnValue({ data: { accentColor: '#F0354B', dynamicBackground: true } } as ReturnType<typeof useSettings>)
    mockMutate.mockClear()
  })
  afterEach(() => vi.clearAllMocks())

  it('renders all 6 preset swatches', () => {
    wrap(<AccentSwatches />)
    expect(screen.getByRole('button', { name: /red \(default\)/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /indigo/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /green/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /amber/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /cyan/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /pink/i })).toBeInTheDocument()
  })

  it('marks the active preset as pressed', () => {
    wrap(<AccentSwatches />)
    const redBtn = screen.getByRole('button', { name: /red \(default\)/i })
    expect(redBtn).toHaveAttribute('aria-pressed', 'true')
  })

  it('marks non-active presets as not pressed', () => {
    wrap(<AccentSwatches />)
    const indigoBtn = screen.getByRole('button', { name: /indigo/i })
    expect(indigoBtn).toHaveAttribute('aria-pressed', 'false')
  })

  it('selecting a preset calls applyAccent and mutates settings with its hex', async () => {
    wrap(<AccentSwatches />)
    fireEvent.click(screen.getByRole('button', { name: /indigo/i }))
    expect(applyAccent).toHaveBeenCalledWith('#7C6AF7')
    expect(mockMutate).toHaveBeenCalledWith({ accentColor: '#7C6AF7' })
  })

  it('reveals a hex input when the custom swatch is clicked', () => {
    wrap(<AccentSwatches />)
    expect(screen.queryByPlaceholderText('#000000')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /custom accent color/i }))
    expect(screen.getByPlaceholderText('#000000')).toBeInTheDocument()
  })

  it('typing a valid hex in the custom input calls applyAccent and mutates settings', () => {
    wrap(<AccentSwatches />)
    fireEvent.click(screen.getByRole('button', { name: /custom accent color/i }))
    const input = screen.getByPlaceholderText('#000000')
    fireEvent.change(input, { target: { value: '#abcdef' } })
    expect(applyAccent).toHaveBeenCalledWith('#abcdef')
    expect(mockMutate).toHaveBeenCalledWith({ accentColor: '#abcdef' })
  })

  it('does not call applyAccent for incomplete hex', () => {
    wrap(<AccentSwatches />)
    fireEvent.click(screen.getByRole('button', { name: /custom accent color/i }))
    const input = screen.getByPlaceholderText('#000000')
    fireEvent.change(input, { target: { value: '#abc' } })
    expect(applyAccent).not.toHaveBeenCalled()
  })

  it('reflects a non-preset active color from settings', () => {
    vi.mocked(useSettings).mockReturnValue({ data: { accentColor: '#7C6AF7', dynamicBackground: true } } as ReturnType<typeof useSettings>)
    wrap(<AccentSwatches />)
    const indigoBtn = screen.getByRole('button', { name: /indigo/i })
    expect(indigoBtn).toHaveAttribute('aria-pressed', 'true')
    const redBtn = screen.getByRole('button', { name: /red \(default\)/i })
    expect(redBtn).toHaveAttribute('aria-pressed', 'false')
  })

  it('initializes custom hex input to current accent when it is not a preset', () => {
    vi.mocked(useSettings).mockReturnValue({ data: { accentColor: '#123456', dynamicBackground: true } } as ReturnType<typeof useSettings>)
    wrap(<AccentSwatches />)
    fireEvent.click(screen.getByRole('button', { name: /custom accent color/i }))
    const input = screen.getByPlaceholderText('#000000') as HTMLInputElement
    expect(input.value).toBe('#123456')
  })

  it('initializes custom hex to #000000 when current accent is a preset', () => {
    wrap(<AccentSwatches />)
    fireEvent.click(screen.getByRole('button', { name: /custom accent color/i }))
    const input = screen.getByPlaceholderText('#000000') as HTMLInputElement
    expect(input.value).toBe('#000000')
  })
})
