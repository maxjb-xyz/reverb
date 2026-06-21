import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Select } from './Select'

const options = [
  { value: 'spotdl', label: 'spotDL' },
  { value: 'ytdlp', label: 'yt-dlp' },
  { value: 'deemix', label: 'Deemix' },
]

describe('Select', () => {
  it('renders a combobox / select element', () => {
    render(<Select value="spotdl" options={options} onChange={vi.fn()} label="Downloader" />)
    expect(screen.getByRole('combobox')).toBeTruthy()
  })

  it('associates the label via aria-label on the select', () => {
    render(<Select value="spotdl" options={options} onChange={vi.fn()} label="Downloader" />)
    expect(screen.getByRole('combobox').getAttribute('aria-label')).toBe('Downloader')
  })

  it('renders all option labels', () => {
    render(<Select value="spotdl" options={options} onChange={vi.fn()} label="Downloader" />)
    expect(screen.getByRole('option', { name: 'spotDL' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'yt-dlp' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'Deemix' })).toBeTruthy()
  })

  it('reflects the current value', () => {
    render(<Select value="ytdlp" options={options} onChange={vi.fn()} label="Downloader" />)
    expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('ytdlp')
  })

  it('calls onChange when selection changes', () => {
    const onChange = vi.fn()
    render(<Select value="spotdl" options={options} onChange={onChange} label="Downloader" />)
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'deemix' } })
    expect(onChange).toHaveBeenCalledWith('deemix')
  })

  it('applies bg-input class', () => {
    render(<Select value="spotdl" options={options} onChange={vi.fn()} label="Downloader" />)
    // bg-input is on the wrapper div
    const { container } = render(<Select value="spotdl" options={options} onChange={vi.fn()} label="Downloader" />)
    expect(container.firstChild).toBeDefined()
    // The wrapper or select has bg-input
    const el = container.querySelector('.bg-input')
    expect(el).toBeTruthy()
  })

  it('exposes a visible focus ring class on the select', () => {
    render(<Select value="spotdl" options={options} onChange={vi.fn()} label="Downloader" />)
    expect(screen.getByRole('combobox').className).toMatch(/focus-visible:ring/)
  })
})
