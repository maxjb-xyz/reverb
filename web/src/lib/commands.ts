import type { IconName } from '../components/ui/Icon'
import type { PlayerStore } from './playerStore'

export interface CommandContext {
  navigate(to: string): void
  player: Pick<PlayerStore, 'toggle' | 'next' | 'prev' | 'toggleShuffle' | 'cycleRepeat'>
  ui: { togglePanel(p: 'downloads' | 'nowplaying'): void; openCinema(): void }
}
export interface Command { id: string; title: string; hint?: string; icon: IconName; keywords: string[]; run(ctx: CommandContext): void }

export function baseCommands(isManager: boolean): Command[] {
  const nav = (id: string, title: string, to: string, icon: IconName): Command => ({ id, title, icon, keywords: ['navigate'], run: (ctx) => ctx.navigate(to) })
  const commands: Command[] = [
    nav('nav-home', 'Go to Home', '/', 'home'), nav('nav-search', 'Search', '/search', 'search'), nav('nav-library', 'Library', '/library', 'browse'), nav('nav-collection', 'Collection', '/collection', 'browse'),
    { id: 'panel-downloads', title: 'Downloads', icon: 'dl', keywords: ['download', 'panel'], run: (ctx) => ctx.ui.togglePanel('downloads') },
    nav('nav-requests', 'Requests', '/requests', 'bell'), nav('nav-stats', 'Stats', '/stats', 'chart'), nav('nav-settings', 'Settings', '/settings', 'full'),
    { id: 'player-toggle', title: 'Play / Pause', icon: 'play', hint: 'Space', keywords: ['pause', 'resume'], run: (ctx) => ctx.player.toggle() },
    { id: 'player-next', title: 'Next track', icon: 'next', keywords: ['skip', 'forward'], run: (ctx) => ctx.player.next() },
    { id: 'player-prev', title: 'Previous track', icon: 'prev', keywords: ['back'], run: (ctx) => ctx.player.prev() },
    { id: 'player-shuffle', title: 'Toggle shuffle', icon: 'shuffle', keywords: ['random'], run: (ctx) => ctx.player.toggleShuffle() },
    { id: 'player-repeat', title: 'Cycle repeat', icon: 'repeat', keywords: ['loop'], run: (ctx) => ctx.player.cycleRepeat() },
    { id: 'player-cinema', title: 'Full screen', icon: 'expand', keywords: ['cinema'], run: (ctx) => ctx.ui.openCinema() },
  ]
  if (isManager) commands.splice(7, 0, nav('nav-admin', 'Admin', '/admin', 'browse'))
  return commands
}
export function matchCommands(query: string, commands: Command[]): Command[] {
  const q = query.trim().toLowerCase()
  if (!q) return commands
  const title: Command[] = []; const keyword: Command[] = []
  for (const command of commands) { if (command.title.toLowerCase().includes(q)) title.push(command); else if (command.keywords.some((word) => word.includes(q))) keyword.push(command) }
  return [...title, ...keyword]
}
