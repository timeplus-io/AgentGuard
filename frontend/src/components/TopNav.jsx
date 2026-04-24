import { useAuth } from '../context/AuthContext'
import { api } from '../api/client'

export default function TopNav({ title }) {
  const { auth, setAuth } = useAuth()

  const handleLogout = async () => {
    await api.authLogout().catch(() => {})
    setAuth(prev => ({ ...prev, loading: false, loggedIn: false, username: '' }))
  }

  const initials = auth.username
    ? auth.username.slice(0, 2).toUpperCase()
    : 'AG'

  return (
    <header className="top-nav fixed top-0 right-0 h-16 bg-gray-800 border-b border-gray-700 z-50 flex justify-between items-center px-8 backdrop-blur-md">
      <div className="flex items-center gap-4 flex-1">
        {title && (
          <h2 className="text-lg font-semibold text-gray-200 tracking-tight">{title}</h2>
        )}
      </div>
      <div className="flex items-center gap-6">
        <div className="flex items-center gap-4 text-gray-400">
          <button className="hover:text-pink-400 transition-colors">
            <span className="material-symbols-outlined">monitor_heart</span>
          </button>
          <button className="hover:text-pink-400 transition-colors relative">
            <span className="material-symbols-outlined">notifications</span>
            <span className="absolute top-0 right-0 w-2 h-2 bg-pink-400 rounded-full"></span>
          </button>
        </div>
        <div className="h-8 w-px bg-[#574149] opacity-20"></div>
        <div className="flex items-center gap-3">
          {auth.loggedIn && (
            <div className="text-right hidden sm:block">
              <p className="text-xs font-medium text-gray-100">{auth.username}</p>
              <button
                onClick={handleLogout}
                className="text-[10px] text-pink-400 uppercase font-semibold tracking-tighter hover:text-gray-100 transition-colors"
              >
                Logout
              </button>
            </div>
          )}
          <div className="w-8 h-8 rounded-sm bg-gray-700 flex items-center justify-center text-pink-400 font-semibold text-xs">{initials}</div>
        </div>
      </div>
    </header>
  )
}
