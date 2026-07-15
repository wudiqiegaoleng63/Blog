import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, onAuthenticationLost, setAccessToken } from './api'
import type { User, UserProfile } from './types'

interface AuthState {
  user: User | null
  profile: UserProfile | null
  loading: boolean
  login: (email: string, password: string) => Promise<void>
  register: (email: string, username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refreshMe: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [profile, setProfile] = useState<UserProfile | null>(null)
  const [loading, setLoading] = useState(true)

  const refreshMe = useCallback(async () => {
    const result = await api.me()
    setUser(result.user)
    setProfile(result.profile)
  }, [])

  useEffect(() => {
    onAuthenticationLost(() => {
      setUser(null)
      setProfile(null)
    })
    return () => onAuthenticationLost(null)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void api
      .me()
      .then((result) => {
        if (!controller.signal.aborted) {
          setUser(result.user)
          setProfile(result.profile)
        }
      })
      .catch(() => undefined)
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [])

  const login = useCallback(
    async (email: string, password: string) => {
      const pair = await api.login({ email, password })
      setAccessToken(pair.access_token, pair.expires_in)
      await refreshMe()
    },
    [refreshMe],
  )

  const register = useCallback(
    async (email: string, username: string, password: string) => {
      const pair = await api.register({ email, username, password })
      setAccessToken(pair.access_token, pair.expires_in)
      await refreshMe()
    },
    [refreshMe],
  )

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } finally {
      setAccessToken(null)
      setUser(null)
      setProfile(null)
    }
  }, [])

  const value = useMemo(
    () => ({ user, profile, loading, login, register, logout, refreshMe }),
    [user, profile, loading, login, register, logout, refreshMe],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (!context) throw new Error('useAuth must be used inside AuthProvider')
  return context
}
