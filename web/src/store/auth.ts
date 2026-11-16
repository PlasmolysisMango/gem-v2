import { create } from 'zustand'
import { setToken } from '../api/http'

export interface AuthUser {
  id: number
  username: string
}

interface AuthState {
  token: string | null
  user: AuthUser | null
  login: (token: string, user: AuthUser) => void
  logout: () => void
}

const KEY = 'gem.auth'

function load(): { token: string | null; user: AuthUser | null } {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw) {
      const v = JSON.parse(raw)
      setToken(v.token)
      return v
    }
  } catch {
    /* ignore */
  }
  return { token: null, user: null }
}

export const useAuth = create<AuthState>((set) => ({
  ...load(),
  login: (token, user) => {
    localStorage.setItem(KEY, JSON.stringify({ token, user }))
    setToken(token)
    set({ token, user })
  },
  logout: () => {
    localStorage.removeItem(KEY)
    setToken(null)
    set({ token: null, user: null })
  },
}))
