import { useState, useEffect, useRef } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../context/AuthContext'
import { useTheme } from '../context/ThemeContext'
import { api } from '../api/client'
import { useOpenThreats } from '../hooks/useLiveThreatFeed'
import { uiKey } from '../lib/perm'
import { openTabby, isTabbyPage } from '../lib/tabby'
import { LiveThreatFeedList } from './LiveThreatFeed'
import TabbyAvatar from './TabbyAvatar'

function NotificationsDrawer({ open, onClose, threats, loading, error }) {
  const { t } = useTranslation()
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
        aria-label={t('topNav.liveThreatFeed')}
      >
        <div className="h-10 px-4 border-b border-gray-700 bg-gray-800 flex items-center justify-between">
          <div className="flex items-center gap-2 min-w-0">
            <span className="material-symbols-outlined text-primary text-lg">notifications</span>
            <h2 className="text-sm font-semibold text-gray-100 truncate">{t('topNav.liveThreatFeed')}</h2>
            <span className="text-[10px] font-mono text-red-400">{threats.length}</span>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="w-8 h-8 flex items-center justify-center rounded-sm text-gray-400 hover:text-gray-100 hover:bg-gray-700 transition-colors"
            title={t('topNav.closeNotifications')}
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
                emptyMessage={loading ? t('topNav.loadingOpenThreats') : t('topNav.noOpenThreats')}
                onThreatClick={handleThreatClick}
              />
            </div>
          </section>
        </div>
      </aside>
    </>
  )
}

export default function TopNav() {
  const { t } = useTranslation()
  const { auth, can, setAuth } = useAuth()
  const location = useLocation()
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
  const showTabbyLauncher = can(uiKey('sentry')) && !isTabbyPage(location.pathname)

  return (
    <header className="app-chrome top-nav h-10 bg-gray-800 border-b border-gray-700 z-[60] flex justify-between items-center pl-6 pr-4 backdrop-blur-md">
      <div className="flex items-center gap-4 flex-1">
        <div className="flex items-center gap-2">
          <img
            src="/static/agentguard-logo.svg"
            alt="AgentGuard"
            className="h-[18px] w-auto mt-0.5"
          />
          <span className="text-[10px] font-semibold text-gray-400 uppercase" style={{fontFamily: 'Poppins, sans-serif', letterSpacing: '0.1em'}}>
            {t('topNav.byTimeplus')}
          </span>
        </div>
      </div>
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-3 text-gray-400">
          {showTabbyLauncher && (
            <button
              type="button"
              onClick={openTabby}
              className="h-8 px-3 rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] flex items-center gap-2 text-xs font-semibold hover:scale-[0.98] transition-transform focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-pink-500 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-800"
              title={t('topNav.talkToTabby')}
              aria-label={t('topNav.talkToTabby')}
            >
              <TabbyAvatar className="h-5 w-5" ring="ring-[#3d0023]/30" />
              {t('topNav.talkToTabby')}
            </button>
          )}
          <button
            type="button"
            onClick={() => setNotificationsOpen(true)}
            className="h-10 px-3 hover:bg-gray-700 rounded-sm transition-colors flex items-center gap-2"
            title={t('topNav.liveThreatFeed')}
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
            <div className="profile-menu absolute right-0 top-full mt-1 bg-gray-800 border border-gray-600 rounded-sm shadow-lg min-w-[184px] z-[70] overflow-hidden">
              <button
                type="button"
                onClick={() => {
                  toggleTheme()
                  setDropdownOpen(false)
                }}
                className="profile-menu-item w-full h-10 px-4 text-left text-xs text-gray-300 hover:bg-gray-700 hover:text-gray-100 transition-colors flex items-center gap-3"
              >
                <span className="material-symbols-outlined text-lg">
                  {theme === 'dark' ? 'light_mode' : 'dark_mode'}
                </span>
                {theme === 'dark' ? t('topNav.switchToLightMode') : t('topNav.switchToDarkMode')}
              </button>
              <div className="profile-menu-divider h-px bg-gray-600" />
              <button
                type="button"
                onClick={() => {
                  handleLogout()
                  setDropdownOpen(false)
                }}
                className="profile-menu-item w-full h-10 px-4 text-left text-xs text-gray-300 hover:bg-gray-700 hover:text-pink-400 transition-colors flex items-center gap-3"
              >
                <span className="material-symbols-outlined text-lg">logout</span>
                {t('topNav.logout')}
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
