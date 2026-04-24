import { useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { api } from '../api/client'

export default function Login() {
  const { setAuth } = useAuth()
  const [username, setUsername] = useState(() => localStorage.getItem('agentguard_username') || '')
  const [password, setPassword] = useState('')
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e) => {
    e.preventDefault()
    setError(null)
    setLoading(true)
    try {
      const data = await api.authLogin(username, password)
      localStorage.setItem('agentguard_username', data.username)
      setPassword('')
      setAuth(prev => ({ ...prev, loggedIn: true, username: data.username }))
    } catch (err) {
      setError(err.message === 'HTTP 401' ? 'Invalid credentials' : (err.message || 'Login failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0] flex flex-col">
      <nav className="bg-[#15121d] shadow-[0_0_12px_rgba(213,63,140,0.12)] sticky top-0 z-50 h-16 w-full flex items-center px-6 backdrop-blur-md">
        <span className="text-lg font-semibold bg-clip-text text-transparent bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] tracking-tight">AgentGuard</span>
      </nav>

      <main className="flex-1 flex items-center justify-center p-6">
        <div className="w-full max-w-sm space-y-8">
          <div className="text-center space-y-2">
            <span
              className="material-symbols-outlined text-4xl text-[#ffb0cf]"
              style={{ fontVariationSettings: "'FILL' 1" }}
            >security</span>
            <h1 className="text-2xl font-bold text-white">Sign in to AgentGuard</h1>
            <p className="text-zinc-500 text-xs">Use your Timeplus credentials</p>
          </div>

          <form onSubmit={handleSubmit} className="bg-[#2c2834] rounded-sm p-8 space-y-4">
            <div className="space-y-1">
              <label htmlFor="login-username" className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Username</label>
              <input
                id="login-username"
                type="text"
                value={username}
                onChange={e => setUsername(e.target.value)}
                placeholder="default"
                required
                autoComplete="username"
                className="w-full bg-gray-800 border border-gray-600 text-sm py-2.5 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-[#e7dff0] font-mono"
              />
            </div>

            <div className="space-y-1">
              <label htmlFor="login-password" className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Password</label>
              <input
                id="login-password"
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder="Leave blank if not set"
                autoComplete="current-password"
                className="w-full bg-gray-800 border border-gray-600 text-sm py-2.5 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-[#e7dff0] font-mono"
              />
            </div>

            {error && (
              <div className="flex items-center gap-2 text-xs text-[#ffb4ab]">
                <span className="material-symbols-outlined text-xs">error</span>
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={loading || !username}
              className="w-full bg-primary-button py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] transition-transform disabled:opacity-70 disabled:cursor-not-allowed"
            >
              {loading ? 'Signing in…' : 'Sign In'}
            </button>
          </form>
        </div>
      </main>
    </div>
  )
}
