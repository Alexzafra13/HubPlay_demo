import { create } from 'zustand'
import { api } from '@/api/client'
import type { User } from '@/api/types'

const USER_KEY = 'hubplay_user'

interface AuthState {
  user: User | null
  isAuthenticated: boolean
  // bootstrapped flips once the app has either confirmed the
  // persisted session is still alive (refresh succeeded) or decided
  // it is not (no user in storage, or refresh 401'd). Routes wait
  // for this so the first wave of authed queries doesn't all 401
  // against an expired access cookie and pollute the dev console
  // — refresh dedup masks the symptom but the noise is real.
  bootstrapped: boolean

  setAuth: (user: User) => void
  logout: () => void
  bootstrap: () => Promise<void>
  updateUser: (user: User) => void
}

export const useAuthStore = create<AuthState>()((set, get) => ({
  user: null,
  isAuthenticated: false,
  bootstrapped: false,

  setAuth(user: User) {
    localStorage.setItem(USER_KEY, JSON.stringify(user))
    set({ user, isAuthenticated: true, bootstrapped: true })
  },

  logout() {
    localStorage.removeItem(USER_KEY)
    set({ user: null, isAuthenticated: false, bootstrapped: true })
  },

  async bootstrap() {
    if (get().bootstrapped) return

    const userJson = localStorage.getItem(USER_KEY)
    if (!userJson) {
      set({ bootstrapped: true })
      return
    }

    let user: User
    try {
      user = JSON.parse(userJson) as User
    } catch {
      // Corrupted storage — treat as logged-out.
      localStorage.removeItem(USER_KEY)
      set({ bootstrapped: true })
      return
    }

    // Mark authenticated optimistically so admin-only routes don't
    // bounce while we wait for the refresh round-trip; on failure
    // the catch below clears it.
    set({ user, isAuthenticated: true })

    try {
      await api.refresh()
      set({ bootstrapped: true })
    } catch {
      // Refresh failed (expired or revoked). Drop to logged-out and
      // let ProtectedRoute redirect to /login on next render.
      localStorage.removeItem(USER_KEY)
      set({ user: null, isAuthenticated: false, bootstrapped: true })
    }
  },

  updateUser(user: User) {
    localStorage.setItem(USER_KEY, JSON.stringify(user))
    set({ user })
  },
}))
