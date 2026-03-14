import { create } from 'zustand'
import type { User } from '@/api/types'

const STORAGE_KEYS = {
  accessToken: 'hubplay_access_token',
  refreshToken: 'hubplay_refresh_token',
  user: 'hubplay_user',
} as const

interface AuthState {
  user: User | null
  accessToken: string | null
  refreshToken: string | null

  readonly isAuthenticated: boolean

  setAuth: (user: User, accessToken: string, refreshToken: string) => void
  logout: () => void
  loadFromStorage: () => void
  updateUser: (user: User) => void
}

export const useAuthStore = create<AuthState>()((set, get) => ({
  user: null,
  accessToken: null,
  refreshToken: null,

  get isAuthenticated(): boolean {
    return get().user !== null && get().accessToken !== null
  },

  setAuth(user: User, accessToken: string, refreshToken: string) {
    localStorage.setItem(STORAGE_KEYS.accessToken, accessToken)
    localStorage.setItem(STORAGE_KEYS.refreshToken, refreshToken)
    localStorage.setItem(STORAGE_KEYS.user, JSON.stringify(user))

    set({ user, accessToken, refreshToken })
  },

  logout() {
    localStorage.removeItem(STORAGE_KEYS.accessToken)
    localStorage.removeItem(STORAGE_KEYS.refreshToken)
    localStorage.removeItem(STORAGE_KEYS.user)

    set({ user: null, accessToken: null, refreshToken: null })
  },

  loadFromStorage() {
    const accessToken = localStorage.getItem(STORAGE_KEYS.accessToken)
    const refreshToken = localStorage.getItem(STORAGE_KEYS.refreshToken)
    const userJson = localStorage.getItem(STORAGE_KEYS.user)

    if (accessToken && refreshToken && userJson) {
      try {
        const user = JSON.parse(userJson) as User
        set({ user, accessToken, refreshToken })
      } catch {
        // Corrupted storage — clear everything
        localStorage.removeItem(STORAGE_KEYS.accessToken)
        localStorage.removeItem(STORAGE_KEYS.refreshToken)
        localStorage.removeItem(STORAGE_KEYS.user)
      }
    }
  },

  updateUser(user: User) {
    localStorage.setItem(STORAGE_KEYS.user, JSON.stringify(user))
    set({ user })
  },
}))
