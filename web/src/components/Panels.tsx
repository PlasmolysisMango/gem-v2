import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import type { Card, ChatMessage, CurvePoint, GameEvent, GameOver, MoveEvent, MoveReview, RoomState, View } from '../api/types'
import { pct, seatLabel, total } from '../lib/gems'
import { ReviewBadge } from './HintPanel'
import { CardTile, MoveIcons, TokenRow, useCardCatalog } from './Pieces'

/** 按人数设置平板列数；PC 左栏单列，手机优先两列。 */
export const PANEL_COLS: Record<number, string> = { 1: 'lg:grid-cols-1', 2: 'lg:grid-cols-2', 3: 'sm:grid-cols-3', 4: 'lg:grid-cols-4' }

/** 玩家资源和上一回合。手机折叠对手预留卡；我方预留卡由棋盘提供购买入口。 */
export function PlayerPanel({
  view,
  room,
  seat,
  lastMove,
  compact,
  moveEvents,
  own,
  onInspectReserved,
}: {
  view: View
  room: RoomState | null
  seat: number
  lastMove?: string
  compact?: boolean
  moveEvents?: MoveEvent[]
  own?: boolean
  onInspectReserved?: (card?: Card) => void
}) {
  const [detail, setDetail] = useState<{ card?: Card; kind: 'reserve' | 'buy' } | null>(null)
  const p = view.players[seat]
  const info = room?.seats[seat]
  const active = view.current === seat && view.phase !== 'over'
  const winner = view.winners.includes(seat)
  const cards = useCardCatalog()
  const visibleCards = [...cards, ...view.board.flat().filter((c): c is NonNullable<typeof c> => c !== null), ...p.reserved]
  const canInspect = !own || compact
  const inspect = (card?: Card) => {
    if (onInspectReserved) onInspectReserved(card)
    else setDetail({ card, kind: 'reserve' })
  }
  const inspectBought = (card: Card) => setDetail({ card, kind: 'buy' })
  return (
    <div
      className={`min-w-0 rounded-lg border-2 ${compact ? 'p-1.5' : 'p-2'} ${own ? 'h-full' : ''}
        ${active ? 'border-yellow-400 bg-slate-800' : 'border-slate-700 bg-slate-800/50'}`}
    >
      <div className="flex justify-between items-start gap-2">
        <span className={`min-w-0 break-words font-semibold ${compact ? 'text-sm leading-tight' : ''}`}>
          {own ? '我方 · ' : ''}{seatLabel(room, seat)}
          {seat === view.viewer && <span className="text-xs text-sky-300"> (你)</span>}
          {info && info.kind === 'human' && !info.connected && <span className="text-xs text-rose-300"> 离线</span>}
          {winner && <span className="text-xs text-yellow-300"> 🏆</span>}
        </span>
        <span className={`shrink-0 font-black leading-none text-yellow-300 ${compact ? 'text-xl' : 'text-2xl'}`} aria-label={`${p.points} 分`}>{p.points}<span className="text-[10px] font-normal ml-0.5">分</span></span>
      </div>
      <TokenRow tokens={p.tokens} bonuses={p.bonuses} dense={compact} />
      <div className={`flex items-center justify-evenly gap-1 whitespace-nowrap border-t border-slate-700/60 py-1 ${compact ? 'text-[10px]' : 'text-xs'} text-slate-400`}>
        <span>卡<span className="ml-0.5 font-bold text-slate-100">{p.nCards}</span></span>
        <span>筹码<span className="ml-0.5 font-bold text-slate-100">{total(p.tokens)}</span>/10</span>
        {canInspect ? (
          <button type="button" className="min-h-[32px] rounded px-1 text-sky-300 hover:bg-slate-700 disabled:text-slate-500"
            aria-haspopup="dialog" aria-label={`查看${seatLabel(room, seat)}预留卡（${p.reserved.length}/3）`}
            disabled={p.reserved.length === 0} onClick={() => inspect()}>
            预留<span className="ml-0.5 font-bold">{p.reserved.length}</span>/3
          </button>
        ) : <span>预留<span className="ml-0.5 font-bold text-slate-100">{p.reserved.length}</span>/3</span>}
        {p.nobles.length > 0 && <span className="text-amber-300">贵族{p.nobles.length}</span>}
      </div>
      <div className="flex min-w-0 items-center gap-1 border-t border-slate-700/60 pt-1 text-xs text-slate-200" title={lastMove}
        aria-label={`${seatLabel(room, seat)}上一步：${lastMove ?? '尚未行动'}`}>
        <span className="shrink-0 text-[10px] text-slate-500">上一步</span>
        {moveEvents?.length ? <MoveIcons events={moveEvents} cards={visibleCards} compact={compact} onReserveClick={canInspect ? inspect : undefined} onBuyClick={inspectBought} /> : <span className="min-w-0 break-words">{lastMove ?? '—'}</span>}
      </div>
      {detail && (
        <PanelDialog title={`${seatLabel(room, seat)} · ${detail.kind === 'buy' ? '购买详情' : '预留详情'}`} onClose={() => setDetail(null)}>
          <div className="grid grid-cols-3 gap-2" aria-label={detail.kind === 'buy' ? '购买卡详情' : '预留卡详情'}>
            {(detail.card ? [detail.card] : p.reserved).map((c, i) => (
              <div key={i} className="h-32"><CardTile card={c} fluid /></div>
            ))}
          </div>
        </PanelDialog>
      )}
    </div>
  )
}

