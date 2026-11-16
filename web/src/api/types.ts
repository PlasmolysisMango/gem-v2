// Protocol types mirroring internal/game/protocol.go and internal/engine (JSON shapes).

export type Color = 'white' | 'blue' | 'green' | 'red' | 'black' | 'gold'
export const GEM_COLORS: Color[] = ['white', 'blue', 'green', 'red', 'black']
export const ALL_COLORS: Color[] = [...GEM_COLORS, 'gold']

/** Token counts indexed white, blue, green, red, black, gold. */
export type Gems = number[]
/** Cost / bonuses indexed white, blue, green, red, black. */
export type Cost = number[]

export type ActionType =
  | 'take_diff'
  | 'take_same'
  | 'reserve'
  | 'reserve_deck'
  | 'buy'
  | 'buy_reserved'
  | 'discard'
  | 'noble'
  | 'pass'

export interface Action {
  type: ActionType
  gems: Gems
  card: number
  level: number
  noble: number
  hasPay?: boolean
}

export interface Card {
  id: number
  level: number
  bonus: Color
  points: number
  cost: Cost
  hidden?: boolean
}

export interface Noble {
  id: number
  points: number
  req: Cost
}

export interface PlayerView {
  seat: number
  tokens: Gems
  bonuses: Cost
  points: number
  reserved: Card[]
  cards: number[]
  nCards: number
  nobles: number[]
}

export type Phase = 'main' | 'discard' | 'noble' | 'over'

export interface View {
  viewer: number
  nPlayers: number
  current: number
  phase: Phase
  turn: number
  round: number
  bank: Gems
  board: (Card | null)[][]
  deckLen: number[]
  nobles: Noble[]
  players: PlayerView[]
  lastRound: boolean
  winners: number[]
  legal?: Action[]
}

export interface Envelope<T = unknown> {
  type: string
  roomId?: string
  payload?: T
  reqId?: string
}

export type SeatKind = 'open' | 'closed' | 'human' | 'ai'
export type AILevel = 'easy' | 'medium' | 'hard'

export interface SeatInfo {
  index: number
  kind: SeatKind
  userId?: number
  name: string
  level?: AILevel
  connected: boolean
  bot?: boolean
}

export interface RoomState {
  id: string
  name: string
  ownerId: number
  ownerName: string
  status: 'waiting' | 'playing' | 'finished' | 'closed'
  seats: SeatInfo[]
  hints: boolean
  turnSeconds: number
  gameId?: number
  yourSeat: number
  spectators: number
  deadline?: number
}

export interface RoomSummary {
  id: string
  name: string
  ownerName: string
  status: string
  humans: number
  ais: number
  open: number
  seats: number
  hints: boolean
}

export interface RoomSettings {
  name: string
  seats: number
  ai: AILevel[]
  hints?: boolean
  turnSeconds: number
}

export interface GameEvent {
  kind: 'action' | 'noble' | 'game_over'
  seat: number
  action: Action
  card: number
  noble: number
  paid: Gems
  gold: boolean
  seq: number
  text: string
}

export interface Reason {
  tag: string
  text: string
}

export interface PVStep {
  seat: number
  action: Action
  text: string
}

export interface Candidate {
  action: Action
  text: string
  winRate: number
  visits: number
  reasons: Reason[]
  pv: PVStep[]
}

export interface Analysis {
  seat: number
  level: AILevel
  strategy: string
  strategyText: string
  winRate: number
  candidates: Candidate[]
  iterations: number
  elapsedMs: number
  endgame: boolean
}

export interface MoveReview {
  seat: number
  played: Action
  playedText: string
  best: Action
  bestText: string
  playedWinRate: number
  bestWinRate: number
  delta: number
  verdict: 'best' | 'good' | 'mistake' | 'blunder'
  verdictText: string
  comment: string
}

export interface CurvePoint {
  turn: number
  winRate: number
  best: number
}

export interface GameOver {
  gameId: number
  winners: number[]
  points: number[]
  cards: number[]
  reviews: MoveReview[]
  curve: CurvePoint[]
}

export interface ChatMessage {
  from?: string
  seat: number
  text: string
  ts?: number
}

export interface User {
  id: number
  username: string
  createdAt: string
}

export interface GamePlayer {
  seat: number
  userId: number | null
  username?: string
  aiLevel?: AILevel
  finalPoints: number | null
  winner: boolean
}

export interface GameRecord {
  id: number
  roomId: string
  seed: string
  playerCount: number
  status: string
  startedAt: number
  endedAt: number | null
  winnerSeat: number | null
  players: GamePlayer[]
  actions?: { seq: number; seat: number; action: string; ts: number; review?: string }[]
}

export interface Stats {
  user: User
  games: number
  wins: number
  winRate: number
  avgPoints: number
  recent: GameRecord[]
}
