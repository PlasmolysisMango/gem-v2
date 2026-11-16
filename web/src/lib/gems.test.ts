import { describe, expect, it } from 'vitest'
import type { Action, GameEvent, PlayerView } from '../api/types'
import {
  autoPay,
  canAddDiscard,
  canAddToken,
  discardTarget,
  emptyGems,
  frameMoves,
  frameMoveEvents,
  gapOf,
  gapText,
  lastMoves,
  lastMoveEvents,
  matchDiscard,
  matchTake,
  payOptions,
  takeInProgress,
  withPayment,
} from './gems'

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

  it('lists every gold/colored split with the default first', () => {
    // needs white 2 + blue 1; holds white 2, blue 1, gold 2
    const p = player([2, 1, 0, 0, 0, 2])
    const opts = payOptions(p, card)
    expect(opts[0]).toEqual(autoPay(p, card))
    expect(opts[0]).toEqual([2, 1, 0, 0, 0, 0])
    expect(opts).toContainEqual([1, 1, 0, 0, 0, 1])
    expect(opts).toContainEqual([2, 0, 0, 0, 0, 1])
    expect(opts).toContainEqual([0, 1, 0, 0, 0, 2])
    expect(opts).toContainEqual([1, 0, 0, 0, 0, 2])
    expect(opts).toHaveLength(5)
    for (const o of opts) {
      // never overpays: colored + gold equals the price after bonuses
      expect(o[0] + o[1] + o[5]).toBe(3)
    }
    expect(payOptions(player([1, 0, 0, 0, 0, 1]), card)).toEqual([])
  })

  it('sends the explicit payment only when it differs from the default', () => {
    const buy: Action = { type: 'buy', gems: emptyGems(), card: 1, level: 0, noble: 0 }
    const def = [2, 1, 0, 0, 0, 0]
    expect(withPayment(buy, def, def)).toBe(buy)
    expect(withPayment(buy, [1, 1, 0, 0, 0, 1], def)).toEqual({ ...buy, gems: [1, 1, 0, 0, 0, 1], hasPay: true })
  })

  it('reports the per-color gap and whether gold covers it', () => {
    expect(gapOf(player([1, 0, 0, 0, 0, 1]), card)).toEqual({ gap: [1, 1, 0, 0, 0], gold: 1, missing: 1 })
    expect(gapText(player([1, 0, 0, 0, 0, 1]), card)).toBe('还缺 白1 蓝1，黄金抵扣 1 枚后仍差 1 枚')
    expect(gapText(player([1, 1, 0, 0, 0, 1]), card)).toBe('还缺 白1，可用 1 枚黄金补足')
    expect(gapText(player([0, 0, 0, 0, 0, 0], [2, 1, 0, 0, 0]), card)).toBe('宝石已足够，无需黄金')
  })
})

describe('last moves', () => {
  const ev = (seat: number, text: string, kind: GameEvent['kind'] = 'action', type: Action['type'] = 'take_diff'): GameEvent => ({
    kind,
    seat,
    action: { type, gems: emptyGems(), card: 0, level: 0, noble: 0 },
    card: 0,
    noble: 0,
    paid: emptyGems(),
    gold: false,
    seq: 0,
    text,
  })
  const names = (s: number) => ['小明', '小红'][s]

  it('keeps each seat latest turn, joining discards and nobles to it', () => {
    const m = lastMoves(
      [
        ev(0, '小明 拿取 白 蓝 绿'),
        ev(1, '小红 购买 L1'),
        ev(1, '小红 迎接贵族', 'noble'),
        ev(0, '小明 拿取 红 红'),
        ev(0, '小明 归还 红', 'action', 'discard'),
        ev(-1, '对局结束', 'game_over'),
      ],
      names,
    )
    expect(m.get(0)).toBe('拿取 红 红；归还 红')
    expect(m.get(1)).toBe('购买 L1；迎接贵族')
  })

  it('图标保留主行动、归还与贵族，忽略重复的贵族选择和结束事件', () => {
    const first = ev(0, '拿取')
    const next = ev(0, '买卡', 'action', 'buy')
    const discard = ev(0, '归还', 'action', 'discard')
    const noble = ev(0, '贵族', 'noble')
    const other = ev(1, '预留', 'action', 'reserve')
    const m = lastMoveEvents([first, other, next, discard, ev(0, '选贵族', 'action', 'noble'), noble, ev(-1, '结束', 'game_over')])
    expect(m.get(0)).toEqual([next, discard, noble])
    expect(m.get(1)).toEqual([other])
    expect(m.has(-1)).toBe(false)
  })

  it('回放图标合并同回合多个帧，支持没有 events 的旧数据', () => {
    const main = ev(0, '拿取')
    const discard = ev(0, '归还', 'action', 'discard')
    const noble = ev(0, '贵族', 'noble')
    const m = frameMoveEvents([{ events: [main] }, { events: [discard, noble] }, {}])
    expect(m.get(0)).toEqual([main, discard, noble])
    expect(frameMoveEvents([{}]).size).toBe(0)
  })

  it('groups consecutive replay frames of one seat', () => {
    const m = frameMoves([
      { seat: 0, text: 'a' },
      { seat: 1, text: 'b' },
      { seat: 1, text: 'c' },
      { seat: 0, text: 'd' },
    ])
    expect(m.get(0)).toBe('d')
    expect(m.get(1)).toBe('b；c')
  })
})
