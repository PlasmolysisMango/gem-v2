import type { ReactElement, ReactNode } from 'react'
import { isValidElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { StaticRouter } from 'react-router-dom/server'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, type Frame, type Replay as ReplayData } from '../api/http'
import type { Action, View } from '../api/types'
import { GameBoard } from '../components/GameBoard'
import { HintPanel } from '../components/HintPanel'
import Replay from './Replay'

const route = vi.hoisted(() => ({ id: '1' }))
vi.mock('react-router-dom', async (original) => ({
  ...await original<typeof import('react-router-dom')>(),
  useParams: () => route,
}))

// The frontend test environment is Node-only. Drive Replay's hooks/effect cleanup
// without adding a DOM dependency; render its real child components with SSR.
let active: HookHarness | null = null
vi.mock('react', async (original) => {
  const react = await original<typeof import('react')>()
  return {
    ...react,
    useState: (initial: unknown) => active ? active.state(initial) : react.useState(initial),
    useRef: (initial: unknown) => active ? active.ref(initial) : react.useRef(initial),
    useMemo: (factory: () => unknown, deps: readonly unknown[]) => active ? active.memo(factory, deps) : react.useMemo(factory, deps),
    useEffect: (effect: Effect, deps?: readonly unknown[]) => active ? active.effect(effect, deps) : react.useEffect(effect, deps),
  }
})

type Effect = () => void | (() => void)
type Slot = { value?: unknown; deps?: readonly unknown[]; cleanup?: () => void }
type ControlsProps = { onIndexChange: (idx: number) => void; onPlayingChange: (playing: boolean) => void }
const sameDeps = (a?: readonly unknown[], b?: readonly unknown[]) => !!a && !!b && a.length === b.length && a.every((v, i) => Object.is(v, b[i]))

function elements(node: ReactNode): ReactElement[] {
  if (Array.isArray(node)) return node.flatMap(elements)
  if (!isValidElement<{ children?: ReactNode }>(node)) return []
  return [node, ...elements(node.props.children)]
}

class HookHarness {
  private slots: Slot[] = []
  private cursor = 0
  private pending: { slot: Slot; effect: Effect }[] = []
  private mounted = true
  dirty = true
  tree: ReactNode = null

  state(initial: unknown) {
    const index = this.cursor++
    if (!this.slots[index]) this.slots[index] = { value: typeof initial === 'function' ? initial() : initial }
    const slot = this.slots[index]
    return [slot.value, (update: unknown) => {
      const next = typeof update === 'function' ? update(slot.value) : update
      if (!Object.is(slot.value, next)) {
        slot.value = next
        this.dirty = true
      }
    }]
  }

  ref(initial: unknown) {
    const index = this.cursor++
    this.slots[index] ??= { value: { current: initial } }
    return this.slots[index].value
  }

  memo(factory: () => unknown, deps: readonly unknown[]) {
    const index = this.cursor++
    const slot = this.slots[index] ??= {}
    if (!sameDeps(slot.deps, deps)) {
      slot.value = factory()
      slot.deps = deps
    }
    return slot.value
  }

  effect(effect: Effect, deps?: readonly unknown[]) {
    const index = this.cursor++
    const slot = this.slots[index] ??= {}
    if (!sameDeps(slot.deps, deps)) {
      slot.deps = deps
      this.pending.push({ slot, effect })
    }
  }

  flush() {
    if (!this.mounted) return
    let renders = 0
    while (this.dirty) {
      if (++renders > 20) throw new Error('Replay effects keep restarting')
      this.dirty = false
      this.cursor = 0
      active = this
      try { this.tree = Replay() } finally { active = null }
      const pending = this.pending.splice(0)
      pending.forEach(({ slot }) => slot.cleanup?.())
      pending.forEach(({ slot, effect }) => { slot.cleanup = effect() || undefined })
    }
  }

  async settle() {
    for (let i = 0; i < 3; i++) {
      await Promise.resolve()
      this.flush()
    }
  }

  controls() {
    const control = elements(this.tree).find((node) => typeof node.type === 'function' && node.type.name === 'ReplayControls')
    if (!control) throw new Error('Replay controls missing')
    return (control as ReactElement<ControlsProps>).props
  }

  jump(idx: number) {
    this.controls().onIndexChange(idx)
    this.flush()
  }

