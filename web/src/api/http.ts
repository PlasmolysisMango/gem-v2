import { appUrl } from '../base'
import type { AuthUser } from '../store/auth'
import type { AdminSettings, Card, GameRecord, MoveEvent, Noble, RoomSettings, RoomSummary, Stats, View } from './types'

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

let onUnauthorized: () => void = () => {
  const next = location.pathname + location.search
  location.assign(`/login?next=${encodeURIComponent(next)}`)
}

export function setUnauthorizedHandler(f: () => void) {
  onUnauthorized = f
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(appUrl(path), {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 401) onUnauthorized()
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
  me: () => request<AuthUser>('GET', '/api/me'),
  rooms: () => request<RoomSummary[]>('GET', '/api/rooms'),
  createRoom: (s: RoomSettings) => request<RoomSummary>('POST', '/api/rooms', s),
  game: (id: number) => request<Replay>('GET', `/api/games/${id}`),
  stats: (userId: number) => request<Stats>('GET', `/api/users/${userId}/stats`),
  adminSettings: () => request<AdminSettings>('GET', '/api/admin/settings'),
  setAdminSettings: (s: AdminSettings) => request<AdminSettings>('PUT', '/api/admin/settings', s),
  closeRoom: (id: string) => request<void>('DELETE', `/api/admin/rooms/${encodeURIComponent(id)}`),
}
