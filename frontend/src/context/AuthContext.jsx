import { createContext, useContext, useState, useEffect, useCallback } from 'react'
import { api } from '../api/client'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [auth, setAuth] = useState({
    loading: true,
    setupComplete: false,
    loggedIn: false,
    username: '',
  })

  const refresh = useCallback(() => {
    api.authStatus()
      .then(data => setAuth({
        loading: false,
        setupComplete: data.setup_complete,
        loggedIn: data.logged_in,
        username: data.username || '',
      }))
      .catch(() => setAuth({ loading: false, setupComplete: false, loggedIn: false, username: '' }))
  }, [])

  useEffect(() => { refresh() }, [refresh])

  return (
    <AuthContext.Provider value={{ auth, setAuth, refresh }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}
