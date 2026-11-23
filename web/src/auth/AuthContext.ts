import { createContext, useContext } from 'react'

export interface AuthState {
  token: string | null
  login: (username: string, password: string) => Promise<void>
  logout: () => void
}

export const AuthContext = createContext<AuthState | null>(null)

export function useAuth(): AuthState {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth 必须在 AuthProvider 内使用')
  }
  return context
}
