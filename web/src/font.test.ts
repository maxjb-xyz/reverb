import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
const main = readFileSync(resolve(__dirname, './main.tsx'), 'utf8')
describe('typography', () => {
  it('imports the self-hosted Figtree variable font', () => {
    expect(main).toMatch(/@fontsource-variable\/figtree/)
  })
})