/** 预留详情弹层，支持 Esc、遮罩关闭及键盘焦点恢复。 */
export function PanelDialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const titleId = useId()
  const dialog = useRef<HTMLDivElement>(null)
  const close = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    const previous = document.activeElement
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    close.current?.focus()
    return () => {
      document.body.style.overflow = overflow
      if (previous instanceof HTMLElement) previous.focus()
    }
  }, [])
  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-3" onClick={onClose}>
      <div ref={dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}
        className="result-dialog flex w-full max-w-xl min-w-0 flex-col gap-3 overflow-y-auto rounded-xl border border-slate-600 bg-slate-900 p-3 sm:p-4"
        onClick={(e) => e.stopPropagation()} onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.stopPropagation()
            onClose()
          }
          if (e.key !== 'Tab') return
          const targets = dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], input, [tabindex="0"]')
          if (!targets?.length) return
          const first = targets[0]
          const last = targets[targets.length - 1]
          if (e.shiftKey && document.activeElement === first) {
            e.preventDefault()
            last.focus()
          } else if (!e.shiftKey && document.activeElement === last) {
            e.preventDefault()
            first.focus()
          }
        }}>
        <div className="flex shrink-0 items-center justify-between gap-2">
          <h2 id={titleId} className="font-bold">{title}</h2>
          <button ref={close} className="btn text-sm" onClick={onClose} aria-label={`关闭${title}`}>关闭</button>
        </div>
        {children}
      </div>
    </div>
  )
}

/** 日志与玩家面板的“上一步”共用图标渲染；盲扣牌在服务端按视角隐藏，不会泄露身份。 */
export function EventLog({ log, seatName, className = 'h-64 xl:h-48' }: {
  log: GameEvent[]
  seatName?: (seat: number) => string
  className?: string
}) {
  const cards = useCardCatalog()
  return (
    <div
      className={`${className} min-w-0 overflow-y-auto break-words text-sm xl:text-xs space-y-1 bg-slate-950/60 rounded p-2 flex flex-col-reverse`}
    >
      <div className="space-y-1">
        {log.length === 0 && <div className="text-slate-500">还没有行动</div>}
        {log.map((e, i) => e.kind === 'game_over' ? (
          <div key={i} className="font-semibold text-amber-300" title={e.text}>
            {e.text || '对局结束'}
          </div>
        ) : (
          <div key={i} className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-0.5" title={e.text}>
            <span className="shrink-0 font-semibold text-sky-300">{seatName?.(e.seat) ?? `玩家${e.seat + 1}`}</span>
            <MoveIcons events={[e]} cards={cards} />
          </div>
        ))}
      </div>
    </div>
  )
}

