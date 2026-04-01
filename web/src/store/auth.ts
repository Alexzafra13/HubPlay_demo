import { create } from 'zustand'
import type { User } from '@/api/types'

const USER_KEY = 'hubplay_user'

interface AuthState {
  user: User | null
  isAuthenticated: boolean

  setAuth: (user: User) => void
  logout: () => void
  loadFromStorage: () => void
  updateUser: (user: User) => void
}

export const useAuthStore = create<AuthState>()((set) => ({
  user: null,
  isAuthenticated: false,

  setAuth(user: User) {
    localStorage.setItem(USER_KEY, JSON.stringify(user))
    set({ user, isAuthenticated: true })
  },

  logout() {
    localStorage.removeItem(USER_KEY)
    set({ user: null, isAuthenticated: false })
  },

  loadFromStorage() {
    const userJson = localStorage.getItem(USER_KEY)

    if (userJson) {
      try {
        const user = JSON.parse(userJson) as User
        set({ user, isAuthenticated: true })
      } catch {
        // Corrupted storage — clear everything
        localStorage.removeItem(USER_KEY)
      }
    }
  },

  updateUser(user: User) {
    localStorage.setItem(USER_KEY, JSON.stringify(user))
    set({ user })
  },
}))
