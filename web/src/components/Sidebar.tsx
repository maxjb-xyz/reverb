import { NavLink } from 'react-router-dom'

const items = [
  { to: '/search', label: 'Search' },
  { to: '/library', label: 'Library' },
  { to: '/settings', label: 'Settings' },
]

export function Sidebar() {
  return (
    <nav className="w-56 shrink-0 border-r border-neutral-800 p-4 space-y-1">
      <div className="text-xl font-bold mb-4 text-accent">Crate</div>
      {items.map((i) => (
        <NavLink
          key={i.to}
          to={i.to}
          className={({ isActive }) =>
            `block rounded px-3 py-2 ${isActive ? 'bg-accent/20 text-accent' : 'hover:bg-neutral-800'}`
          }
        >
          {i.label}
        </NavLink>
      ))}
    </nav>
  )
}
