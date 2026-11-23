import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { login as requestLogin } from '../api/auth'
import { UNAUTHORIZED_EVENT } from '../api/client'
import { AuthContext, type AuthState } from './AuthContext'
import { clearToken, getToken, setToken } from './storage'

export default function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setTokenState] = useState<string | null>(() => getToken())

  useEffect(() => {
    const handleUnauthorized = () => setTokenState(null)
    window.addEventListener(UNAUTHORIZED_EVENT, handleUnauthorized)
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, handleUnauthorized)
  }, [])

  const value = useMemo<AuthState>(
    () => ({
      token,
      async login(username, password) {
        const response = await requestLogin(username, password)
        setToken(response.token)
        setTokenState(response.token)
      },
      logout() {
        clearToken()
        setTokenState(null)
      },
    }),
    [token],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
