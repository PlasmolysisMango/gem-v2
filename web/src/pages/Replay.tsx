import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type Replay as ReplayData } from '../api/http'
import type { CurvePoint, MoveReview, RoomState } from '../api/types'
import { GameBoard } from '../components/GameBoard'
import { ReviewBadge } from '../components/HintPanel'
import { PlayerPanel, ReviewSummary, WinCurve } from '../components/Panels'
import { LANDSCAPE_QUERY, frameMoveEvents, frameMoves } from '../lib/gems'
import { LEVEL_NAME } from './Lobby'

export default function Replay() {
  const { id = '' } = useParams()
  const [data, setData] = useState<ReplayData | null>(null)
  const [error, setError] = useState('')
  const [idx, setIdx] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [panel, setPanel] = useState<'review' | 'log'>('log')

  useEffect(() => {
    api
      .game(Number(id))
      .then((d) => {
        setData(d)
        setIdx(0)
      })
      .catch((e) => setError(e.message))
  }, [id])

  const n = data?.frames.length ?? 0
  useEffect(() => {
    if (!playing) return
    const t = setInterval(() => setIdx((i) => (i < n ? i + 1 : i)), 800)
    return () => clearInterval(t)
  }, [playing, n])
  useEffect(() => {
    if (idx >= n) setPlaying(false)
  }, [idx, n])

  // a synthetic room so the board components can label seats
  const room = useMemo<RoomState | null>(() => {
    if (!data) return null
    return {
      id: data.roomId,
      name: '',
      ownerId: 0,
      ownerName: '',
      status: 'finished',
      hints: false,
      turnSeconds: 0,
      yourSeat: -1,
      spectators: 0,
      seats: data.players.map((p) => ({
        index: p.seat,
        kind: p.userId ? 'human' : 'ai',
        userId: p.userId ?? undefined,
        name: p.username || (p.aiLevel ? `AI·${LEVEL_NAME[p.aiLevel]}` : `玩家${p.seat + 1}`),
        level: p.aiLevel,
        connected: true,
      })),
    }
  }, [data])

  const reviews = useMemo(() => (data?.frames.map((f) => f.review).filter(Boolean) as MoveReview[]) ?? [], [data])
  const curveBySeat = useMemo(() => {
    const m = new Map<number, CurvePoint[]>()
    data?.frames.forEach((f) => {
      if (!f.review) return
      const arr = m.get(f.seat) ?? []
      arr.push({ turn: f.view.turn, winRate: f.review.playedWinRate, best: f.review.bestWinRate })
      m.set(f.seat, arr)
    })
    return m
  }, [data])

  if (error) return <div className="text-rose-400 text-center mt-20">{error}</div>
  if (!data || !room) return <div className="text-slate-400 text-center mt-20">加载中…</div>

  const frame = idx > 0 ? data.frames[idx - 1] : null
  const view = frame ? frame.view : data.initial
  const label = (s: number) => room.seats[s]?.name ?? `玩家${s + 1}`
  const moves = frameMoves(data.frames.slice(0, idx))
  const moveEvents = frameMoveEvents(data.frames.slice(0, idx))
  const jump = (i: number) => {
    setIdx(i)
    if (!window.matchMedia(LANDSCAPE_QUERY).matches) {
      document.getElementById('replay-board')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    }
  }

  // Same arrangement as the room: players | board | review and moves on desktop, one page on phones.
  return (
    <div className="mx-auto min-w-0 max-w-[2400px] flex flex-col gap-2 sm:gap-3 xl:h-full">
      <div className="flex items-center gap-x-3 gap-y-1 flex-wrap shrink-0">
        <h2 className="sm:text-lg font-bold">对局复盘 #{data.id}</h2>
        <span className="text-xs text-slate-400">
          {new Date(data.startedAt * 1000).toLocaleString()} · 种子 {data.seed} · {data.status === 'aborted' ? '已中止' : '已结束'}
        </span>
        <Link to="/" className="btn text-xs ml-auto">
          返回大厅
        </Link>
      </div>
      <div className="min-w-0 flex flex-col gap-2 sm:gap-3 xl:flex-1 xl:min-h-0 xl:grid xl:grid-cols-[clamp(14rem,17vw,21rem)_minmax(0,1fr)_clamp(16rem,20vw,26rem)] xl:gap-3">
        <section
          aria-label="玩家"
          className="hidden xl:grid xl:grid-cols-1 xl:content-start xl:gap-2 xl:min-h-0 xl:overflow-y-auto xl:pr-1"
        >
          {view.players.map((p) => (
            <PlayerPanel key={p.seat} view={view} room={room} seat={p.seat} lastMove={moves.get(p.seat)} moveEvents={moveEvents.get(p.seat)} />
          ))}
        </section>
        <section id="replay-board" aria-label="棋盘" className="min-w-0 scroll-mt-2 flex flex-col gap-2 xl:min-h-0">
          <div className="card !p-2 flex flex-wrap items-center gap-2 shrink-0">
            <button className="btn" aria-label="回到初始局面" disabled={idx === 0} onClick={() => setIdx(0)}>
              ⏮
            </button>
            <button className="btn" aria-label="上一手" disabled={idx === 0} onClick={() => setIdx(Math.max(0, idx - 1))}>
              ◀
            </button>
            <button className="btn-primary w-16" onClick={() => setPlaying(!playing)}>
              {playing ? '暂停' : '播放'}
            </button>
            <button className="btn" aria-label="下一手" disabled={idx === n} onClick={() => setIdx(Math.min(n, idx + 1))}>
              ▶
            </button>
            <button className="btn" aria-label="跳到最后一手" disabled={idx === n} onClick={() => setIdx(n)}>
              ⏭
            </button>
            <div className="w-full sm:contents flex items-center gap-2">
              <input
                aria-label="复盘进度"
                type="range"
                min={0}
                max={n}
                value={idx}
                onChange={(e) => setIdx(Number(e.target.value))}
                className="min-w-0 min-h-[44px] flex-1"
              />
              <span className="shrink-0 font-mono text-sm w-20 text-right">
                {idx}/{n}
              </span>
            </div>
            <div className="w-full text-sm break-words">
              {frame ? `${label(frame.seat)} ${frame.text}` : '初始局面'}
            </div>
            {frame?.review && (
              <div className="w-full">
                <ReviewBadge r={frame.review} />
              </div>
            )}
          </div>
          <div className="min-w-0 xl:flex-1 xl:min-h-0 xl:overflow-y-auto">
            <GameBoard view={view} room={room} moves={moves} moveHistory={moveEvents} />
          </div>
        </section>
        <aside className="min-w-0 flex flex-col gap-2 xl:min-h-0 xl:gap-3">
          <nav className="grid grid-cols-2 gap-1 xl:hidden" aria-label="复盘分区">
            {(
              [
                ['log', '着法'],
                ['review', '评估'],
              ] as const
            ).map(([key, text]) => (
              <button
                key={key}
                className={`${panel === key ? 'btn-primary' : 'btn'} px-1 text-sm`}
                aria-pressed={panel === key}
                aria-controls={`replay-${key}`}
                onClick={() => setPanel(key)}
              >
                {text}
              </button>
            ))}
          </nav>
          <div id="replay-review" className={`min-w-0 xl:max-h-[45%] xl:overflow-y-auto ${panel === 'review' ? '' : 'hidden xl:block'}`}>
            {reviews.length > 0 ? (
              <div className="card space-y-2">
                <h3 className="font-semibold">走后复盘（困难 AI 评估）</h3>
                {[...curveBySeat.entries()].map(([seat, curve]) => (
                  <div key={seat} className="space-y-1">
                    <div className="text-sm">{label(seat)}</div>
                    <ReviewSummary reviews={reviews.filter((r) => r.seat === seat)} />
                    <WinCurve curve={curve} />
                  </div>
                ))}
              </div>
            ) : (
              <div className="card text-sm text-slate-400">本局没有走后复盘数据。</div>
            )}
          </div>
          <div
            id="replay-log"
            className={`card h-72 xl:h-auto xl:flex-1 xl:min-h-0 overflow-y-auto break-words text-sm xl:text-xs font-mono space-y-0.5 ${panel === 'log' ? '' : 'hidden xl:block'}`}
          >
            {data.frames.map((f, i) => (
              <div
                key={i}
                role="button"
                tabIndex={0}
                onClick={() => jump(i + 1)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    jump(i + 1)
                  }
                }}
                className={`min-h-[44px] xl:min-h-0 cursor-pointer px-1 py-2 xl:py-0 rounded ${idx === i + 1 ? 'bg-sky-800' : 'hover:bg-slate-700'}`}
              >
                <span className="text-slate-500 mr-1">{i + 1}.</span>
                <span className="text-sky-300">{label(f.seat)}</span> {f.text}
                {f.review && f.review.verdict !== 'best' && f.review.verdict !== 'good' && (
                  <span className={f.review.verdict === 'blunder' ? 'text-rose-400' : 'text-amber-400'}> [{f.review.verdictText}]</span>
                )}
              </div>
            ))}
          </div>
        </aside>
      </div>
    </div>
  )
}
