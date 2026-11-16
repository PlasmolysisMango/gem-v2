import { useEffect, useRef, useState } from 'react'
import type { ChatMessage, CurvePoint, GameEvent, GameOver, MoveReview, RoomState, View } from '../api/types'
import { pct, total } from '../lib/gems'
import { seatLabel } from './GameBoard'
import { ReviewBadge } from './HintPanel'
import { CardTile, NobleTile, TokenRow } from './Pieces'

export function PlayerPanel({ view, room, seat }: { view: View; room: RoomState | null; seat: number }) {
  const p = view.players[seat]
  const info = room?.seats[seat]
  const active = view.current === seat && view.phase !== 'over'
  const winner = view.winners.includes(seat)
  return (
    <div className={`rounded-lg p-2 border-2 ${active ? 'border-yellow-400 bg-slate-800' : 'border-slate-700 bg-slate-800/50'}`}>
      <div className="flex justify-between items-start gap-2">
        <span className="min-w-0 break-words font-semibold">
          {seatLabel(room, seat)}
          {seat === view.viewer && <span className="text-xs text-sky-300"> (你)</span>}
          {info?.bot && <span className="text-xs text-amber-300"> 托管中</span>}
          {info && info.kind === 'human' && !info.connected && <span className="text-xs text-rose-300"> 离线</span>}
          {winner && <span className="text-xs text-yellow-300"> 🏆</span>}
        </span>
        <span className="shrink-0 text-2xl font-black text-yellow-300">{p.points}</span>
      </div>
      <TokenRow tokens={p.tokens} bonuses={p.bonuses} />
      <div className="text-xs text-slate-400 mt-1">
        筹码 {total(p.tokens)} · 卡 {p.nCards} · 预留 {p.reserved.length}
      </div>
      {seat !== view.viewer && p.reserved.length > 0 && (
        <div className="flex flex-wrap gap-1 mt-1">
          {p.reserved.map((c, i) => (
            <CardTile key={i} card={c} small />
          ))}
        </div>
      )}
      {p.nobles.length > 0 && (
        <div className="flex flex-wrap gap-1 mt-1">
          {p.nobles.map((id) => {
            const n = { id, points: 3, req: [0, 0, 0, 0, 0] }
            return <NobleTile key={id} noble={n} small />
          })}
        </div>
      )}
    </div>
  )
}

export function EventLog({ log }: { log: GameEvent[] }) {
  return (
    <div className="h-64 xl:h-48 min-w-0 overflow-y-auto break-words text-sm xl:text-xs space-y-0.5 font-mono bg-slate-950/60 rounded p-2 flex flex-col-reverse">
      <div>
        {log.map((e, i) => (
          <div key={i} className={e.kind === 'noble' ? 'text-amber-300' : ''}>
            {e.text}
          </div>
        ))}
      </div>
    </div>
  )
}

export function Chat({ chat, onSend }: { chat: ChatMessage[]; onSend: (t: string) => void }) {
  const [text, setText] = useState('')
  return (
    <div className="space-y-1">
      <div className="h-48 xl:h-24 overflow-y-auto break-words text-sm xl:text-xs bg-slate-950/60 rounded p-2">
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

export function WinCurve({ curve }: { curve: CurvePoint[] }) {
  if (curve.length < 2) return <div className="text-xs text-slate-500">样本不足，无法绘制胜率曲线</div>
  const w = 420
  const h = 140
  const x = (i: number) => 20 + (i / (curve.length - 1)) * (w - 30)
  const y = (v: number) => 10 + (1 - v) * (h - 20)
  const line = (k: 'winRate' | 'best') => curve.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p[k]).toFixed(1)}`).join(' ')
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="w-full h-auto bg-slate-950/60 rounded">
      <line x1={20} x2={w - 10} y1={y(0.5)} y2={y(0.5)} stroke="#475569" strokeDasharray="4 4" />
      <path d={line('best')} fill="none" stroke="#a78bfa" strokeWidth={1.5} strokeDasharray="3 2" />
      <path d={line('winRate')} fill="none" stroke="#f0abfc" strokeWidth={2} />
      {curve.map((p, i) => (
        <circle
          key={i}
          cx={x(i)}
          cy={y(p.winRate)}
          r={2.5}
          fill={p.best - p.winRate > 0.15 ? '#f43f5e' : p.best - p.winRate > 0.05 ? '#f59e0b' : '#f0abfc'}
        />
      ))}
      <text x={22} y={12} fill="#94a3b8" fontSize={10}>
        胜率（实线：你的着法；虚线：最佳着法）
      </text>
    </svg>
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
          胜率为困难 AI 的估计值：{over.curve.length ? pct(over.curve[over.curve.length - 1].winRate) : '-'}（最后一手）
        </div>
      </div>
    </div>
  )
}
