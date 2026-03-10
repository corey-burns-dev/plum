import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import {
  getMe,
  getSetupStatus,
  login as apiLogin,
  logout as apiLogout,
  type User,
} from '../api'

type AuthState = {
  user: User | null
  hasAdmin: boolean
  loading: boolean
  error: string | null
}

type AuthActions = {
  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refreshMe: () => Promise<void>
  refreshSetupStatus: () => Promise<void>
  clearError: () => void
}

const AuthStateContext = createContext<AuthState | null>(null)
const AuthActionsContext = createContext<AuthActions | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [hasAdmin, setHasAdmin] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refreshMe = useCallback(async () => {
    const u = await getMe()
    setUser(u)
  }, [])

  const refreshSetupStatus = useCallback(async () => {
    const status = await getSetupStatus()
    setHasAdmin(status.hasAdmin)
    if (status.hasAdmin) await refreshMe()
  }, [refreshMe])

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const status = await getSetupStatus()
        if (cancelled) return
        setHasAdmin(status.hasAdmin)
        if (status.hasAdmin) {
          const u = await getMe()
          if (cancelled) return
          setUser(u)
        }
      } catch (e) {
        if (!cancelled) setError(String(e))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const login = useCallback(
    async (email: string, password: string) => {
      setError(null)
      const u = await apiLogin({ email, password })
      setUser(u)
    },
    [],
  )

  const logout = useCallback(async () => {
    await apiLogout()
    setUser(null)
    setError(null)
  }, [])

  const clearError = useCallback(() => setError(null), [])

  const state = useMemo(
    () => ({ user, hasAdmin, loading, error }),
    [user, hasAdmin, loading, error],
  )
  const actions = useMemo(
    () => ({ login, logout, refreshMe, refreshSetupStatus, clearError }),
    [login, logout, refreshMe, refreshSetupStatus, clearError],
  )

  return (
    <AuthStateContext.Provider value={state}>
      <AuthActionsContext.Provider value={actions}>{children}</AuthActionsContext.Provider>
    </AuthStateContext.Provider>
  )
}

export function useAuthState(): AuthState {
  const ctx = useContext(AuthStateContext)
  if (!ctx) throw new Error('useAuthState must be used within AuthProvider')
  return ctx
}

export function useAuthActions(): AuthActions {
  const ctx = useContext(AuthActionsContext)
  if (!ctx) throw new Error('useAuthActions must be used within AuthProvider')
  return ctx
}
