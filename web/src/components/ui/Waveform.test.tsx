import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Waveform } from './Waveform'
import { resamplePeaks } from '../../lib/waveform'

describe('resamplePeaks', () => {
  it('leaves short inputs alone', () => {
    expect(resamplePeaks([0.1, 0.2], 96)).toEqual([0.1, 0.2])
  })

  it('keeps the loudest sample of each bucket', () => {
    expect(resamplePeaks([0.1, 0.9, 0.2, 0.3, 0.8, 0.1], 3)).toEqual([0.9, 0.3, 0.8])
  })
})

describe('Waveform', () => {
  it('draws a fixed number of spaced bars and marks the played ones', () => {
    const peaks = Array.from({ length: 400 }, (_, i) => (i % 10) / 10)
    render(<Waveform peaks={peaks} pct={50} bars={40} />)
    const wave = screen.getByTestId('waveform')
    expect(wave.children.length).toBe(40)
    expect(wave.className).toContain('gap-[2px]')
    expect(wave.querySelectorAll('[data-played]').length).toBe(21)
  })
})
