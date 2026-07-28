import { useState, useEffect } from 'react'
import { NavLink } from 'react-router-dom'
import { useNotifications } from './NotificationsProvider'
import { useAuth } from '../context/AuthContext'
import { uiKey } from '../lib/perm'

const navItems = [
  { label: 'Dashboard', to: '/dashboard', icon: 'dashboard', resource: 'dashboard' },
  { label: 'Agents', to: '/agents', icon: 'security', resource: 'agents' },
  { label: 'Threats', to: '/threats', icon: 'gpp_maybe', resource: 'threats' },
  { label: 'Cost', to: '/cost', icon: 'payments', resource: 'cost' },
  { label: 'Rules', to: '/rules', icon: 'rule', resource: 'rules' },
  { label: 'Semantic DLP', to: '/dlp', icon: 'policy', resource: 'dlp' },
  { label: 'Investigate', to: '/investigate', icon: 'search', resource: 'investigate' },
  { label: 'Sentry', to: '/sentry', icon: 'shield_person', resource: 'sentry' },
  { label: 'Settings', to: '/settings', icon: 'settings', resource: 'settings' },
  { label: 'Access Control', to: '/access', icon: 'admin_panel_settings', resource: 'access' },
]

export default function Sidebar() {
  const [userCollapsed, setUserCollapsed] = useState(() =>
    localStorage.getItem('sidebar-collapsed') === 'true'
  )
  const [windowWidth, setWindowWidth] = useState(typeof window !== 'undefined' ? window.innerWidth : 1200)

  const notifications = useNotifications()
  const pendingCount = notifications?.pendingHolds?.size ?? 0
  const { can, auth } = useAuth()
  // Access Control is only meaningful once RBAC is provisioned; hide it
  // otherwise so an unprovisioned deployment can't self-serve-bootstrap it.
  const visibleNav = navItems.filter(item =>
    can(uiKey(item.resource)) && (item.resource !== 'access' || auth.rbacActive),
  )

  // Auto-collapse on screens smaller than 1280px (xl breakpoint), otherwise respect user preference
  const collapsed = windowWidth < 1280 ? true : userCollapsed

  useEffect(() => {
    const handleResize = () => setWindowWidth(window.innerWidth)
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [])

  useEffect(() => {
    document.body.classList.toggle('sidebar-collapsed', collapsed)
    localStorage.setItem('sidebar-collapsed', userCollapsed)
  }, [collapsed, userCollapsed])

  return (
    <aside className={`h-[calc(100vh-2.5rem)] fixed left-0 top-10 bg-gray-800 border-r border-gray-700 flex flex-col py-8 gap-2 z-40 transition-all duration-200 ${collapsed ? 'w-16 px-2' : 'w-64 px-4'}`}>

      {/* Nav */}
      <nav className="flex-1 flex flex-col gap-1">
        {visibleNav.map(item => {
          const showBadge = item.to === '/threats' && pendingCount > 0
          return (
            <NavLink
              key={item.to}
              to={item.to}
              title={collapsed ? `${item.label}${showBadge ? ` (${pendingCount} pending)` : ''}` : undefined}
              className={({ isActive }) => {
                const base = 'relative flex items-center py-2 text-xs font-medium rounded-sm transition-all'
                const layout = collapsed ? 'justify-center px-2' : 'gap-3 px-3'
                const state = isActive
                  ? `text-pink-400 bg-gray-700${collapsed ? '' : ' translate-x-1'}`
                  : 'text-gray-400 hover:bg-gray-700 hover:text-gray-100'
                return `${base} ${layout} ${state}`
              }}
            >
              <span className="material-symbols-outlined text-base shrink-0 relative">
                {item.icon}
                {showBadge && collapsed && (
                  <span className="absolute -top-1 -right-2 bg-[#ffb4ab] text-[#3d0023] text-[8px] font-bold rounded-full px-1 min-w-[14px] text-center leading-[14px] h-[14px]">
                    {pendingCount}
                  </span>
                )}
              </span>
              {!collapsed && <span>{item.label}</span>}
              {showBadge && !collapsed && (
                <span className="ml-auto bg-[#ffb4ab] text-[#3d0023] text-[9px] font-bold rounded-full px-1.5 py-0.5 min-w-[16px] text-center">
                  {pendingCount}
                </span>
              )}
            </NavLink>
          )
        })}
      </nav>

      {/* Collapse toggle */}
      <button
        onClick={() => setUserCollapsed(v => !v)}
        title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        className={`flex items-center justify-center w-8 h-8 rounded-sm text-gray-400 hover:text-gray-100 hover:bg-gray-700 transition-colors ${collapsed ? 'mx-auto' : 'mx-2'}`}
      >
        <span className="material-symbols-outlined text-base">
          {collapsed ? 'chevron_right' : 'chevron_left'}
        </span>
      </button>
    </aside>
  )
}
