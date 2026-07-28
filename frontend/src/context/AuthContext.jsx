import { createContext, useContext, useState, useEffect, useCallback, useMemo } from 'react'
import { api } from '../api/client'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [auth, setAuth] = useState({
    loading: true,
    setupComplete: false,
    loggedIn: false,
    username: '',
    role: '',
    permissions: [],
    rbacActive: false,
  })

  const refresh = useCallback(() => {
    api.authStatus()
      .then(data => setAuth({
        loading: false,
        setupComplete: data.setup_complete,
        loggedIn: data.logged_in,
        username: data.username || '',
        role: data.role || '',
        permissions: data.permissions || [],
        rbacActive: !!data.rbac_active,
      }))
      .catch(() => setAuth({
        loading: false, setupComplete: false, loggedIn: false,
        username: '', role: '', permissions: [], rbacActive: false,
      }))
  }, [])

  useEffect(() => { refresh() }, [refresh])

  const permSet = useMemo(() => new Set(auth.permissions || []), [auth.permissions])

  // can(perm) — true when the user holds the permission. When RBAC is not
  // active (a deployment that hasn't provisioned it) everything is allowed, so
  // behavior is unchanged until an admin sets up access control (#243).
  const can = useCallback(
    (perm) => !auth.rbacActive || permSet.has(perm),
    [auth.rbacActive, permSet],
  )

  return (
    <AuthContext.Provider value={{ auth, setAuth, refresh, can }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}
