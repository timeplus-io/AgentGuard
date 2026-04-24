import { useState, useEffect } from 'react'
import { NavLink } from 'react-router-dom'
import { useNotifications } from './NotificationsProvider'

const navItems = [
  { label: 'Dashboard', to: '/dashboard', icon: 'dashboard' },
  { label: 'Agents', to: '/agents', icon: 'security' },
  { label: 'Threats', to: '/threats', icon: 'gpp_maybe' },
  { label: 'Cost', to: '/cost', icon: 'payments' },
  { label: 'Rules', to: '/rules', icon: 'rule' },
  { label: 'Investigate', to: '/investigate', icon: 'search' },
]

export default function Sidebar() {
  const [userCollapsed, setUserCollapsed] = useState(() =>
    localStorage.getItem('sidebar-collapsed') === 'true'
  )
  const [windowWidth, setWindowWidth] = useState(typeof window !== 'undefined' ? window.innerWidth : 1200)

  const notifications = useNotifications()
  const pendingCount = notifications?.pendingHolds?.size ?? 0

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
    <aside className={`h-screen fixed left-0 top-0 bg-gray-800 border-r border-gray-700 flex flex-col py-8 gap-2 z-40 transition-all duration-200 ${collapsed ? 'w-16 px-2' : 'w-64 px-4'}`}>
      {/* Logo */}
      <div className={`mb-10 flex items-center ${collapsed ? 'justify-center' : 'px-2'}`}>
        {collapsed ? (
          <svg viewBox="7 36 130 93" className="h-5 w-auto shrink-0" fill="none" xmlns="http://www.w3.org/2000/svg" aria-label="AgentGuard">
            <path d="M43.0913 38.9258C46.7086 38.0481 50.4097 37.6258 54.1091 37.6601C54.3823 37.6627 54.447 38.0397 54.1936 38.142C49.7631 39.9306 45.6116 42.376 41.8833 45.4073C39.4699 47.3695 37.2597 49.5556 35.2805 51.9315C35.2626 51.9531 35.2412 51.9714 35.2172 51.9859C31.1721 54.4264 27.6235 57.6566 24.7984 61.5143C20.2965 67.6616 17.8646 75.0834 17.854 82.7057C17.8664 90.3331 20.3037 97.7588 24.813 103.907C27.632 107.75 31.1696 110.969 35.2006 113.404C35.2246 113.419 35.246 113.437 35.2639 113.458C37.2475 115.843 39.4628 118.036 41.8833 120.004C45.6119 123.036 49.7637 125.481 54.1946 127.27C54.448 127.372 54.3833 127.749 54.1101 127.751C50.4104 127.786 46.709 127.364 43.0913 126.487C36.5872 124.908 30.5167 121.897 25.3218 117.674C20.1269 113.45 15.9389 108.119 13.062 102.07C10.1852 96.0209 8.69193 89.4054 8.69193 82.7057C8.69196 76.0061 10.1852 69.3905 13.062 63.3414C15.9389 57.2925 20.127 51.9616 25.3218 47.7379C30.5166 43.5143 36.5873 40.504 43.0913 38.9258Z" fill="#EA9FC5"/>
            <path d="M90.6092 37.6582C81.7091 37.6582 73.0088 40.3002 65.6086 45.2501C58.2084 50.2001 52.4406 57.2356 49.0346 65.467C45.6287 73.6984 44.7376 82.7561 46.4739 91.4945C48.2102 100.233 52.4961 108.26 58.7894 114.56C65.0828 120.86 73.101 125.15 81.8302 126.888C90.5593 128.627 99.6073 127.734 107.83 124.325C116.053 120.915 123.081 115.141 128.025 107.733C132.97 100.325 135.609 91.6157 135.609 82.7061C135.595 70.7629 130.85 59.3128 122.414 50.8677C113.978 42.4226 102.54 37.6721 90.6092 37.6582ZM126.426 82.7061C126.426 89.7988 124.326 96.7324 120.391 102.63C116.455 108.528 110.86 113.125 104.315 115.84C97.7691 118.554 90.5663 119.265 83.6173 117.881C76.6682 116.498 70.2851 113.082 65.2751 108.067C60.2652 103.052 56.8535 96.6619 55.4714 89.7055C54.0894 82.749 54.7992 75.5385 57.511 68.9859C60.2228 62.4333 64.8148 56.8329 70.7063 52.893C76.5978 48.9531 83.5241 46.8507 90.6092 46.8516C100.105 46.863 109.208 50.6441 115.923 57.3657C122.637 64.0873 126.414 73.2004 126.426 82.7061Z" fill="#D53F8C"/>
            <path d="M61.5445 38.9262C66.0138 37.8418 70.6108 37.4521 75.1749 37.7603C75.4465 37.7787 75.4905 38.1569 75.2333 38.246C71.4617 39.553 67.8516 41.342 64.4976 43.5855C61.5076 45.5855 58.7739 47.9126 56.3383 50.5108C56.3177 50.5328 56.2933 50.5509 56.2663 50.5642C51.1391 53.1025 46.6676 56.8448 43.2544 61.5059C38.7515 67.6553 36.3238 75.0816 36.3238 82.7062C36.3238 90.3308 38.7515 97.757 43.2544 103.906C46.6669 108.567 51.1375 112.308 56.2634 114.846C56.2904 114.86 56.3148 114.878 56.3353 114.9C56.676 115.263 57.0217 115.623 57.3745 115.976C62.423 121.03 68.535 124.845 75.2293 127.165C75.4865 127.254 75.4425 127.633 75.1708 127.651C70.6081 127.959 66.0125 127.57 61.5445 126.486C55.0404 124.908 48.9698 121.898 43.7749 117.674C38.5799 113.45 34.391 108.12 31.5142 102.071C28.6374 96.0214 27.1451 89.4058 27.1451 82.7062C27.1451 76.0065 28.6374 69.3909 31.5142 63.3418C34.391 57.2927 38.58 51.9621 43.7749 47.7383C48.9698 43.5147 55.0404 40.5044 61.5445 38.9262Z" fill="#DF6FA9"/>
            <path d="M83.7889 49.5481C83.9446 49.516 84.1061 49.5612 84.2228 49.669C88.5607 53.6772 92.072 58.5056 94.5484 63.8825C97.2666 69.7844 98.6739 76.2062 98.6743 82.7052C98.6715 89.2017 97.2626 95.6209 94.5445 101.52C92.0297 106.978 88.4486 111.87 84.0193 115.913C84.0161 115.916 84.0116 115.917 84.0074 115.916C80.6055 115.239 77.3477 114.045 74.3394 112.393C74.1774 112.304 74.1671 112.076 74.3181 111.97C78.6377 108.924 82.2398 104.953 84.8531 100.331C87.8956 94.9503 89.4956 88.8719 89.4956 82.6886C89.4956 76.5052 87.8957 70.427 84.8531 65.0458C82.2465 60.4359 78.657 56.4719 74.352 53.4294C74.2011 53.3227 74.2117 53.0947 74.3738 53.006C77.329 51.3878 80.5034 50.2248 83.7889 49.5481Z" fill="#EA9FC5"/>
            <path d="M90.607 48.8535C95.6925 48.8597 100.658 50.0128 105.162 52.1664C105.218 52.1936 105.27 52.2314 105.312 52.2778C108.466 55.7104 111.075 59.6208 113.036 63.8753C115.756 69.7792 117.165 76.2041 117.166 82.7059C117.166 89.2078 115.758 95.6326 113.037 101.536C111.067 105.81 108.443 109.736 105.27 113.179C105.228 113.225 105.176 113.263 105.119 113.29C104.603 113.536 104.08 113.77 103.549 113.99C98.0087 116.288 91.9694 117.063 86.0511 116.258C85.7989 116.224 85.7599 115.878 85.9946 115.78C91.7218 113.381 96.7565 109.523 100.573 104.55C105.381 98.2856 107.988 90.6063 107.988 82.7059C107.988 74.8053 105.381 67.1253 100.573 60.8604C96.7603 55.8932 91.7321 52.0376 86.012 49.6386C85.7773 49.5401 85.8165 49.1944 86.0687 49.1602C87.5671 48.9569 89.0831 48.8534 90.607 48.8535Z" fill="#DF6FA9"/>
          </svg>
        ) : (
          <img
            src="/static/agentguard-logo.svg"
            alt="AgentGuard"
            className="h-8 w-auto"
          />
        )}
      </div>

      {/* Nav */}
      <nav className="flex-1 flex flex-col gap-1">
        {navItems.map(item => {
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
