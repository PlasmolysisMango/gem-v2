import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { Action, Card, Gems, MoveEvent, RoomState, View } from '../api/types'
import { ALL_COLORS } from '../api/types'
import {
  canAddDiscard,
  canAddToken,
  COLOR_NAME,
  discardTarget,
  emptyGems,
  findAction,
  gapOf,
  gapText,
  gemsText,
  highlightOf,
  matchDiscard,
  matchTake,
  payOptions,
  seatLabel,
  takeInProgress,
  total,
  withPayment,
} from '../lib/gems'
import { CardTile, DeckTile, GEM_BG, GemToken, NobleTile } from './Pieces'
import { PanelDialog, PlayerPanel } from './Panels'

export { seatLabel } from '../lib/gems'

type Selection = { kind: 'card'; card: Card; reserved: boolean } | { kind: 'deck'; level: number } | null

const GAP_KEY = 'gem.showGap'

function loadShowGap(): boolean {
  try {
    return typeof localStorage !== 'undefined' && localStorage.getItem(GAP_KEY) === '1'
  } catch {
    return false
  }
}

/** Compact token chips for a payment, e.g. ●2 ●1 (gold included). */
function PayChips({ pay }: { pay: Gems }) {
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      {ALL_COLORS.map((c, i) =>
        pay[i] > 0 ? (
          <span
            key={c}
            className={`${GEM_BG[c]} w-5 h-5 rounded-full text-[11px] font-bold inline-flex items-center justify-center ring-1 ring-white/60`}
            aria-label={`${COLOR_NAME[c]} ${pay[i]}`}
          >
            {pay[i]}
          </span>
        ) : null,
      )}
      {total(pay) === 0 && <span className="text-xs">免费</span>}
    </span>
  )
}

/** "?" button that toggles the how-to-play bubble; closes on outside tap or Escape. */
function HelpTip() {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    if (!open) return
    const away = (e: PointerEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false)
    }
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('pointerdown', away)
    document.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('pointerdown', away)
      document.removeEventListener('keydown', esc)
    }
  }, [open])
  return (
    <span ref={box} className="relative">
      <button
        type="button"
        className={`w-8 h-8 rounded-full border text-sm font-bold ${open ? 'border-sky-300 bg-sky-800 text-white' : 'border-slate-500 text-slate-300 hover:bg-slate-700'}`}
        aria-label="操作说明"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        ?
      </button>
      {open && (
        <span
          role="tooltip"
          className="absolute top-full right-0 z-20 mt-1 w-60 rounded-lg border border-slate-600 bg-slate-800 p-2 text-xs leading-5 text-slate-200 shadow-lg"
        >
          轮到你时：点击银行筹码拿取，或点击卡牌购买/预留，点击牌堆盲扣。「显示缺口」会标出缺少的宝石；可买的卡牌显示最佳支付数量，优先保留黄金。
        </span>
      )}
    </span>
  )
}

/**
 * 交互棋盘；没有 onAct 时为观战/回放。
 * 横屏左侧贵族、右侧银行，下方我方信息及预留卡；竖屏按棋盘、银行、双方玩家信息排列。
 */
