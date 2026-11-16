import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { StaticRouter } from 'react-router-dom/server'
import Room from '../pages/Room'
import * as gameStore from '../store/game'
import type { Action, Analysis, Card, GameOver, MoveEvent, RoomState, View } from '../api/types'
import { GameBoard } from './GameBoard'
import { HintPanel } from './HintPanel'
import { ChatDialog, GameOverModal, LogDialog, PlayerPanel } from './Panels'
import { CardTile, GemToken, MoveIcons, TokenRow } from './Pieces'

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
    expect(html).toContain('w-full min-w-0 h-[6.5rem]')
    expect(html).toContain('aria-pressed="true"')
  })

  it('费用点无暗色底板、靠白环区分同色；四色费用与小卡按 2x2 排列不遮挡分数', () => {
    const four: Card = { id: 9, level: 2, bonus: 'blue', points: 2, cost: [1, 2, 3, 0, 4] }
    const big = renderToStaticMarkup(<CardTile card={four} fluid />)
    expect(big).not.toContain('bg-slate-950/55')
    expect(big).toContain('ring-white/90')
    expect(big).toContain('aria-label="蓝 2"')
    expect(big).toContain('grid grid-cols-2')
    const three = renderToStaticMarkup(<CardTile card={{ ...four, cost: [0, 2, 3, 0, 4] }} fluid />)
    expect(three).toContain('flex flex-col-reverse')
    const small = renderToStaticMarkup(<CardTile card={four} small />)
    expect(small).toContain('grid grid-cols-2')
  })

  it('缺口标记显示每色差额与黄金补足情况', () => {
    const html = renderToStaticMarkup(<CardTile card={card(1)} fluid gap={{ gap: [1, 0, 0, 1, 0], gold: 1, missing: 1 }} />)
    expect(html).toContain('还缺白 1')
    expect(html).toContain('还缺红 1')
    expect(html).toContain('差1')
    const ok = renderToStaticMarkup(<CardTile card={card(1)} fluid gap={{ gap: [0, 0, 0, 1, 0], gold: 1, missing: 0 }} />)
    expect(ok).toContain('金1')
  })

  it('棋盘保留所有明牌和牌堆，银行下方统一两列玩家栏，横屏我方预留仍常驻', () => {
    const html = renderToStaticMarkup(<GameBoard view={fixture()} room={null} onAct={() => {}} />)
    expect(html).toContain('grid-cols-[2.75rem_repeat(4,minmax(0,1fr))]')
    expect(html).toContain('xl:grid-rows-[repeat(3,auto)]')
    expect(html).toContain('xl:h-[clamp(6rem,18dvh,8rem)]')
    expect(html).toContain('显示缺口')
    expect(html).toContain('aria-label="操作说明"')
    expect(html).not.toContain('轮到你了')
    expect(html).toContain('xl:[justify-content:safe_center]')
    expect(html.match(/title="L[123] 蓝卡 1分"/g)).toHaveLength(15)
    expect(html).toContain('我的预留')
    expect(html).toContain('aria-label="银行宝石"')
    expect(html).toContain('xl:col-start-3 xl:row-start-1')
    expect(html).toContain('xl:grid-cols-1 xl:grid-rows-[repeat(6,minmax(min-content,1fr))]')
    expect(html).toContain('aria-label="我方信息"')
    const players = html.match(/<section aria-label="玩家信息"[\s\S]*?<\/section>/)?.[0] ?? ''
    expect(players).toContain('grid grid-cols-2 gap-1.5 xl:hidden')
    expect(players).toContain('我方 · 玩家1')
    expect(players).toContain('玩家2')
    expect(players.match(/aria-label="查看玩家[12]预留卡（3\/3）"/g)).toHaveLength(2)
    expect(players).not.toContain('title="L1 蓝卡 1分"')
    expect(html.indexOf('aria-label="银行宝石"')).toBeLessThan(html.indexOf('aria-label="玩家信息"'))
    expect(html.indexOf('aria-label="玩家信息"')).toBeLessThan(html.indexOf('aria-label="我方信息"'))
    const bank = html.match(/<section aria-label="银行宝石"[\s\S]*?<\/section>/)?.[0] ?? ''
    expect(bank).toContain('<span class="xl:hidden"><button')
    expect(bank.indexOf('aria-label="操作说明"')).toBeLessThan(bank.indexOf('显示缺口'))
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

  it('横屏对手资源六列居中，预留数量提供弹层入口且不常驻卡牌', () => {
    const html = renderToStaticMarkup(<PlayerPanel view={fixture()} room={null} seat={1} />)
    expect(html).toContain('grid w-full grid-cols-6 justify-items-center')
    expect(html).toContain('justify-evenly gap-1 whitespace-nowrap')
    expect(html).toContain('min-w-0 break-words font-semibold')
    expect(html).toContain('aria-label="查看玩家2预留卡（3/3）"')
    expect(html).toContain('aria-haspopup="dialog"')
    expect(html).not.toContain('title="L1 蓝卡 1分"')
    expect(html).toContain('上一步：')
  })

  it('竖屏双方紧凑玩家面板均以预留数量打开详情且保留上一步', () => {
    const html = renderToStaticMarkup(<PlayerPanel view={fixture()} room={null} seat={1} lastMove="购买 L1" compact />)
    expect(html).toContain('aria-haspopup="dialog"')
    expect(html).toContain('查看玩家2预留卡（3/3）')
    const own = renderToStaticMarkup(<PlayerPanel view={fixture()} room={null} seat={0} compact own />)
    expect(own).toContain('查看玩家1预留卡（3/3）')
    const landscapeOwn = renderToStaticMarkup(<PlayerPanel view={fixture()} room={null} seat={0} own />)
    expect(landscapeOwn).not.toContain('aria-haspopup="dialog"')
    expect(html).not.toContain('title="L1 蓝卡 1分"')
    expect(html).not.toContain('line-clamp-2')
    expect(html).toContain('购买 L1')
    expect(html).toContain('w-5 h-5')
  })

  it('六色筹码与五色卡牌数量按列对齐，手机小屏有紧凑尺寸', () => {
    const html = renderToStaticMarkup(<TokenRow tokens={[1, 2, 3, 4, 0, 1]} bonuses={[2, 1, 0, 3, 4]} dense />)
    expect(html.match(/枚"/g)).toHaveLength(6)
    expect(html.match(/张"/g)).toHaveLength(5)
    expect(html).toContain('grid w-full grid-cols-6 justify-items-center')
    expect(html).toContain('xs:w-6')
  })

  it('购买和支付、暗牌预留与黄金、归还和贵族均使用图标且不泄漏暗牌', () => {
    const move = (type: Action['type'], values: Partial<MoveEvent> = {}): MoveEvent => ({
      kind: 'action', seat: 1, action: action({ type }), card: 50, noble: -1, paid: [0, 1, 0, 0, 0, 1], gold: false, ...values,
    })
    const buy = renderToStaticMarkup(<MoveIcons events={[move('buy_reserved')]} cards={[card(50)]} />)
    expect(buy).toContain('aria-label="蓝卡 1分"')
    expect(buy).toContain('aria-label="蓝 1 枚"')
    expect(buy).toContain('aria-label="金 1 枚"')
    const hidden = renderToStaticMarkup(<MoveIcons events={[move('reserve_deck', { card: -12, gold: true })]} cards={[card(50)]} compact />)
    expect(hidden).toContain('L2 暗牌')
    expect(hidden).not.toContain('蓝卡')
    expect(hidden).toContain('aria-label="金 1 枚"')
    const extra = renderToStaticMarkup(<MoveIcons events={[
      move('discard', { action: action({ type: 'discard', gems: [0, 0, 0, 2, 0, 0] }) }),
      move('pass', { kind: 'noble', noble: 2 }),
    ]} cards={[]} />)
    expect(extra).toContain('aria-label="红 2 枚"')
    expect(extra).toContain('贵族到访，获得3分')
    expect(extra).toContain('flex-wrap')
  })

  it('横屏左侧只显示对手、右侧只显示 AI，聊天旁是日志按钮；竖屏双方在银行下方', () => {
    const state = gameStore.useGame.getState()
    const room: RoomState = {
      id: 'TEST', name: '测试房间', ownerId: 1, ownerName: '我方测试', status: 'playing', hints: false,
      turnSeconds: 0, yourSeat: 0, spectators: 0,
      seats: [0, 1].map((index) => ({ index, kind: 'human', name: index ? '对手测试' : '我方测试', connected: true })),
    }
    const spy = vi.spyOn(gameStore, 'useGame')
    try {
      spy.mockReturnValue({ ...state, room, view: fixture() })
      const html = renderToStaticMarkup(<StaticRouter location="/room/TEST"><Room /></StaticRouter>)
      const opponents = html.match(/<section aria-label="对手信息"[\s\S]*?<\/section>/)?.[0] ?? ''
      expect(opponents).toContain('对手测试')
      expect(opponents).not.toContain('我方测试')
      expect(opponents).toContain('hidden xl:flex')
      expect(html).toContain('我方 · 我方测试')
      const ai = html.match(/<aside aria-label="AI提示"[\s\S]*?<\/aside>/)?.[0] ?? ''
      expect(ai).toContain('room-hint')
      expect(ai).not.toContain('操作日志')
      expect(ai).not.toContain('聊天')
      expect(html).toMatch(/聊天[\s\S]*?<\/button><button[^>]*aria-label="操作日志"[^>]*>日志<\/button>/)
      spy.mockReturnValue({ ...state, room, view: { ...fixture(), viewer: -1 } })
      const spectator = renderToStaticMarkup(<StaticRouter location="/room/TEST"><Room /></StaticRouter>)
      const players = spectator.match(/<section aria-label="玩家信息"[\s\S]*?<\/section>/)?.[0] ?? ''
      expect(players).toContain('我方测试')
      expect(players).toContain('对手测试')
    } finally {
      spy.mockRestore()
    }
  })

  it('观战棋盘只有银行，不生成我方面板', () => {
    const view = { ...fixture(), viewer: -1 }
    const html = renderToStaticMarkup(<GameBoard view={view} room={null} />)
    expect(html).toContain('aria-label="银行宝石"')
    expect(html).not.toContain('aria-label="我方信息"')
    expect(html).not.toContain('我的预留')
  })

  it('四人竖屏玩家栏保持 2×2，我方非零座位优先，双方上一步预留图标可打开详情', () => {
    const view = fixture()
    view.viewer = 2
    view.nPlayers = 4
    view.players = [0, 1, 2, 3].map((seat) => ({ ...view.players[0], seat }))
    const move: MoveEvent = { kind: 'action', seat: 2, action: action({ type: 'reserve', card: 50 }), card: 50, noble: -1, paid: [0, 0, 0, 0, 0, 0], gold: true }
    const html = renderToStaticMarkup(<GameBoard view={view} room={null} moveHistory={new Map([0, 1, 2, 3].map((seat) => [seat, [{ ...move, seat }]]))} />)
    const players = html.match(/<section aria-label="玩家信息"[\s\S]*?<\/section>/)?.[0] ?? ''
    expect(players).toContain('grid-cols-2')
    expect(players).not.toContain('grid-cols-4')
    expect(players.match(/查看玩家[1-4]预留卡（3\/3）/g)).toHaveLength(4)
    expect(players.match(/查看上一步预留卡详情/g)).toHaveLength(4)
    expect(players.indexOf('我方 · 玩家3')).toBeLessThan(players.indexOf('玩家4'))
  })

  it('上一步盲扣图标提供详情入口但不从目录泄漏暗牌身份，购买图标不可打开预留', () => {
    const move: MoveEvent = { kind: 'action', seat: 1, action: action({ type: 'reserve_deck', level: 2 }), card: -12, noble: -1, paid: [0, 0, 0, 0, 0, 0], gold: true }
    const html = renderToStaticMarkup(<MoveIcons events={[move]} cards={[card(50)]} onReserveClick={() => {}} />)
    expect(html).toContain('查看上一步预留卡详情')
    expect(html).toContain('L2 暗牌')
    expect(html).not.toContain('蓝卡')
    const buy = renderToStaticMarkup(<MoveIcons events={[{ ...move, card: 50, action: action({ type: 'buy_reserved', card: 50 }) }]} cards={[card(50)]} onReserveClick={() => {}} />)
    expect(buy).not.toContain('查看上一步预留卡详情')
    const view = fixture()
    view.players[1].reserved = []
    const empty = renderToStaticMarkup(<PlayerPanel view={view} room={null} seat={1} />)
    expect(empty).toMatch(/disabled=""[^>]*>预留/)
  })

  it('日志共用有高度约束的模态弹层，完整保留日志和关闭入口', () => {
    const html = renderToStaticMarkup(<LogDialog log={[{ kind: 'action', seat: 0, action: action({}), card: -1, noble: -1, paid: [0, 0, 0, 0, 0, 0], gold: false, seq: 1, text: '测试操作' }]} onClose={() => {}} />)
    expect(html).toContain('aria-modal="true"')
    expect(html).toContain('result-dialog')
    expect(html).toContain('overflow-y-auto')
    expect(html).toContain('关闭操作日志')
    expect(html).toContain('测试操作')
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

  it('聊天以弹层打开：手机底部抽屉、大屏右侧抽屉，带关闭入口', () => {
    const html = renderToStaticMarkup(<ChatDialog chat={[{ from: 'alice', seat: 0, text: 'hi' }]} onSend={() => {}} onClose={() => {}} />)
    expect(html).toContain('role="dialog"')
    expect(html).toContain('h-[70dvh] w-full')
    expect(html).toContain('sm:w-96')
    expect(html).toContain('aria-label="关闭聊天"')
    expect(html).toContain('alice')
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
