import { useState, useEffect, useRef } from 'react'
import { useAuth } from '../context/AuthContext'
import { api } from '../api/client'

export default function TopNav({ title }) {
  const { auth, setAuth } = useAuth()
  const [dropdownOpen, setDropdownOpen] = useState(false)
  const dropdownRef = useRef(null)

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target)) {
        setDropdownOpen(false)
      }
    }

    if (dropdownOpen) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [dropdownOpen])

  const handleLogout = async () => {
    await api.authLogout().catch(() => {})
    setAuth(prev => ({ ...prev, loading: false, loggedIn: false, username: '' }))
  }

  const initials = auth.username
    ? auth.username.charAt(0).toUpperCase()
    : 'A'

  return (
    <header className="top-nav h-10 bg-gray-800 border-b border-gray-700 z-[60] flex justify-between items-center pl-6 pr-4 backdrop-blur-md">
      <div className="flex items-center gap-4 flex-1">
        <div className="flex items-center gap-2">
          <img
            src="/static/agentguard-logo.svg"
            alt="AgentGuard"
            className="h-[18px] w-auto mt-0.5"
          />
          <span className="text-[10px] font-semibold text-gray-400 uppercase" style={{fontFamily: 'Poppins, sans-serif', letterSpacing: '0.1em'}}>
            BY TIMEPLUS
          </span>
        </div>
        {title && (
          <h2 className="text-lg font-semibold text-gray-200 tracking-tight">{title}</h2>
        )}
      </div>
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-3 text-gray-400">
          <button
            className="w-10 h-10 hover:text-pink-400 hover:bg-gray-700 rounded-sm transition-colors flex items-center justify-center"
            title="System Health"
          >
            <span className="material-symbols-outlined text-lg">monitor_heart</span>
          </button>
          <button
            className="w-10 h-10 hover:text-pink-400 hover:bg-gray-700 rounded-sm transition-colors relative flex items-center justify-center"
            title="Notifications"
          >
            <span className="material-symbols-outlined text-lg">notifications</span>
            <span className="absolute top-1 right-1 w-2 h-2 bg-pink-400 rounded-full"></span>
          </button>
        </div>
        <div className="h-8 w-px bg-[#574149] opacity-20"></div>
        <div className="relative" ref={dropdownRef}>
          {auth.loggedIn && (
            <button
              onClick={() => setDropdownOpen(!dropdownOpen)}
              className="h-10 px-3 flex items-center gap-3 hover:bg-gray-700 rounded-sm transition-colors"
            >
              <div className="text-right hidden sm:block">
                <p className="text-xs font-medium text-gray-100">{auth.username}</p>
              </div>
              <div className="w-6 h-6 rounded-full bg-gradient-to-br from-pink-400 to-pink-500 flex items-center justify-center text-gray-900 font-bold text-xs shadow-sm">
                {initials}
              </div>
            </button>
          )}

          {dropdownOpen && (
            <div className="absolute right-0 top-full mt-1 bg-gray-800 border border-gray-600 rounded-sm shadow-lg min-w-[120px] z-[70]">
              <button
                onClick={() => {
                  handleLogout()
                  setDropdownOpen(false)
                }}
                className="w-full px-4 py-2 text-left text-xs text-gray-300 hover:bg-gray-700 hover:text-pink-400 transition-colors"
              >
                Logout
              </button>
            </div>
          )}
        </div>
      </div>
    </header>
  )
}
