import { create } from 'zustand'
import { api } from '../api/http'

export interface AuthUser {
  id: number
  username: string
  externalId?: string
  isAdmin?: boolean
  entry?: { uid: string; username: string; displayName: string; role: string; grants: string }
}

interface AuthState {
  user: AuthUser | null
  ready: boolean
  setUser: (user: AuthUser) => void
  hydrate: () => Promise<void>
  logout: () => void
}

export const useAuth = create<AuthState>((set) => ({
  user: null,
  ready: false,
  setUser: (user) => set({ user }),
  hydrate: async () => {
    localStorage.removeItem('gem.auth')
    try {
      const user = await api.me()
      set({ user, ready: true })
    } catch {
      set({ user: null, ready: true })
    }
  },
  logout: () => {
    void fetch('/api/auth/logout', { method: 'POST', credentials: 'same-origin' }).finally(() => {
      location.assign('/login')
    })
  },
}))
