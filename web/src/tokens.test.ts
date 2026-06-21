import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const css = readFileSync(resolve(__dirname, './index.css'), 'utf8')

describe('design tokens', () => {
  it('keeps the configurable accent defaulting to red #F0354B channels', () => {
    expect(css).toMatch(/--color-accent:\s*240 53 75/)
  })
  it('defines the core surface, text and status tokens', () => {
    for (const t of ['--bg-base', '--bg-surface', '--bg-raised', '--bg-input',
      '--text-primary', '--text-secondary', '--text-muted',
      '--status-success', '--status-warning', '--status-error']) {
      expect(css, t).toContain(t)
    }
  })
  it('disables motion under prefers-reduced-motion', () => {
    expect(css).toMatch(/prefers-reduced-motion:\s*reduce/)
  })
})
