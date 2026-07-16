import { describe, expect, it } from 'vitest'
import { baseCommands, matchCommands } from './commands'
describe('commands', () => {
  it('hides Admin for non-managers', () => { expect(baseCommands(false).map((c) => c.id)).not.toContain('nav-admin'); expect(baseCommands(true).map((c) => c.id)).toContain('nav-admin') })
  it('matches title before keywords case-insensitively', () => { const cmds = baseCommands(true); expect(matchCommands('down', cmds)[0].id).toBe('panel-downloads'); expect(matchCommands('', cmds)).toHaveLength(cmds.length) })
  it('matches player verbs via keywords', () => expect(matchCommands('skip', baseCommands(false)).map((c) => c.id)).toContain('player-next'))
})
