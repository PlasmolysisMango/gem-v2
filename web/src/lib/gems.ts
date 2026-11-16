// Pure game-UI logic shared by components (unit tested with vitest).
import type { Action, Card, Color, GameEvent, Gems, MoveEvent, PlayerView, RoomState } from '../api/types'
import { ALL_COLORS, GEM_COLORS } from '../api/types'

export const COLOR_NAME: Record<Color, string> = {
  white: '白',
  blue: '蓝',
  green: '绿',
  red: '红',
  black: '黑',
  gold: '金',
}

export const LANDSCAPE_QUERY = '(min-width: 1024px) and (orientation: landscape)'

export const COLOR_INDEX: Record<Color, number> = { white: 0, blue: 1, green: 2, red: 3, black: 4, gold: 5 }

export function seatLabel(room: RoomState | null, seat: number): string {
  return room?.seats[seat]?.name || `玩家${seat + 1}`
}

export const emptyGems = (): Gems => [0, 0, 0, 0, 0, 0]

export const total = (g: Gems) => g.reduce((a, b) => a + b, 0)

export function sameGems(a: Gems, b: Gems): boolean {
  for (let i = 0; i < 6; i++) if ((a[i] ?? 0) !== (b[i] ?? 0)) return false
  return true
}

export function gemsText(g: Gems): string {
  const parts: string[] = []
  ALL_COLORS.forEach((c, i) => {
    if (g[i]) parts.push(`${COLOR_NAME[c]}${g[i]}`)
  })
  return parts.join(' ') || '无'
}

/** Whether adding one token of color i to the current selection can still lead to a legal take. */
export function canAddToken(sel: Gems, bank: Gems, i: number): boolean {
  if (i === 5) return false // gold is only obtained by reserving
  const n = total(sel)
  const distinct = sel.filter((x) => x > 0).length
  if (sel[i] === 1) {
    // second of the same color: only as a 2-same take from a pile of 4+
    return n === 1 && bank[i] >= 4
  }
  if (sel[i] >= 2) return false
  if (bank[i] - sel[i] <= 0) return false
  if (sel.some((x) => x >= 2)) return false
  return distinct < 3 && n < 3
}

/** Finds the legal take action matching the selection, if any. */
export function matchTake(legal: Action[], sel: Gems): Action | undefined {
  return legal.find((a) => (a.type === 'take_diff' || a.type === 'take_same') && sameGems(a.gems, sel))
}

/** Whether the selection is a strict prefix of some legal take (more tokens may be added). */
export function takeInProgress(legal: Action[], sel: Gems): boolean {
  return legal.some(
    (a) => (a.type === 'take_diff' || a.type === 'take_same') && a.gems.every((v, i) => v >= (sel[i] ?? 0)) && total(a.gems) > total(sel),
  )
}

/** Discard: must return exactly n tokens chosen from what the player holds. */
export function discardTarget(p: PlayerView): number {
  return Math.max(0, total(p.tokens) - 10)
}

export function canAddDiscard(sel: Gems, p: PlayerView, i: number): boolean {
  return total(sel) < discardTarget(p) && sel[i] < p.tokens[i]
}

export function matchDiscard(legal: Action[], sel: Gems): Action | undefined {
  return legal.find((a) => a.type === 'discard' && sameGems(a.gems, sel))
}

export function findAction(legal: Action[] | undefined, type: Action['type'], card?: number, level?: number): Action | undefined {
  return legal?.find((a) => a.type === type && (card === undefined || a.card === card) && (level === undefined || a.level === level))
}

/** Default payment the server will use: colored tokens first, gold covers the remainder. */
export function autoPay(p: PlayerView, c: Card): Gems | null {
  const pay = emptyGems()
  let gold = 0
  for (let i = 0; i < 5; i++) {
    const need = Math.max(0, c.cost[i] - p.bonuses[i])
    const use = Math.min(need, p.tokens[i])
    pay[i] = use
    gold += need - use
  }
  if (gold > p.tokens[5]) return null
  pay[5] = gold
  return pay
}

/** Remaining tokens needed per color to buy c (after bonuses and tokens, before gold). */
export function shortfall(p: PlayerView, c: Card): number[] {
  return GEM_COLORS.map((_, i) => Math.max(0, c.cost[i] - p.bonuses[i] - p.tokens[i]))
}

/** Shortfall summary: per-color gap, gold that can cover it, and what is still missing afterwards. */
export function gapOf(p: PlayerView, c: Card): { gap: number[]; gold: number; missing: number } {
  const gap = shortfall(p, c)
  const need = total(gap)
  const gold = Math.min(need, p.tokens[5])
  return { gap, gold, missing: need - gold }
}

