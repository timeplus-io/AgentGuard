import { createContext, useContext, useState, useEffect, useCallback } from 'react'
import { api } from '../api/client'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [auth, setAuth] = useState({
    loading: true,
    setupComplete: false,
    loggedIn: false,
    username: '',
    supervisor: null, // {state,...} or {state:'disabled'}
  })

  const refresh = useCallback(() => {
    // allSettled so a transient authStatus failure doesn't force supervisor to
    // "disabled" (which would mask the boot screen during all-in-one startup).
    Promise.allSettled([api.authStatus(), api.supervisorStatus()])
      .then(([authRes, supRes]) => {
        const data = authRes.status === 'fulfilled' ? authRes.value : {}
        const sup = supRes.status === 'fulfilled' ? supRes.value : { state: 'disabled' }
        setAuth({
          loading: false,
          setupComplete: !!data.setup_complete,
          loggedIn: !!data.logged_in,
          username: data.username || '',
          supervisor: sup,
        })
      })
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