export function Chat({
  chat,
  onSend,
  className = 'h-48 xl:h-24',
}: {
  chat: ChatMessage[]
  onSend: (t: string) => void
  className?: string
}) {
  const [text, setText] = useState('')
  const list = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (list.current) list.current.scrollTop = list.current.scrollHeight
  }, [chat.length])
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-1">
      <div ref={list} className={`${className} min-h-0 overflow-y-auto break-words text-sm xl:text-xs bg-slate-950/60 rounded p-2`}>
        {chat.length === 0 && <div className="text-slate-500">还没有消息</div>}
        {chat.map((m, i) => (
          <div key={i}>
            <b className="text-sky-300">{m.from}</b>：{m.text}
          </div>
        ))}
      </div>
      <form
        className="flex gap-1"
        onSubmit={(e) => {
          e.preventDefault()
          if (text.trim()) onSend(text.trim())
          setText('')
        }}
      >
        <input
          aria-label="聊天内容"
          className="input min-w-0 flex-1 text-base sm:text-xs"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="聊天…"
          maxLength={200}
        />
        <button className="btn text-xs">发送</button>
      </form>
    </div>
  )
}

export function WinCurve({ curve, selectedTurn, onSelect }: {
  curve: CurvePoint[]
  selectedTurn?: number
  onSelect?: (index: number) => void
}) {
  const [hover, setHover] = useState<number | null>(null)
  if (curve.length === 0) return <div className="text-xs text-slate-500">暂无胜率数据</div>
  const w = 420
  const h = 160
  const first = curve[0].turn
  const last = curve[curve.length - 1].turn
  const x = (i: number) => first === last ? 225 : 40 + ((curve[i].turn - first) / (last - first)) * 365
  const y = (v: number) => 22 + (1 - v) * 112
  const selected = selectedTurn === undefined ? -1 : curve.findIndex((p) => p.turn === selectedTurn)
  const active = Math.min(hover ?? (selected >= 0 ? selected : curve.length - 1), curve.length - 1)
  const point = curve[active]
  const line = (k: 'winRate' | 'best') => curve.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p[k]).toFixed(1)}`).join(' ')
  const nearest = (clientX: number, rect: DOMRect) => {
    const px = ((clientX - rect.left) / rect.width) * w
    let best = 0
    curve.forEach((_, i) => {
      if (Math.abs(x(i) - px) < Math.abs(x(best) - px)) best = i
    })
    return best
  }
  return (
    <div className="space-y-1">
      <div className="text-[11px] text-slate-400">胜率（实线：实际着法；虚线：最佳着法；点击数据点跳转回合）</div>
      <svg
        aria-label="胜率曲线，移动鼠标或聚焦数据点查看百分比，点击或按回车跳转到该回合"
        viewBox={`0 0 ${w} ${h}`}
        className={`w-full h-auto bg-slate-950/60 rounded ${onSelect ? 'cursor-pointer' : ''}`}
        onPointerMove={(e) => setHover(nearest(e.clientX, e.currentTarget.getBoundingClientRect()))}
        onPointerLeave={() => setHover(null)}
        onClick={onSelect ? (e) => onSelect(nearest(e.clientX, e.currentTarget.getBoundingClientRect())) : undefined}
      >
        {[0, 0.5, 1].map((v) => (
          <g key={v}>
            <line x1={40} x2={405} y1={y(v)} y2={y(v)} stroke="#475569" strokeDasharray="4 4" />
            <text x={35} y={y(v) + 4} textAnchor="end" fill="#94a3b8" fontSize={11}>{v * 100}%</text>
          </g>
        ))}
        <path d={line('best')} fill="none" stroke="#a78bfa" strokeWidth={1.5} strokeDasharray="3 2" />
        <path d={line('winRate')} fill="none" stroke="#f0abfc" strokeWidth={2} />
        <line x1={x(active)} x2={x(active)} y1={22} y2={134} stroke="#cbd5e1" strokeDasharray="2 3" />
        {curve.map((p, i) => {
          const label = `第 ${p.turn + 1} 回合：实际 ${pct(p.winRate)}，最佳 ${pct(p.best)}`
          const select = onSelect ? () => onSelect(i) : undefined
          return (
            <g key={p.turn}>
              <circle
                tabIndex={0}
                role={select ? 'button' : undefined}
                aria-label={label}
                onFocus={() => setHover(i)}
                onBlur={() => setHover(null)}
                onKeyDown={select ? (e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    select()
                  }
                } : undefined}
                cx={x(i)} cy={y(p.winRate)} r={i === active ? 4 : 2.5}
                fill={p.best - p.winRate > 0.15 ? '#f43f5e' : p.best - p.winRate > 0.05 ? '#f59e0b' : '#f0abfc'}
                stroke={i === selected ? '#fde047' : undefined}
                strokeWidth={i === selected ? 1.5 : undefined}
              >
                <title>{label}</title>
              </circle>
            </g>
          )
        })}
        <text x={40} y={153} fill="#94a3b8" fontSize={11}>第 {first + 1} 回合</text>
        {last !== first && <text x={405} y={153} textAnchor="end" fill="#94a3b8" fontSize={11}>第 {last + 1} 回合</text>}
      </svg>
      <div role="status" className="text-xs text-fuchsia-200 font-mono">
        第 {point.turn + 1} 回合 · 实际 {pct(point.winRate)} · 最佳 {pct(point.best)}
      </div>
    </div>
  )
}

export function ReviewSummary({ reviews }: { reviews: MoveReview[] }) {
  const count = (v: string) => reviews.filter((r) => r.verdict === v).length
  return (
    <div className="text-sm flex flex-wrap gap-x-3 gap-y-1">
      <span className="text-emerald-400">最佳 {count('best')}</span>
      <span className="text-sky-400">良好 {count('good')}</span>
      <span className="text-amber-400">失误 {count('mistake')}</span>
      <span className="text-rose-400">严重失误 {count('blunder')}</span>
    </div>
  )
}

export function GameOverModal({
  over,
  room,
  onClose,
  onReplay,
}: {
  over: GameOver
  room: RoomState | null
  onClose: () => void
  onReplay: () => void
}) {
  const bad = over.reviews.filter((r) => r.verdict === 'mistake' || r.verdict === 'blunder')
  const dialog = useRef<HTMLDivElement>(null)
  const closeButton = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    const previousFocus = document.activeElement
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    closeButton.current?.focus()
    return () => {
      document.body.style.overflow = previousOverflow
      if (previousFocus instanceof HTMLElement) previousFocus.focus()
    }
  }, [])
  return (
    <div className="fixed inset-0 bg-black/70 flex items-center justify-center p-3 sm:p-4 z-50">
      <div
        ref={dialog}
        role="dialog"
        aria-modal="true"
        aria-labelledby="result-title"
        className="result-dialog w-full max-w-[560px] min-w-0 bg-slate-900 border border-slate-600 rounded-xl p-4 sm:p-6 overflow-y-auto overscroll-contain break-words space-y-3"
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.stopPropagation()
            onClose()
          }
          if (e.key !== 'Tab') return
          const targets = dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], [tabindex="0"]')
          if (!targets?.length) return
          const first = targets[0]
          const last = targets[targets.length - 1]
          if (e.shiftKey && document.activeElement === first) {
            e.preventDefault()
            last.focus()
          } else if (!e.shiftKey && document.activeElement === last) {
            e.preventDefault()
            first.focus()
          }
        }}
      >
        <div className="flex items-center justify-between gap-2">
          <h2 id="result-title" className="text-2xl font-bold">
            对局结束
          </h2>
          <button ref={closeButton} className="btn" onClick={onClose} aria-label="关闭对局结果">
            关闭
          </button>
        </div>
        <div>
          胜者：<b className="text-yellow-300">{over.winners.map((s) => seatLabel(room, s)).join('、')}</b>
        </div>
        <table className="w-full table-fixed text-sm">
          <thead>
            <tr className="text-slate-400">
              <th className="text-left">玩家</th>
              <th>分数</th>
              <th>发展卡</th>
            </tr>
          </thead>
          <tbody>
            {over.points.map((p, i) => (
              <tr key={i}>
                <td className="break-words">{seatLabel(room, i)}</td>
                <td className="text-center">{p}</td>
                <td className="text-center">{over.cards[i]}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {over.reviews.length > 0 && (
          <div className="space-y-2">
            <h3 className="font-semibold">全局复盘</h3>
            <ReviewSummary reviews={over.reviews} />
            <WinCurve curve={over.curve} />
            {bad.slice(0, 6).map((r, i) => (
              <ReviewBadge key={i} r={r} />
            ))}
          </div>
        )}
        <div className="flex flex-wrap gap-2 justify-end">
          <button className="btn" onClick={onReplay}>
            逐步复盘
          </button>
          <button className="btn-primary" onClick={onClose}>
            收起结果
          </button>
        </div>
        <div className="text-xs text-slate-500">
          胜率为大师 AI 的估计值：{over.curve.length ? pct(over.curve[over.curve.length - 1].winRate) : '-'}（最后一手）
        </div>
      </div>
    </div>
  )
}
