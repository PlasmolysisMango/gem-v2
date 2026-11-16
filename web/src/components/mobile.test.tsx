import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Action, Analysis, Card, GameOver, View } from '../api/types'
import { GameBoard } from './GameBoard'
import { HintPanel } from './HintPanel'
import { GameOverModal, PlayerPanel } from './Panels'
import { CardTile, GemToken } from './Pieces'

const action = (values: Partial<Action>): Action => ({
  type: 'pass',
  gems: [0, 0, 0, 0, 0, 0],
  card: 0,
  level: 0,
  noble: 0,
  ...values,
})
const card = (id: number, level = 1): Card => ({ id, level, bonus: 'blue', points: 1, cost: [1, 0, 1, 1, 1] })
function fixture(phase: View['phase'] = 'main'): View {
  return {
    viewer: 0,
    nPlayers: 2,
    current: 0,
    phase,
    turn: 1,
    round: 1,
    bank: [4, 4, 4, 4, 4, 5],
    deckLen: [36, 26, 16],
    board: [1, 2, 3].map((level) => [1, 2, 3, 4].map((i) => card(level * 10 + i, level))),
    nobles: [1, 2, 3, 4, 5].map((id) => ({ id, points: 3, req: [3, 3, 3, 0, 0] })),
    players: [0, 1].map((seat) => ({
      seat,
      tokens: [4, 2, 2, 2, 2, 0],
      bonuses: [0, 0, 0, 0, 0],
      points: 1,
      cards: [],
      nCards: 0,
      reserved: [card(50), card(51), card(52)],
      nobles: [],
    })),
    lastRound: false,
    winners: [],
    legal:
      phase === 'discard'
        ? [action({ type: 'discard', gems: [2, 0, 0, 0, 0, 0] })]
        : phase === 'noble'
          ? [action({ type: 'noble', noble: 1 }), action({ type: 'noble', noble: 2 })]
          : [action({ type: 'take_diff', gems: [1, 1, 1, 0, 0, 0] }), action({ type: 'reserve_deck', level: 1 })],
  }
}

describe('手机端组件结构', () => {
  it('可点击筹码提供 44px 触控目标，装饰筹码保持紧凑', () => {
    const clickable = renderToStaticMarkup(<GemToken color="blue" count={2} size="sm" onClick={() => {}} />)
    expect(clickable).toContain('w-11 h-11')
    expect(clickable).toContain('aria-label="蓝 2 枚"')
    const decoration = renderToStaticMarkup(<GemToken color="blue" count={2} size="sm" />)
    expect(decoration).toContain('w-6 h-6')
    expect(decoration).toContain('disabled')
  })

  it('卡牌可以填满网格单元，不强制撑宽手机页面', () => {
    const html = renderToStaticMarkup(<CardTile card={card(1)} fluid selected onClick={() => {}} />)
    expect(html).toContain('w-full min-w-0 h-32')
    expect(html).toContain('aria-pressed="true"')
  })

  it('窄屏棋盘保留所有明牌、牌堆、银行和自己的三张预留', () => {
    const html = renderToStaticMarkup(<GameBoard view={fixture()} room={null} onAct={() => {}} />)
    expect(html).toContain('grid-cols-2 min-[420px]:grid-cols-4')
    expect(html).toContain('sm:grid-cols-[5rem_repeat(4,6rem)]')
    expect(html.match(/title="L[123] 蓝卡 1分"/g)).toHaveLength(15)
    expect(html).toContain('我的预留')
    expect(html).toContain('银行（点击选择）')
    expect(html).not.toContain('fixed')
  })

  it('归还筹码和选贵族阶段仍有完整操作入口且允许换行', () => {
    const discard = renderToStaticMarkup(<GameBoard view={fixture('discard')} room={null} onAct={() => {}} />)
    expect(discard).toContain('确认归还')
    expect(discard).toContain('grid-cols-6 gap-0.5')
    const noble = renderToStaticMarkup(<GameBoard view={fixture('noble')} room={null} onAct={() => {}} />)
    expect(noble).toContain('同时满足多位贵族')
    expect(noble).toContain('flex flex-wrap items-center gap-3')
  })

  it('对手预留卡使用换行容器而非挤压在单行', () => {
    const html = renderToStaticMarkup(<PlayerPanel view={fixture()} room={null} seat={1} />)
    expect(html).toContain('flex flex-wrap gap-1 mt-1')
    expect(html).toContain('min-w-0 break-words font-semibold')
  })

  it('提示候选支持键盘与触控，选择候选不会自动执行动作', () => {
    const hint: Analysis = {
      seat: 0,
      level: 'hard',
      strategy: 'engine',
      strategyText: '引擎',
      winRate: 0.6,
      iterations: 100,
      elapsedMs: 1000,
      endgame: false,
      candidates: [{ action: action({ type: 'reserve_deck', level: 1 }), text: '盲扣', winRate: 0.6, visits: 10, reasons: [], pv: [] }],
    }
    const html = renderToStaticMarkup(
      <HintPanel enabled myTurn loading={false} hint={hint} onRequest={() => {}} onFocus={() => {}} focus={0} lastReview={null} />,
    )
    expect(html).toContain('role="button"')
    expect(html).toContain('tabindex="0"')
    expect(html).toContain('不会自动落子')
    expect(html).toContain('shrink-0')
  })

  it('结果弹窗宽度受视口约束、可滚动并有顶部关闭入口', () => {
    const over: GameOver = { gameId: 1, winners: [0], points: [15, 12], cards: [7, 8], reviews: [], curve: [] }
    const html = renderToStaticMarkup(<GameOverModal over={over} room={null} onClose={() => {}} onReplay={() => {}} />)
    expect(html).toContain('role="dialog"')
    expect(html).toContain('aria-modal="true"')
    expect(html).toContain('w-full max-w-[560px] min-w-0')
    expect(html).toContain('overflow-y-auto overscroll-contain')
    expect(html).toContain('aria-label="关闭对局结果"')
    expect(html).not.toMatch(/(?:^|[\s"])w-\[560px\](?=[\s"])/)
  })
})
