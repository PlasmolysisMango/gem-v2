import { create } from 'zustand'
import { Socket } from '../api/ws'
import type {
  Action,
  Analysis,
  ChatMessage,
  Envelope,
  GameEvent,
  GameOver,
  MoveReview,
  RoomState,
  SeatKind,
  View,
} from '../api/types'

interface GameState {
  socket: Socket | null
  connected: boolean
  roomId: string | null
  room: RoomState | null
  view: View | null
  gameId: number
  log: GameEvent[]
  hint: Analysis | null
  hintTurn: number
  hintLoading: boolean
  reviews: MoveReview[]
  lastReview: MoveReview | null
  gameOver: GameOver | null
  chat: ChatMessage[]
  error: string | null
  connect: (token: string) => void
  disconnect: () => void
  joinRoom: (id: string) => void
  leaveRoom: () => void
  setSeat: (seat: number, kind: SeatKind, level?: string) => void
  start: () => void
  act: (a: Action) => void
  requestHint: () => void
  sendChat: (text: string) => void
  clearError: () => void
}

const fresh = {
  room: null,
  view: null,
  gameId: 0,
  log: [] as GameEvent[],
  hint: null,
  hintTurn: -1,
  hintLoading: false,
  reviews: [] as MoveReview[],
  lastReview: null,
  gameOver: null,
  chat: [] as ChatMessage[],
  error: null,
}

export const useGame = create<GameState>((set, get) => {
  const handle = (e: Envelope<any>) => {
    const { roomId } = get()
    if (e.roomId && roomId && e.roomId !== roomId && e.type !== 'error') return
    switch (e.type) {
      case 'room_state':
        set({ room: e.payload })
        break
      case 'game_view': {
        const prev = get().view
        const view: View = e.payload.view
        const turnChanged = !prev || prev.turn !== view.turn || prev.phase !== view.phase || prev.current !== view.current
        set({
          view,
          gameId: e.payload.gameId,
          ...(turnChanged ? { hint: null, hintLoading: false } : {}),
          ...(prev && view.turn < prev.turn ? { log: [], reviews: [], gameOver: null } : {}),
        })
        break
      }
      case 'events': {
        const events = e.payload.events as GameEvent[]
        // replace: recent history resent on (re)joining, superseding the local log
        set((s) => ({ log: (e.payload.replace ? events : [...s.log, ...events]).slice(-300) }))
        break
      }
      case 'hint':
        set({ hint: e.payload.analysis, hintTurn: e.payload.turn, hintLoading: false })
        break
      case 'move_review':
        set((s) => ({ reviews: [...s.reviews, e.payload.review], lastReview: e.payload.review }))
        break
      case 'game_over':
        set({ gameOver: e.payload })
        break
      case 'chat':
        set((s) => ({ chat: [...s.chat, e.payload].slice(-100) }))
        break
      case 'error':
        set({ error: e.payload?.message ?? '未知错误', hintLoading: false })
        break
    }
  }

  return {
    socket: null,
    connected: false,
    roomId: null,
    ...fresh,
    connect: (token) => {
      if (get().socket) return
      const s = new Socket(token)
      s.onMessage = handle
      s.onStatus = (connected) => set({ connected })
      s.onOpen = () => {
        const id = get().roomId
        if (id) s.send('join_room', id)
      }
      s.connect()
      set({ socket: s })
    },
    disconnect: () => {
      get().socket?.close()
      set({ socket: null, connected: false, roomId: null, ...fresh })
    },
    joinRoom: (id) => {
      if (get().roomId === id) return
      set({ roomId: id, ...fresh })
      get().socket?.send('join_room', id)
    },
    leaveRoom: () => {
      const { roomId, socket } = get()
      if (roomId) socket?.send('leave_room', roomId)
      set({ roomId: null, ...fresh })
    },
    setSeat: (seat, kind, level) => get().socket?.send('set_seat', get().roomId!, { seat, kind, level }),
    start: () => get().socket?.send('start', get().roomId!),
    act: (action) => get().socket?.send('action', get().roomId!, { action }),
    requestHint: () => {
      set({ hintLoading: true })
      get().socket?.send('request_hint', get().roomId!)
    },
    sendChat: (text) => get().socket?.send('chat', get().roomId!, { text }),
    clearError: () => set({ error: null }),
  }
})