/** Human-readable gap, e.g. "还缺 蓝2 黑1，可用金 1 抵扣，仍差 2 枚". */
export function gapText(p: PlayerView, c: Card): string {
  const { gap, gold, missing } = gapOf(p, c)
  const parts = GEM_COLORS.flatMap((col, i) => (gap[i] ? [`${COLOR_NAME[col]}${gap[i]}`] : []))
  if (parts.length === 0) return '宝石已足够，无需黄金'
  const base = `还缺 ${parts.join(' ')}`
  if (missing === 0) return `${base}，可用 ${gold} 枚黄金补足`
  return gold > 0 ? `${base}，黄金抵扣 ${gold} 枚后仍差 ${missing} 枚` : `${base}，共差 ${missing} 枚`
}

/**
 * Every legal way to pay for c: for each color, how many colored tokens to spend, with gold covering
 * the rest. Sorted so the default (fewest gold) comes first. Capped to keep the UI manageable.
 */
export function payOptions(p: PlayerView, c: Card, limit = 12): Gems[] {
  const need = GEM_COLORS.map((_, i) => Math.max(0, c.cost[i] - p.bonuses[i]))
  const out: Gems[] = []
  const cur = emptyGems()
  const rec = (i: number, gold: number) => {
    if (i === 5) {
      out.push([...cur.slice(0, 5), p.tokens[5] - gold])
      return
    }
    for (let use = Math.min(need[i], p.tokens[i]); use >= 0; use--) {
      const g = gold - (need[i] - use)
      if (g < 0) break
      cur[i] = use
      rec(i + 1, g)
    }
    cur[i] = 0
  }
  rec(0, p.tokens[5])
  out.sort((a, b) => a[5] - b[5])
  return out.slice(0, limit)
}

/** The buy action to send for a chosen payment; the default payment keeps the plain legal action. */
export function withPayment(a: Action, pay: Gems | null, def: Gems | null): Action {
  if (!pay || (def && sameGems(pay, def))) return a
  return { ...a, gems: [...pay], hasPay: true }
}

/** Short description of each player's latest turn, taken from the server log text without the name. */
export function lastMoves(log: GameEvent[], names: (seat: number) => string): Map<number, string> {
  const m = new Map<number, string>()
  for (const e of log) {
    if (e.seat < 0 || e.kind === 'game_over') continue
    // Choosing a noble is followed by the noble event itself; one line is enough.
    if (e.kind === 'action' && e.action.type === 'noble') continue
    const who = names(e.seat)
    const text = e.text.startsWith(`${who} `) ? e.text.slice(who.length + 1) : e.text
    // Returning tokens and noble visits belong to the turn's main move.
    const main = e.kind === 'action' && e.action.type !== 'discard'
    m.set(e.seat, main || !m.has(e.seat) ? text : `${m.get(e.seat)}；${text}`)
  }
  return m
}

/** lastMoves for replay frames (texts carry no name): consecutive frames of one seat form a turn. */
export function frameMoves(frames: { seat: number; text: string }[]): Map<number, string> {
  const m = new Map<number, string>()
  let prev = -1
  for (const f of frames) {
    if (f.seat < 0) continue
    m.set(f.seat, f.seat === prev && m.has(f.seat) ? `${m.get(f.seat)}；${f.text}` : f.text)
    prev = f.seat
  }
  return m
}

/** 将归还和贵族合并到同一回合，下一次主行动替换该玩家的上一步。 */
export function lastMoveEvents(events: MoveEvent[]): Map<number, MoveEvent[]> {
  const moves = new Map<number, MoveEvent[]>()
  for (const e of events) {
    if (e.seat < 0 || e.kind === 'game_over' || (e.kind === 'action' && e.action.type === 'noble')) continue
    const main = e.kind === 'action' && e.action.type !== 'discard'
    moves.set(e.seat, main ? [e] : [...(moves.get(e.seat) ?? []), e])
  }
  return moves
}

export function frameMoveEvents(frames: { events?: MoveEvent[] }[]): Map<number, MoveEvent[]> {
  return lastMoveEvents(frames.flatMap((f) => f.events ?? []))
}

/** Highlight keys for an analysis candidate action (cards/colors to emphasize on the board). */
export function highlightOf(a: Action | undefined): { cards: Set<number>; colors: Set<number>; deck?: number } {
  const cards = new Set<number>()
  const colors = new Set<number>()
  if (!a) return { cards, colors }
  if (a.type === 'buy' || a.type === 'buy_reserved' || a.type === 'reserve') cards.add(a.card)
  if (a.type === 'take_diff' || a.type === 'take_same') a.gems.forEach((v, i) => v > 0 && colors.add(i))
  return { cards, colors, deck: a.type === 'reserve_deck' ? a.level : undefined }
}

export function pct(x: number): string {
  return `${(x * 100).toFixed(1)}%`
}
