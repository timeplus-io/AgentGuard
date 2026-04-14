import { useState, useEffect } from 'react'
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
  const [collapsed, setCollapsed] = useState(() =>
    localStorage.getItem('sidebar-collapsed') === 'true'
  )

  useEffect(() => {
    document.body.classList.toggle('sidebar-collapsed', collapsed)
    localStorage.setItem('sidebar-collapsed', collapsed)
  }, [collapsed])

  return (
    <aside className={`h-screen fixed left-0 top-0 bg-[#211e29] flex flex-col py-8 gap-2 z-40 transition-all duration-200 ${collapsed ? 'w-16 px-2' : 'w-64 px-4'}`}>
      {/* Logo */}
      <div className={`mb-10 flex items-center ${collapsed ? 'justify-center' : 'px-2 gap-3'}`}>
        <div className="w-8 h-8 rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] flex items-center justify-center shrink-0">
          <span className="material-symbols-outlined text-[#3d0023] text-lg" style={{ fontVariationSettings: "'FILL' 1" }}>security</span>
        </div>
        {!collapsed && (
          <div>
            <h1 className="text-base font-bold text-[#ffb0cf] leading-none">AgentGuard</h1>
            <p className="text-[10px] text-zinc-500 uppercase tracking-widest mt-1">Global Health: Secure</p>
          </div>
        )}
      </div>

      {/* Nav */}
      <nav className="flex-1 flex flex-col gap-1">
        {navItems.map(item => (
          <NavLink
            key={item.to}
            to={item.to}
            title={collapsed ? item.label : undefined}
            className={({ isActive }) => {
              const base = 'flex items-center py-2 text-xs font-medium rounded-sm transition-all'
              const layout = collapsed ? 'justify-center px-2' : 'gap-3 px-3'
              const state = isActive
                ? `text-[#ffb0cf] bg-[#2c2834]${collapsed ? '' : ' translate-x-1'}`
                : 'text-zinc-500 hover:bg-[#2c2834] hover:text-white'
              return `${base} ${layout} ${state}`
            }}
          >
            <span className="material-symbols-outlined text-base shrink-0">{item.icon}</span>
            {!collapsed && <span>{item.label}</span>}
          </NavLink>
        ))}
      </nav>

      {/* Collapse toggle */}
      <button
        onClick={() => setCollapsed(v => !v)}
        title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        className={`flex items-center justify-center w-8 h-8 rounded-sm text-zinc-500 hover:text-white hover:bg-[#2c2834] transition-colors ${collapsed ? 'mx-auto' : 'mx-2'}`}
      >
        <span className="material-symbols-outlined text-base">
          {collapsed ? 'chevron_right' : 'chevron_left'}
        </span>
      </button>
    </aside>
  )
}
