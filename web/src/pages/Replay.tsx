import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type Replay as ReplayData } from '../api/http'
import type { CurvePoint, MoveReview, RoomState } from '../api/types'
import { GameBoard } from '../components/GameBoard'
import { ReviewBadge } from '../components/HintPanel'
import { PlayerPanel, ReviewSummary, WinCurve } from '../components/Panels'
import { LEVEL_NAME } from './Lobby'

export default function Replay() {
  const { id = '' } = useParams()
  const [data, setData] = useState<ReplayData | null>(null)
  const [error, setError] = useState('')
  const [idx, setIdx] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [panel, setPanel] = useState<'board' | 'players' | 'review' | 'log'>('board')

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

  return (
    <div className="max-w-[1500px] mx-auto min-w-0 space-y-3">
      <nav className="grid grid-cols-4 gap-1 xl:hidden" aria-label="复盘分区">
        {(
          [
            ['board', '棋盘'],
            ['players', '玩家'],
            ['review', '评估'],
            ['log', '着法'],
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
      <div className="grid xl:grid-cols-[minmax(0,1fr)_420px] gap-4">
        <section id="replay-board" className={`min-w-0 space-y-3 ${panel === 'board' ? '' : 'hidden xl:block'}`}>
          <div className="flex items-center gap-3 flex-wrap">
            <h2 className="text-lg font-bold">对局复盘 #{data.id}</h2>
            <span className="text-xs text-slate-400">
              {new Date(data.startedAt * 1000).toLocaleString()} · 种子 {data.seed} · {data.status === 'aborted' ? '已中止' : '已结束'}
            </span>
            <Link to="/" className="btn text-xs ml-auto">
              返回大厅
            </Link>
          </div>
          <div className="card flex flex-wrap items-center gap-2">
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
          </div>
          <div className="text-sm min-h-[1.5rem]">{frame ? frame.text : '初始局面'}</div>
          {frame?.review && <ReviewBadge r={frame.review} />}
          <GameBoard view={view} room={room} />
        </section>
        <aside className={`min-w-0 space-y-3 ${panel === 'board' ? 'hidden xl:block' : ''}`}>
          <div
            id="replay-players"
            className={`grid grid-cols-1 min-[400px]:grid-cols-2 gap-2 ${panel === 'players' ? '' : 'hidden xl:grid'}`}
          >
            {view.players.map((p) => (
              <PlayerPanel key={p.seat} view={view} room={room} seat={p.seat} />
            ))}
          </div>
          <div id="replay-review" className={panel === 'review' ? '' : 'hidden xl:block'}>
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
            className={`card h-80 overflow-y-auto break-words text-sm xl:text-xs font-mono space-y-0.5 ${panel === 'log' ? '' : 'hidden xl:block'}`}
          >
            {data.frames.map((f, i) => (
              <div
                key={i}
                role="button"
                tabIndex={0}
                onClick={() => {
                  setIdx(i + 1)
                  setPanel('board')
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    setIdx(i + 1)
                    setPanel('board')
                  }
                }}
                className={`min-h-[44px] xl:min-h-0 cursor-pointer px-1 py-2 xl:py-0 rounded ${idx === i + 1 ? 'bg-sky-800' : 'hover:bg-slate-700'}`}
              >
                <span className="text-slate-500 mr-1">{i + 1}.</span>
                {f.text}
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
