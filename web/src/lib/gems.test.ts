import { describe, expect, it } from 'vitest'
import type { Action, PlayerView } from '../api/types'
import { autoPay, canAddDiscard, canAddToken, discardTarget, emptyGems, matchDiscard, matchTake, takeInProgress } from './gems'

const take = (gems: number[], type: Action['type'] = 'take_diff'): Action => ({ type, gems, card: -1, level: 0, noble: 0 })

describe('token selection', () => {
  const bank = [4, 3, 1, 0, 2, 5]
  it('allows up to three different colors', () => {
    const sel = emptyGems()
    expect(canAddToken(sel, bank, 0)).toBe(true)
    sel[0] = 1
    expect(canAddToken(sel, bank, 1)).toBe(true)
    sel[1] = 1
    expect(canAddToken(sel, bank, 2)).toBe(true)
    sel[2] = 1
    expect(canAddToken(sel, bank, 4)).toBe(false)
  })
  it('rejects empty piles and gold', () => {
    expect(canAddToken(emptyGems(), bank, 3)).toBe(false)
    expect(canAddToken(emptyGems(), bank, 5)).toBe(false)
  })
  it('allows two of a color only from a pile of four or more', () => {
    const sel = emptyGems()
    sel[0] = 1
    expect(canAddToken(sel, bank, 0)).toBe(true)
    const sel2 = emptyGems()
    sel2[1] = 1
    expect(canAddToken(sel2, bank, 1)).toBe(false)
    sel[0] = 2
    expect(canAddToken(sel, bank, 1)).toBe(false)
  })
  it('matches the legal action list', () => {
    const legal = [take([1, 1, 1, 0, 0, 0]), take([2, 0, 0, 0, 0, 0], 'take_same')]
    expect(matchTake(legal, [1, 1, 1, 0, 0, 0])?.type).toBe('take_diff')
    expect(matchTake(legal, [2, 0, 0, 0, 0, 0])?.type).toBe('take_same')
    expect(matchTake(legal, [1, 1, 0, 0, 0, 0])).toBeUndefined()
    expect(takeInProgress(legal, [1, 1, 0, 0, 0, 0])).toBe(true)
    expect(takeInProgress(legal, [0, 0, 0, 0, 1, 0])).toBe(false)
  })
})

const player = (tokens: number[], bonuses = [0, 0, 0, 0, 0]): PlayerView => ({
  seat: 0,
  tokens,
  bonuses,
  points: 0,
  reserved: [],
  cards: [],
  nCards: 0,
  nobles: [],
})

describe('discard', () => {
  it('requires returning down to ten', () => {
    const p = player([3, 3, 3, 2, 1, 0])
    expect(discardTarget(p)).toBe(2)
    const sel = emptyGems()
    expect(canAddDiscard(sel, p, 5)).toBe(false)
    sel[4] = 1
    expect(canAddDiscard(sel, p, 4)).toBe(false)
    expect(canAddDiscard(sel, p, 0)).toBe(true)
    const legal: Action[] = [{ type: 'discard', gems: [1, 0, 0, 0, 1, 0], card: -1, level: 0, noble: 0 }]
    sel[0] = 1
    expect(matchDiscard(legal, sel)).toBeDefined()
  })
})

describe('payment', () => {
  const card = { id: 1, level: 1, bonus: 'red' as const, points: 0, cost: [2, 1, 0, 0, 0] }
  it('uses bonuses, then colored tokens, then gold', () => {
    expect(autoPay(player([1, 1, 0, 0, 0, 1], [0, 0, 0, 0, 0]), card)).toEqual([1, 1, 0, 0, 0, 1])
    expect(autoPay(player([0, 0, 0, 0, 0, 0], [2, 1, 0, 0, 0]), card)).toEqual([0, 0, 0, 0, 0, 0])
    expect(autoPay(player([1, 0, 0, 0, 0, 1]), card)).toBeNull()
  })
})