export function GameBoard({
  view,
  room,
  onAct,
  hintAction,
  lastMove,
  moveEvents,
  moves,
  moveHistory,
  playerFooter,
}: {
  view: View
  room: RoomState | null
  onAct?: (a: Action) => void
  hintAction?: Action
  lastMove?: string
  moveEvents?: MoveEvent[]
  moves?: Map<number, string>
  moveHistory?: Map<number, MoveEvent[]>
  playerFooter?: ReactNode
}) {
  const me = view.viewer >= 0 ? view.players[view.viewer] : null
  const legal = onAct && view.current === view.viewer ? (view.legal ?? []) : []
  const myTurn = legal.length > 0
  const [sel, setSel] = useState<number[]>(emptyGems())
  const [pick, setPick] = useState<Selection>(null)
  const [reserveDetail, setReserveDetail] = useState<{ card?: Card } | null>(null)
  const [payIdx, setPayIdx] = useState(0)
  const [showGap, setShowGap] = useState(loadShowGap)
  const hl = useMemo(() => highlightOf(hintAction), [hintAction])

  useEffect(() => {
    setSel(emptyGems())
    setPick(null)
    setReserveDetail(null)
  }, [view.turn, view.phase, view.current])
  useEffect(() => setPayIdx(0), [pick])

  const toggleGap = () => {
    const next = !showGap
    setShowGap(next)
    try {
      localStorage.setItem(GAP_KEY, next ? '1' : '0')
    } catch {
      // storage unavailable (private mode): keep the in-memory setting
    }
  }
  const gapFor = (c: Card | null) => (showGap && me && c && !c.hidden ? gapOf(me, c) : undefined)

  const takeAction = myTurn && view.phase === 'main' ? matchTake(legal, sel) : undefined
  const canMore = myTurn && view.phase === 'main' && takeInProgress(legal, sel)

  const clickBank = (i: number) => {
    if (!myTurn || view.phase !== 'main') return
    if (!canAddToken(sel, view.bank, i)) return
    const next = [...sel]
    next[i]++
    setSel(next)
    setPick(null)
  }
  const unselect = (i: number) => {
    const next = [...sel]
    next[i] = Math.max(0, next[i] - 1)
    setSel(next)
  }

  const select = (selection: NonNullable<Selection>) => {
    setSel(emptyGems())
    setPick(selection)
    if (selection.kind !== 'card' || !selection.reserved) setReserveDetail(null)
  }

  const send = (a?: Action) => {
    if (a && onAct) onAct(a)
    setSel(emptyGems())
    setPick(null)
    setReserveDetail(null)
  }

  const inspectMyReserved = (card?: Card) => {
    // 上一步盲扣事件只公开等级；我方可用自己可见的最新预留牌打开详情。
    const inspected = card?.hidden
      ? [...(me?.reserved ?? [])].reverse().find((c) => c.level === card.level && !c.hidden) ?? card
      : card
    setSel(emptyGems())
    setPick(inspected && !inspected.hidden ? { kind: 'card', card: inspected, reserved: true } : null)
    setReserveDetail({ card: inspected })
  }

  const cardActions = (c: Card, reserved: boolean) => ({
    buy: findAction(legal, reserved ? 'buy_reserved' : 'buy', c.id),
    reserve: reserved ? undefined : findAction(legal, 'reserve', c.id),
  })

  const pickInfo = (() => {
    if (!pick) return null
    if (pick.kind === 'deck') {
      const a = findAction(legal, 'reserve_deck', undefined, pick.level)
      return (
        <div className="rounded-lg bg-slate-800 p-3 space-y-2">
          <div>
            盲扣 L{pick.level} 牌堆顶牌{view.bank[5] > 0 ? '（获得 1 金）' : ''}
          </div>
          <div className="flex flex-wrap gap-2">
            <button className="btn-primary flex-1 sm:flex-none" disabled={!a} onClick={() => send(a)}>
              确认盲扣
            </button>
            <button className="btn" onClick={() => setPick(null)}>
              取消选择
            </button>
          </div>
        </div>
      )
    }
    const { buy, reserve } = cardActions(pick.card, pick.reserved)
    const options = me && buy ? payOptions(me, pick.card) : []
    const pay = options[Math.min(payIdx, options.length - 1)] ?? null
    return (
      <div className="rounded-lg bg-slate-800 p-2 sm:p-3 space-y-2 break-words">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span className="font-semibold">
            L{pick.card.level} {COLOR_NAME[pick.card.bonus]}卡 {pick.card.points} 分
          </span>
          {me && <span className={`text-sm ${buy ? 'text-lime-300' : 'text-rose-300'}`}>{gapText(me, pick.card)}</span>}
        </div>
        {options.length > 1 ? (
          <div className="space-y-1">
            <div className="text-xs text-slate-400">支付方式（黄金可替代任意颜色，默认优先用彩色宝石）：</div>
            <div className="flex flex-wrap gap-1.5" role="radiogroup" aria-label="支付方式">
              {options.map((o, i) => (
                <button
                  key={o.join()}
                  role="radio"
                  aria-checked={o === pay}
                  aria-label={`支付 ${gemsText(o)}`}
                  className={`min-h-[44px] rounded border px-2 ${o === pay ? 'border-yellow-300 bg-slate-700' : 'border-slate-600 hover:bg-slate-700/60'}`}
                  onClick={() => setPayIdx(i)}
                >
                  <PayChips pay={o} />
                </button>
              ))}
            </div>
          </div>
        ) : (
          pay && (
            <div className="flex items-center gap-2 text-sm text-slate-300">
              支付 <PayChips pay={pay} />
            </div>
          )
        )}
        <div className="flex flex-wrap gap-2">
          <button className="btn-primary flex-1 sm:flex-none" disabled={!buy} onClick={() => send(buy && withPayment(buy, pay, options[0] ?? null))}>
            购买
          </button>
          {!pick.reserved && (
            <button className="btn flex-1 sm:flex-none" disabled={!reserve} onClick={() => send(reserve)}>
              预留{reserve && view.bank[5] > 0 ? '（+1 金）' : ''}
            </button>
          )}
          <button className="btn" onClick={() => setPick(null)}>
            取消选择
          </button>
        </div>
      </div>
    )
  })()

  const passAction = findAction(legal, 'pass')
  const takeBar = myTurn && view.phase === 'main' && !pick && (total(sel) > 0 || passAction) && (
    <div className="rounded-lg bg-slate-800 p-2 sm:p-3 flex items-center gap-2 flex-wrap min-h-[3.5rem]" aria-live="polite">
      {total(sel) > 0 && (
        <>
          <span>拿取：</span>
          {ALL_COLORS.map((c, i) =>
            Array.from({ length: sel[i] }).map((_, k) => (
              <GemToken key={`${c}${k}`} color={c} size="sm" onClick={() => unselect(i)} title="点击放回" />
            )),
          )}
          <button className="btn-primary" disabled={!takeAction} onClick={() => send(takeAction)}>
            确认拿取
          </button>
          <button className="btn" onClick={() => setSel(emptyGems())}>
            清空
          </button>
          {!takeAction && canMore && <span className="text-xs text-slate-400">继续选择筹码…</span>}
          {me && total(me.tokens) + total(sel) > 10 && <span className="text-xs text-amber-400">拿取后超过 10 枚，需要归还</span>}
        </>
      )}
      {passAction && (
        <button className="btn" onClick={() => send(passAction)}>
          无法行动，跳过
        </button>
      )}
    </div>
  )

  const gapButton = me && (
    <button className={`rounded px-2 min-h-[32px] text-xs border ${showGap ? 'border-rose-400 bg-rose-900/60 text-rose-100' : 'border-slate-600 text-slate-300 hover:bg-slate-700'}`}
      aria-pressed={showGap} onClick={toggleGap} title="在每张卡上标出还缺几个什么颜色的宝石">
      {showGap ? '隐藏缺口' : '显示缺口'}
    </button>
  )
  const playerOrder = Array.from({ length: view.nPlayers }, (_, i) => (i + Math.max(0, view.viewer)) % view.nPlayers)

  return (
    <div className="game-board min-w-0 flex flex-col gap-2 sm:gap-3 xl:h-full xl:grid xl:grid-cols-[auto_minmax(0,1fr)_auto]">
      {/* nobles: a centered row on phones, a vertically centered column on the left on desktop */}
      <div className="board-nobles flex flex-wrap gap-1.5 sm:gap-2 items-center justify-center xl:col-start-1 xl:row-start-1 xl:flex-col xl:flex-nowrap xl:[justify-content:safe_center]">
        <span className="text-xs sm:text-sm text-slate-400 shrink-0">贵族</span>
        {view.nobles.map((n) => (
          <NobleTile key={n.id} noble={n} />
        ))}
      </div>
      {/* card rows, level 3 on top */}
      <div className="min-w-0 flex flex-col gap-1.5 sm:gap-2 xl:grid xl:grid-rows-[repeat(3,auto)] xl:col-start-2 xl:row-start-1">
        {[2, 1, 0].map((lv) => (
          <div key={lv} className="market-level grid min-w-0 gap-1.5 sm:gap-2 grid-cols-[2.75rem_repeat(4,minmax(0,1fr))] sm:grid-cols-[5rem_repeat(4,minmax(0,1fr))] xl:grid-cols-[minmax(3.5rem,5rem)_repeat(4,minmax(0,1fr))] xl:grid-rows-1">
            <DeckTile
              level={lv + 1}
              count={view.deckLen[lv]}
              selected={pick?.kind === 'deck' && pick.level === lv + 1}
              highlight={hl.deck === lv + 1}
              onClick={
                myTurn && view.phase === 'main' && findAction(legal, 'reserve_deck', undefined, lv + 1)
                  ? () => select({ kind: 'deck', level: lv + 1 })
                  : undefined
              }
            />
            {view.board[lv].map((c, i) => (
              <CardTile
                key={c ? c.id : `e${i}`}
                card={c}
                fluid
                gap={gapFor(c)}
                highlight={!!c && hl.cards.has(c.id)}
                selected={!!c && pick?.kind === 'card' && pick.card.id === c.id}
                affordable={!!c && myTurn && !!findAction(legal, 'buy', c.id)}
                onClick={c && myTurn && view.phase === 'main' ? () => select({ kind: 'card', card: c, reserved: false }) : undefined}
              />
            ))}
            {pick && (pick.kind === 'deck' ? pick.level === lv + 1 : !pick.reserved && pick.card.level === lv + 1) && (
              <div className="col-span-full" aria-label="卡牌操作" aria-live="polite">
                {pickInfo}
              </div>
            )}
          </div>
        ))}
      </div>
      {/* 银行：手机横排，PC 在原预留列竖排。 */}
      <section aria-label="银行宝石" className="min-w-0 space-y-1 xl:col-start-3 xl:row-start-1 xl:w-[clamp(4.5rem,6vw,8rem)] xl:grid xl:grid-rows-[auto_minmax(min-content,1fr)_auto] xl:gap-1">
        <div className="flex items-center gap-2 text-xs sm:text-sm text-slate-400 xl:justify-center">
          <span>银行<span className="xl:hidden">（点击选择）</span></span>
          {me && <span className="ml-auto xl:ml-0 flex items-center gap-2"><HelpTip /><span className="xl:hidden">{gapButton}</span></span>}
        </div>
        <div className="grid grid-cols-6 justify-items-center items-center gap-1 sm:gap-3 xl:grid-cols-1 xl:grid-rows-[repeat(6,minmax(min-content,1fr))] xl:gap-1">
          {ALL_COLORS.map((c, i) => (
            <GemToken key={c} color={c} count={view.bank[i] - sel[i]} size="lg" highlight={hl.colors.has(i)}
              onClick={myTurn && view.phase === 'main' && i < 5 ? () => clickBank(i) : undefined}
              disabled={!canAddToken(sel, view.bank, i)} />
          ))}
        </div>
        {me && <div className="hidden xl:block">{gapButton}</div>}
        {takeBar && <div className="xl:hidden">{takeBar}</div>}
      </section>
      <section aria-label="玩家信息" className="grid grid-cols-2 gap-1.5 xl:hidden">
        {playerOrder.map((seat) => (
          <PlayerPanel key={seat} view={view} room={room} seat={seat} compact own={seat === view.viewer}
            lastMove={moves?.get(seat) ?? (seat === view.viewer ? lastMove : undefined)}
            moveEvents={moveHistory?.get(seat) ?? (seat === view.viewer ? moveEvents : undefined)}
            onInspectReserved={seat === view.viewer ? inspectMyReserved : undefined} />
        ))}
      </section>
      {playerFooter != null && <div className="min-w-0 xl:hidden">{playerFooter}</div>}
      {/* 横屏保留我方信息与预留购买；竖屏资源统一放在上面的 2×2 玩家栏。 */}
      <section aria-label={me ? '我方信息' : '观战信息'} className="min-w-0 space-y-2 xl:col-span-3 xl:row-start-2">
        {me && (
          <div className="hidden gap-2 xl:grid xl:grid-cols-[minmax(12rem,1fr)_minmax(0,1fr)]">
            <PlayerPanel view={view} room={room} seat={view.viewer} own lastMove={moves?.get(view.viewer) ?? lastMove} moveEvents={moveHistory?.get(view.viewer) ?? moveEvents} />
            <div className="min-w-0 space-y-1">
              <div className="text-xs text-slate-400">我的预留 {me.reserved.length}/3</div>
              <div className="grid grid-cols-3 gap-1.5 sm:gap-2 xl:h-[clamp(6rem,18dvh,8rem)]">
                {me.reserved.map((c) => (
                  <CardTile key={c.id} card={c} fluid gap={gapFor(c)} highlight={hl.cards.has(c.id)}
                    selected={pick?.kind === 'card' && pick.card.id === c.id}
                    affordable={myTurn && !!findAction(legal, 'buy_reserved', c.id)}
                    onClick={myTurn && view.phase === 'main' ? () => select({ kind: 'card', card: c, reserved: true }) : undefined} />
                ))}
                {Array.from({ length: 3 - me.reserved.length }).map((_, i) => (
                  <div key={`empty${i}`} className="hidden sm:flex h-32 xl:h-full rounded-lg border-2 border-dashed border-slate-700 items-center justify-center text-xs text-slate-600">预留位</div>
                ))}
              </div>
              {pick?.kind === 'card' && pick.reserved && !reserveDetail && <div aria-label="预留卡操作" aria-live="polite">{pickInfo}</div>}
            </div>
          </div>
        )}
        {takeBar && <div className="hidden xl:block">{takeBar}</div>}
        {me && myTurn && view.phase === 'discard' && <DiscardBar view={view} legal={legal} onAct={send} />}
        {myTurn && view.phase === 'noble' && (
          <div className="rounded-lg bg-amber-900/60 p-3 flex flex-wrap items-center gap-3">
            <span className="w-full">同时满足多位贵族，请选择一位迎接：</span>
            {legal.filter((a) => a.type === 'noble').map((a) => {
              const n = view.nobles.find((x) => x.id === a.noble)
              return n ? <NobleTile key={a.noble} noble={n} onClick={() => send(a)} /> : null
            })}
          </div>
        )}
        {view.viewer < 0 && <div className="text-xs text-slate-500">观战视角：{seatLabel(room, view.current)} 行动中</div>}
      </section>
      {reserveDetail && me && (
        <PanelDialog title="我的预留详情" onClose={() => { setReserveDetail(null); setPick(null) }}>
          <div className="grid grid-cols-3 gap-2" aria-label="预留卡详情">
            {(reserveDetail.card ? [reserveDetail.card] : me.reserved).map((c, i) => (
              <div key={i} className="h-32">
                <CardTile card={c} fluid gap={gapFor(c)} selected={pick?.kind === 'card' && pick.card.id === c.id}
                  affordable={myTurn && !!findAction(legal, 'buy_reserved', c.id)}
                  onClick={myTurn && view.phase === 'main' && me.reserved.some((r) => r.id === c.id)
                    ? () => select({ kind: 'card', card: c, reserved: true }) : undefined} />
              </div>
            ))}
          </div>
          {pick?.kind === 'card' && pick.reserved && <div aria-label="预留卡操作" aria-live="polite">{pickInfo}</div>}
        </PanelDialog>
      )}
    </div>
  )
}

