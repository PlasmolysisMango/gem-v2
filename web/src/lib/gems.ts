// Pure game-UI logic shared by components (unit tested with vitest).
import type { Action, Card, Color, Gems, PlayerView } from '../api/types'
import { ALL_COLORS, GEM_COLORS } from '../api/types'

export const COLOR_NAME: Record<Color, string> = {
  white: '白',
  blue: '蓝',
  green: '绿',
  red: '红',
  black: '黑',
  gold: '金',
}

export const COLOR_INDEX: Record<Color, number> = { white: 0, blue: 1, green: 2, red: 3, black: 4, gold: 5 }

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
