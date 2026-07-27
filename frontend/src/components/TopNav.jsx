import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { useTheme } from '../context/ThemeContext'
import { api } from '../api/client'
import { useOpenThreats } from '../hooks/useLiveThreatFeed'
import { LiveThreatFeedList } from './LiveThreatFeed'

function NotificationsDrawer({ open, onClose, threats, loading, error }) {
  const navigate = useNavigate()

  useEffect(() => {
    if (!open) return

    function handleKeyDown(event) {
      if (event.key === 'Escape') onClose()
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose, open])

  const handleThreatClick = (threat) => {
    navigate(threat.threat_id ? `/threats/${threat.threat_id}` : '/threats')
    onClose()
  }

  return (
    <>
      <div
        className={`fixed left-0 right-0 top-[41px] bottom-0 z-[80] bg-black/40 transition-opacity ${open ? 'opacity-100 pointer-events-auto' : 'opacity-0 pointer-events-none'}`}
        onClick={onClose}
        aria-hidden="true"
      />
      <aside
        className={`fixed right-0 top-[41px] z-[90] h-[calc(100vh-41px)] w-full max-w-[420px] bg-gray-900 border-l border-gray-700 shadow-2xl transition-transform duration-200 ${open ? 'translate-x-0' : 'translate-x-full'}`}
        aria-hidden={!open}
        aria-label="Live Threat Feed"
      >
        <div className="h-10 px-4 border-b border-gray-700 bg-gray-800 flex items-center justify-between">
          <div className="flex items-center gap-2 min-w-0">
            <span className="material-symbols-outlined text-primary text-lg">notifications</span>
            <h2 className="text-sm font-semibold text-gray-100 truncate">Live Threat Feed</h2>
            <span className="text-[10px] font-mono text-red-400">{threats.length}</span>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="w-8 h-8 flex items-center justify-center rounded-sm text-gray-400 hover:text-gray-100 hover:bg-gray-700 transition-colors"
            title="Close notifications"
          >
            <span className="material-symbols-outlined text-base">close</span>
          </button>
        </div>

        <div className="h-[calc(100vh-81px)] overflow-y-auto custom-scrollbar">
          <section className="px-4 py-5">
            <div className="space-y-3">
              <LiveThreatFeedList
                threats={threats}
                connected={!loading}
                error={error}
                ruleCategory={{}}
                compact
                emptyMessage={loading ? 'Loading open threats…' : 'No open threats.'}
                onThreatClick={handleThreatClick}
              />
            </div>
          </section>
        </div>
      </aside>
    </>
  )
}

export default function TopNav({ title }) {
  const { auth, setAuth } = useAuth()
  const { theme, toggleTheme } = useTheme()
  const [dropdownOpen, setDropdownOpen] = useState(false)
  const [notificationsOpen, setNotificationsOpen] = useState(false)
  const dropdownRef = useRef(null)
  const {
    threats: openThreats,
    loading: openThreatsLoading,
    error: openThreatsError,
  } = useOpenThreats(auth.loggedIn)

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
  const activeThreatCount = openThreats.length

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
          <h2 className="text-lg font-semibold text-gray-100 tracking-tight">{title}</h2>
        )}
      </div>
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-3 text-gray-400">
          <button
            type="button"
            onClick={() => setNotificationsOpen(true)}
            className="h-10 px-3 hover:bg-gray-700 rounded-sm transition-colors flex items-center gap-2"
            title="Live Threat Feed"
          >
            <span className={`material-symbols-outlined text-lg ${activeThreatCount > 0 ? 'text-red-500' : 'text-gray-400'}`}>
              notifications
            </span>
            {activeThreatCount > 0 && (
              <span className="min-w-5 h-5 px-1.5 rounded-full bg-red-500 text-white text-[10px] font-bold font-mono flex items-center justify-center">
                {activeThreatCount}
              </span>
            )}
          </button>
          <button
            type="button"
            onClick={toggleTheme}
            className="h-10 w-10 hover:bg-gray-700 rounded-sm transition-colors flex items-center justify-center"
            title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
            aria-label={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          >
            <span className="material-symbols-outlined text-lg text-gray-400">
              {theme === 'dark' ? 'light_mode' : 'dark_mode'}
            </span>
          </button>
        </div>
        <div className="h-8 w-px bg-outline-variant opacity-20"></div>
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
      <NotificationsDrawer
        open={notificationsOpen}
        onClose={() => setNotificationsOpen(false)}
        threats={openThreats}
        loading={openThreatsLoading}
        error={openThreatsError}
      />
    </header>
  )
}