function DiscardBar({ view, legal, onAct }: { view: View; legal: Action[]; onAct: (a?: Action) => void }) {
  const me = view.players[view.viewer]
  const [sel, setSel] = useState<number[]>(emptyGems())
  const need = discardTarget(me)
  const a = matchDiscard(legal, sel)
  return (
    <div className="rounded-lg bg-rose-900/60 p-3 space-y-2">
      <div>
        筹码超过 10 枚，请选择归还 {need} 枚（已选 {total(sel)}）：
      </div>
      <div className="grid grid-cols-6 gap-0.5 sm:flex sm:gap-2">
        {ALL_COLORS.map((c, i) => (
          <GemToken
            key={c}
            color={c}
            count={me.tokens[i] - sel[i]}
            onClick={() => canAddDiscard(sel, me, i) && setSel(sel.map((v, j) => (j === i ? v + 1 : v)))}
            disabled={!canAddDiscard(sel, me, i)}
          />
        ))}
      </div>
      <div className="flex flex-wrap gap-2 items-center">
        <span className="text-sm w-full sm:w-auto">归还：{gemsText(sel)}</span>
        <button className="btn-primary" disabled={!a} onClick={() => onAct(a)}>
          确认归还
        </button>
        <button className="btn" onClick={() => setSel(emptyGems())}>
          重选
        </button>
      </div>
    </div>
  )
}