  html() {
    return renderToStaticMarkup(<StaticRouter location={`/replay/${route.id}`}>{this.tree}</StaticRouter>)
  }

  dispose() {
    this.mounted = false
    this.slots.forEach((slot) => slot.cleanup?.())
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

const action: Action = { type: 'reserve_deck', gems: [0, 0, 0, 0, 0, 0], card: 0, level: 1, noble: 0 }
function fixture(id = 1): ReplayData {
  const initial: View = {
    viewer: -1, nPlayers: 4, current: 0, phase: 'main', turn: 0, round: 1,
    bank: [4, 4, 4, 4, 4, 5], deckLen: [36, 26, 16],
    board: [1, 2, 3].map(() => [null, null, null, null]), nobles: [],
    players: [0, 1, 2, 3].map((seat) => ({ seat, tokens: [0, 0, 0, 0, 0, 0], bonuses: [0, 0, 0, 0, 0], points: 0, reserved: [], cards: [], nCards: 0, nobles: [] })),
    lastRound: false, winners: [],
  }
  return {
    id, roomId: `ROOM${id}`, seed: '42', playerCount: 4, status: 'finished', startedAt: 1, endedAt: 2, winnerSeat: 0,
    players: [0, 1, 2, 3].map((seat) => ({ seat, userId: seat + 1, username: `测试玩家${seat + 1}`, finalPoints: 0, winner: seat === 0 })),
    initial,
    frames: [10, 20, 30, 40].map((seq, i) => ({ seq, seat: i, text: `测试行动${seq}`, view: { ...initial, turn: i + 1 }, turn: i, main: seq !== 20 })),
  }
}
function analyzed(frame: Frame): Frame {
  return {
    ...frame,
    analysis: { seat: frame.seat, level: 'master', strategy: 'engine', strategyText: '测试策略', winRate: 0.6, iterations: 100, elapsedMs: 100, endgame: false,
      candidates: [{ action, text: '测试候选', winRate: 0.6, visits: 10, reasons: [], pv: [] }] },
    review: { seat: frame.seat, played: action, playedText: frame.text, best: action, bestText: '测试候选', playedWinRate: 0.5, bestWinRate: 0.6, delta: 0.1, verdict: 'mistake', verdictText: '测试失误', comment: '测试评语' },
  }
}
function savedFixture(id = 1) {
  const data = fixture(id)
  return { ...data, frames: data.frames.map((frame) => frame.main ? analyzed(frame) : frame) }
}

type AnalysisRequest = ReturnType<typeof deferred<Frame>> & { id: number; seq: number; signal: AbortSignal }
let requests: AnalysisRequest[]
let harness: HookHarness
beforeEach(() => {
  route.id = '1'
  requests = []
  harness = new HookHarness()
  vi.spyOn(api, 'game').mockResolvedValue(fixture())
  vi.spyOn(api, 'replayAnalysis').mockImplementation((id, seq, signal) => {
    const request = { ...deferred<Frame>(), id, seq, signal: signal! }
    requests.push(request)
    return request.promise
  })
})
afterEach(() => {
  harness.dispose()
  vi.restoreAllMocks()
})
const seqs = () => requests.map((request) => request.seq)

describe('Replay controls and analysis effects', () => {
  it('places identical controls and the step verdict below all desktop players and immediately after mobile players, keeping only the action text above the board', async () => {
    vi.mocked(api.game).mockResolvedValue(savedFixture())
    await harness.settle()
    harness.jump(3)
    const html = harness.html()
    const desktop = html.match(/<section aria-label="玩家"[\s\S]*?<\/section>/)?.[0] ?? ''
    expect(desktop).toContain('hidden xl:grid')
    for (let seat = 1; seat <= 4; seat++) {
      expect(desktop.indexOf(`测试玩家${seat}`)).toBeLessThan(desktop.indexOf('aria-label="复盘控制"'))
    }
    const controls = html.match(/<div aria-label="复盘控制"[\s\S]*?aria-label="复盘进度"[\s\S]*?<\/span><\/div>[\s\S]*?<\/div><\/div>/g) ?? []
    expect(controls).toHaveLength(2)
    expect(controls[0]).toBe(controls[1])
    expect((controls[0] ?? '').match(/<button\b/g)).toHaveLength(5)
    expect((controls[0] ?? '').match(/<svg\b/g)).toHaveLength(5)
    expect((controls[0] ?? '').match(/aria-hidden="true" focusable="false"/g)).toHaveLength(5)
    for (const label of ['回到初始局面', '上一手', '播放', '下一手', '跳到最后一手']) expect(controls[0]).toContain(`aria-label="${label}"`)
    expect(controls[0]).not.toMatch(/>(首|前|播放|后|末)<\/button>/)
    expect((controls[0] ?? '').match(/w-11 h-11 min-w-\[44px\] min-h-\[44px\] xl:w-8 xl:h-8 xl:min-w-\[32px\] xl:min-h-\[32px\]/g)).toHaveLength(5)
    expect(controls[0]).toContain('flex justify-center')
    expect(controls[0]).toContain('aria-label="复盘进度"')
    expect(controls[0]).toContain('3/4')
    expect(controls[0]).toContain('测试失误')
    expect(controls[0]!.indexOf('测试失误')).toBeGreaterThan(controls[0]!.indexOf('aria-label="复盘进度"'))
    expect(html).not.toMatch(/[⏮⏭◀▶]/)
    const players = html.match(/<section aria-label="玩家信息"[\s\S]*?<\/section>/)?.[0] ?? ''
    expect(html).toContain(`${players}<div class="min-w-0 xl:hidden">${controls[1]}</div>`)
    const board = html.slice(html.indexOf('<section id="replay-board"'))
    expect(board.indexOf('测试行动30')).toBeLessThan(board.indexOf('class="game-board'))
    expect(board.slice(0, board.indexOf('class="game-board'))).not.toContain('测试失误')
    expect(board.slice(0, board.indexOf('class="game-board'))).not.toContain('aria-label="复盘控制"')
    expect(requests).toHaveLength(0)
  })

  it('aborts an old request on navigation and prioritizes frames[idx] before earlier missing main frames', async () => {
    await harness.settle()
    expect(seqs()).toEqual([10])
    harness.jump(2)
    expect(requests[0].signal.aborted).toBe(true)
    expect(seqs()).toEqual([10, 30])
    requests[0].resolve(analyzed(fixture().frames[0])) // even a fetch ignoring abort cannot overwrite the cache
    await harness.settle()
    expect(seqs()).toEqual([10, 30])
    expect(harness.html()).toContain('0/3，当前请求第 3 手')
    requests[1].resolve(analyzed(fixture().frames[2]))
    await harness.settle()
    expect(seqs()).toEqual([10, 30, 10])
    expect(requests[1].signal.aborted).toBe(false)
    expect(harness.html()).toContain('1/3，当前请求第 1 手')
    expect(vi.mocked(api.game)).toHaveBeenCalledTimes(1)
  })

  it('keeps completed frames cached and does not restart its own effect after setData', async () => {
    await harness.settle()
    requests[0].resolve(analyzed(fixture().frames[0]))
    await harness.settle()
    expect(seqs()).toEqual([10, 30]) // frame 20 is not a main action
    expect(requests[0].signal.aborted).toBe(false)
    harness.jump(3)
    expect(requests[1].signal.aborted).toBe(true)
    expect(seqs()).toEqual([10, 30, 40])
    requests[2].resolve(analyzed(fixture().frames[3]))
    await harness.settle()
    expect(seqs()).toEqual([10, 30, 40, 30])
    expect(harness.html()).toContain('2/3，当前请求第 3 手')
    harness.jump(0)
    expect(requests[3].signal.aborted).toBe(true)
    expect(seqs()).toEqual([10, 30, 40, 30, 30])
    expect(harness.html()).not.toContain('本手分析生成中')
    requests[4].resolve(analyzed(fixture().frames[2]))
    await harness.settle()
    expect(harness.html()).toContain('大师 AI 分析已完成：3/3')
    harness.jump(4)
    harness.jump(2)
    expect(seqs()).toEqual([10, 30, 40, 30, 30])
    expect(api.game).toHaveBeenCalledTimes(1)
  })

  it('shows an actual paused error and retries only missing frames without reloading the record', async () => {
    await harness.settle()
    requests[0].resolve(analyzed(fixture().frames[0]))
    await harness.settle()
    requests[1].reject(new Error('分析请求失败'))
    await harness.settle()
    const html = harness.html()
    expect(html).toContain('分析暂停：1/3')
    expect(html).toContain('分析请求失败')
    expect(html).toContain('重试分析')
    expect(html).not.toContain('实时补算中')
    expect(html).not.toContain('本手分析生成中')
    const retry = elements(harness.tree).find((node) => node.type === 'button' && node.props.children === '重试分析')
    expect(retry).toBeDefined()
    retry!.props.onClick()
    harness.flush()
    expect(requests[1].signal.aborted).toBe(true)
    expect(seqs()).toEqual([10, 30, 30])
    expect(harness.html()).not.toContain('分析暂停')
    expect(harness.html()).toContain('1/3，当前请求第 3 手')
    requests[2].resolve(analyzed(fixture().frames[2]))
    await harness.settle()
    expect(seqs()).toEqual([10, 30, 30, 40])
    expect(api.game).toHaveBeenCalledTimes(1)
  })

  it('reports the background frame truthfully without claiming a non-main current frame is loading', async () => {
    await harness.settle()
    harness.jump(1)
    expect(requests[0].signal.aborted).toBe(true)
    expect(seqs()).toEqual([10, 10])
    expect(harness.html()).toContain('0/3，当前请求第 1 手')
    expect(harness.html()).toContain('归还宝石或选择贵族阶段')
    expect(harness.html()).not.toContain('本手分析生成中')
  })

  it('cancels analysis on game changes/unmount and ignores late results from the old game', async () => {
    await harness.settle()
    vi.mocked(api.game).mockResolvedValue(fixture(2))
    route.id = '2'
    harness.dirty = true
    await harness.settle()
    expect(requests[0].signal.aborted).toBe(true)
    expect(requests.map(({ id, seq }) => [id, seq])).toEqual([[1, 10], [2, 10]])
    requests[0].resolve(analyzed(fixture().frames[0]))
    await harness.settle()
    expect(harness.html()).toContain('对局复盘 #2')
    expect(harness.html()).toContain('0/3，当前请求第 1 手')
    harness.dispose()
    expect(requests[1].signal.aborted).toBe(true)
    requests[1].resolve(analyzed(fixture(2).frames[0]))
    await harness.settle()
    expect(requests).toHaveLength(2)
  })

  it('cancels the initial GET on id changes and never starts analysis for a stale response', async () => {
    const first = deferred<ReplayData>()
    const second = deferred<ReplayData>()
    vi.mocked(api.game).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    harness.flush()
    const firstSignal = vi.mocked(api.game).mock.calls[0][1]!
    route.id = '2'
    harness.dirty = true
    harness.flush()
    expect(firstSignal.aborted).toBe(true)
    first.resolve(fixture())
    await harness.settle()
    expect(requests).toHaveLength(0)
    expect(harness.html()).toContain('加载中')
    second.resolve(savedFixture(2))
    await harness.settle()
    expect(harness.html()).toContain('对局复盘 #2')
    expect(requests).toHaveLength(0)
    expect(api.game).toHaveBeenCalledTimes(2)
    harness.dispose()
    expect(vi.mocked(api.game).mock.calls[1][1]!.aborted).toBe(true)
  })

  it('keeps candidate highlighting, saved reviews/WinCurve, playback text and boundary controls', async () => {
    vi.mocked(api.game).mockResolvedValue(savedFixture())
    await harness.settle()
    const hint = elements(harness.tree).find((node) => node.type === HintPanel)!
    hint.props.onFocus(0)
    harness.flush()
    const board = elements(harness.tree).find((node) => node.type === GameBoard)!
    expect(board.props.hintAction).toEqual(action)
    expect(harness.html()).toContain('测试候选')
    expect(harness.html()).toContain('实际 50.0%，最佳 60.0%')
    expect(harness.html()).toMatch(/aria-label="回到初始局面" disabled=""/)
    harness.controls().onPlayingChange(true)
    harness.flush()
    expect(harness.html()).toContain('aria-label="暂停"')
    expect(harness.html()).toContain('d="M6 5h4v14H6zM14 5h4v14h-4z"')
    harness.jump(4)
    expect(harness.html()).toContain('aria-label="播放"')
    expect(harness.html()).toContain('d="M8 5v14l10-7z"')
    expect(harness.html()).toMatch(/aria-label="下一手" disabled=""/)
    expect(harness.html()).toMatch(/aria-label="跳到最后一手" disabled=""/)
    expect(requests).toHaveLength(0)
  })
})
