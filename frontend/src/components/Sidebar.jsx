import { NavLink } from 'react-router-dom'

const navItems = [
  { label: 'Dashboard', to: '/dashboard', icon: 'dashboard' },
  { label: 'Agents', to: '/agents', icon: 'security' },
  { label: 'Threats', to: '/threats', icon: 'gpp_maybe' },
  { label: 'Cost', to: '/cost', icon: 'payments' },
  { label: 'Rules', to: '/rules', icon: 'rule' },
  { label: 'Investigate', to: '/investigate', icon: 'search' },
]

export default function Sidebar() {
  return (
    <aside className="h-screen w-64 fixed left-0 top-0 bg-[#211e29] flex flex-col py-8 px-4 gap-2 z-40">
      <div className="mb-10 px-2 flex items-center gap-3">
        <div className="w-8 h-8 rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] flex items-center justify-center">
          <span className="material-symbols-outlined text-[#3d0023] text-lg" style={{ fontVariationSettings: "'FILL' 1" }}>security</span>
        </div>
        <div>
          <h1 className="text-base font-bold text-[#ffb0cf] leading-none">AgentShield</h1>
          <p className="text-[10px] text-zinc-500 uppercase tracking-widest mt-1">Global Health: Secure</p>
        </div>
      </div>
      <nav className="flex-1 flex flex-col gap-1">
        {navItems.map(item => (
          <NavLink
            key={item.to}
            to={item.to}
            className={({ isActive }) =>
              `flex items-center gap-3 px-3 py-2 text-xs font-medium rounded-sm transition-all ${
                isActive
                  ? 'text-[#ffb0cf] bg-[#2c2834] translate-x-1'
                  : 'text-zinc-500 hover:bg-[#2c2834] hover:text-white'
              }`
            }
          >
            <span className="material-symbols-outlined text-base">{item.icon}</span>
            <span>{item.label}</span>
          </NavLink>
        ))}
      </nav>

    </aside>
  )
}
