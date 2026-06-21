import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Segmented } from './Segmented'

const options = [
  { value: 'library', label: 'My Library' },
  { value: 'everywhere', label: 'Everywhere' },
]

describe('Segmented', () => {
  it('renders a tablist', () => {
    render(<Segmented options={options} value="library" onChange={vi.fn()} />)
    expect(screen.getByRole('tablist')).toBeTruthy()
  })

  it('renders each option as a tab', () => {
    render(<Segmented options={options} value="library" onChange={vi.fn()} />)
    expect(screen.getByRole('tab', { name: 'My Library' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Everywhere' })).toBeTruthy()
  })

  it('marks the selected tab as aria-selected=true', () => {
    render(<Segmented options={options} value="everywhere" onChange={vi.fn()} />)
    expect(screen.getByRole('tab', { name: 'Everywhere' }).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByRole('tab', { name: 'My Library' }).getAttribute('aria-selected')).toBe('false')
  })

  it('applies accent classes to the selected segment', () => {
    render(<Segmented options={options} value="library" onChange={vi.fn()} />)
    const activeTab = screen.getByRole('tab', { name: 'My Library' })
    expect(activeTab.className).toMatch(/bg-accent/)
    expect(activeTab.className).toMatch(/text-black/)
  })

  it('calls onChange with the new value when a tab is clicked', () => {
    const onChange = vi.fn()
    render(<Segmented options={options} value="library" onChange={onChange} />)
    fireEvent.click(screen.getByRole('tab', { name: 'Everywhere' }))
    expect(onChange).toHaveBeenCalledWith('everywhere')
  })

  it('exposes a visible focus ring class on tabs', () => {
    render(<Segmented options={options} value="library" onChange={vi.fn()} />)
    expect(screen.getByRole('tab', { name: 'My Library' }).className).toMatch(/focus-visible:ring/)
  })
})
