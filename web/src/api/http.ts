import { appUrl } from '../base'
import type { AuthUser } from '../store/auth'
import type { Card, GameRecord, MoveEvent, Noble, RoomSettings, RoomSummary, Stats, User, View } from './types'

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

let token: string | null = null
let onUnauthorized: () => void = () => {}

export function setToken(t: string | null) {
  token = t
}

export function setUnauthorizedHandler(f: () => void) {
  onUnauthorized = f
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (token) headers['Authorization'] = `Bearer ${token}`
  const res = await fetch(appUrl(path), { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 401 && token) onUnauthorized()
    throw new ApiError(res.status, data.error || `请求失败 (${res.status})`)
  }
  return data as T
}

export interface AuthResponse {
  token: string
  user: AuthUser
}

export interface Frame {
  seq: number
  seat: number
  text: string
  view: View
  events?: MoveEvent[]
  review?: import('./types').MoveReview
}

export interface Replay extends GameRecord {
  initial: View
  frames: Frame[]
}

let catalog: Promise<{ cards: Card[]; nobles: Noble[] }> | null = null

export const api = {
  cards: () => {
    catalog ??= request<{ cards: Card[]; nobles: Noble[] }>('GET', '/api/cards').catch((e) => {
      catalog = null
      throw e
    })
    return catalog
  },
  register: (username: string, password: string) => request<AuthResponse>('POST', '/api/auth/register', { username, password }),
  login: (username: string, password: string) => request<AuthResponse>('POST', '/api/auth/login', { username, password }),
  me: () => request<User>('GET', '/api/me'),
  rooms: () => request<RoomSummary[]>('GET', '/api/rooms'),
  createRoom: (s: RoomSettings) => request<RoomSummary>('POST', '/api/rooms', s),
  game: (id: number) => request<Replay>('GET', `/api/games/${id}`),
  stats: (userId: number) => request<Stats>('GET', `/api/users/${userId}/stats`),
}
