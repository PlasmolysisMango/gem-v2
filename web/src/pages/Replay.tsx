import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type Replay as ReplayData } from '../api/http'
import type { CurvePoint, MoveReview, RoomState } from '../api/types'
import { GameBoard } from '../components/GameBoard'
import { HintPanel, ReviewBadge } from '../components/HintPanel'
import { PlayerPanel, ReviewSummary, WinCurve } from '../components/Panels'
import { LANDSCAPE_QUERY, frameMoveEvents, frameMoves } from '../lib/gems'
import { LEVEL_NAME } from './Lobby'

function ReplayControls({ idx, n, playing, review, onIndexChange, onPlayingChange }: {
  idx: number
  n: number
  playing: boolean
  review: MoveReview | null
  onIndexChange: (idx: number) => void
  onPlayingChange: (playing: boolean) => void
}) {
  const buttonSize = '!p-0 w-11 h-11 min-w-[44px] min-h-[44px] xl:w-8 xl:h-8 xl:min-w-[32px] xl:min-h-[32px] text-xs shrink-0'
  return (
    <div aria-label="复盘控制" role="group" className="card !p-2 space-y-1">
      <div className="flex justify-center gap-1.5">
        <button className={`btn ${buttonSize}`} aria-label="回到初始局面" disabled={idx === 0} onClick={() => onIndexChange(0)}>
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false"><path d="M3 5h2v14H3zM13 5v14l-7-7zM21 5v14l-7-7z" /></svg>
        </button>
        <button className={`btn ${buttonSize}`} aria-label="上一手" disabled={idx === 0} onClick={() => onIndexChange(Math.max(0, idx - 1))}>
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false"><path d="M16 5v14L6 12z" /></svg>
        </button>
        <button className={`btn-primary ${buttonSize}`} aria-label={playing ? '暂停' : '播放'} aria-pressed={playing} onClick={() => onPlayingChange(!playing)}>
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false"><path d={playing ? 'M6 5h4v14H6zM14 5h4v14h-4z' : 'M8 5v14l10-7z'} /></svg>
        </button>
        <button className={`btn ${buttonSize}`} aria-label="下一手" disabled={idx === n} onClick={() => onIndexChange(Math.min(n, idx + 1))}>
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false"><path d="M8 5v14l10-7z" /></svg>
        </button>
        <button className={`btn ${buttonSize}`} aria-label="跳到最后一手" disabled={idx === n} onClick={() => onIndexChange(n)}>
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false"><path d="M19 5h2v14h-2zM11 5v14l7-7zM3 5v14l7-7z" /></svg>
        </button>
      </div>
      <div className="flex items-center gap-2">
        <input aria-label="复盘进度" type="range" min={0} max={n} value={idx}
          onChange={(e) => onIndexChange(Number(e.target.value))}
          className="min-w-0 min-h-[44px] xl:min-h-[32px] flex-1" />
        <span className="shrink-0 font-mono text-xs">{idx}/{n}</span>
      </div>
      {review && <ReviewBadge r={review} />}
    </div>
  )
}

export default function Replay() {
  const { id = '' } = useParams()
  const [data, setData] = useState<ReplayData | null>(null)
  const [error, setError] = useState('')
  const [idx, setIdx] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [panel, setPanel] = useState<'review' | 'log'>('review')
  const [analysisError, setAnalysisError] = useState('')
  const [retry, setRetry] = useState(0)
  const [focus, setFocus] = useState(-1)
  const dataRef = useRef<ReplayData | null>(null)
  const [loadedGameId, setLoadedGameId] = useState<string | null>(null)
  const [analyzingSeq, setAnalyzingSeq] = useState<number | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    dataRef.current = null
    setData(null)
    setLoadedGameId(null)
    setIdx(0)
    setError('')
    setAnalysisError('')
    setAnalyzingSeq(null)
    setPlaying(false)
    setFocus(-1)
    const load = async () => {
      try {
        const loaded = await api.game(Number(id), controller.signal)
        if (controller.signal.aborted) return
        dataRef.current = loaded
        setData(loaded)
        setLoadedGameId(id)
      } catch (e) {
        if (!controller.signal.aborted) setError((e as Error).message)
      }
    }
    void load()
    return () => controller.abort()
  }, [id])

  useEffect(() => {
    const loaded = dataRef.current
    setAnalyzingSeq(null)
    if (!loaded || loadedGameId !== id) return
    const controller = new AbortController()
    setAnalysisError('')
    const missing = loaded.frames.filter((f) => f.main && !(f.analysis && f.review))
    const next = missing.find((f) => f.seq === loaded.frames[idx]?.seq)
    const queue = next ? [next, ...missing.filter((f) => f.seq !== next.seq)] : missing
    const generate = async () => {
      try {
        for (const frame of queue) {
          if (controller.signal.aborted) return
          const current = dataRef.current?.frames.find((f) => f.seq === frame.seq)
          if (!current || (current.analysis && current.review)) continue
          setAnalyzingSeq(frame.seq)
          const analyzed = await api.replayAnalysis(Number(id), frame.seq, controller.signal)
          if (controller.signal.aborted) return
          const latest = dataRef.current
          if (!latest) return
          const updated = { ...latest, frames: latest.frames.map((f) => f.seq === analyzed.seq ? analyzed : f) }
          dataRef.current = updated
          setData(updated)
        }
        setAnalyzingSeq(null)
      } catch (e) {
        if (!controller.signal.aborted) {
          setAnalyzingSeq(null)
          setAnalysisError((e as Error).message)
        }
      }
    }
    void generate()
    return () => controller.abort()
    // Analysis results update the ref, not this effect's dependencies.
  }, [id, idx, retry, loadedGameId])

  useEffect(() => setFocus(-1), [idx])

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
      arr.push({ turn: f.turn, winRate: f.review.playedWinRate, best: f.review.bestWinRate })
      m.set(f.seat, arr)
    })
    return m
  }, [data])

  if (error) return <div className="text-rose-400 text-center mt-20">{error}</div>
  if (!data || !room) return <div className="text-slate-400 text-center mt-20">加载中…</div>

  const frame = idx > 0 ? data.frames[idx - 1] : null
  const position = frame ? frame.view : data.initial
  const next = data.frames[idx]
  const hint = next?.main ? next.analysis ?? null : null
  const view = { ...position, viewer: next?.seat ?? frame?.seat ?? position.current }
  const analysisTotal = data.frames.filter((f) => f.main).length
  const analysisDone = data.frames.filter((f) => f.main && f.analysis && f.review).length
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
          <ReplayControls idx={idx} n={n} playing={playing} review={frame?.review ?? null} onIndexChange={setIdx} onPlayingChange={setPlaying} />
        </section>
        <section id="replay-board" aria-label="棋盘" className="min-w-0 scroll-mt-2 flex flex-col gap-2 xl:min-h-0">
          <div className="card !p-2 flex flex-wrap items-center gap-2 shrink-0">
            <div className="w-full text-sm break-words">
              {frame ? `${label(frame.seat)} ${frame.text}` : '初始局面'}
            </div>
          </div>
          <div className="min-w-0 xl:flex-1 xl:min-h-0 xl:overflow-y-auto">
            <GameBoard view={view} room={room} moves={moves} moveHistory={moveEvents} hintAction={hint?.candidates[focus]?.action}
              playerFooter={<ReplayControls idx={idx} n={n} playing={playing} review={frame?.review ?? null} onIndexChange={setIdx} onPlayingChange={setPlaying} />} />
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
          <div id="replay-review" className={`min-w-0 space-y-2 xl:max-h-[65%] xl:overflow-y-auto ${panel === 'review' ? '' : 'hidden xl:block'}`}>
            <div className="card space-y-2">
              <div role="status" className="text-xs text-slate-400">
                {analysisError ? `分析暂停：${analysisDone}/${analysisTotal}，已完成的分析已保存，请重试。` : analysisDone === analysisTotal ? `大师 AI 分析已完成：${analysisDone}/${analysisTotal}` : analyzingSeq !== null ? `大师 AI 实时补算中：${analysisDone}/${analysisTotal}，当前请求第 ${data.frames.findIndex((f) => f.seq === analyzingSeq) + 1} 手（排队或演算）；已完成的分析已保存。` : `大师 AI 分析待开始：${analysisDone}/${analysisTotal}。`}
              </div>
              {data.background && analysisDone < analysisTotal && <div className="text-xs text-slate-500">
                打开时后台：{({ pending: '排队', running: '演算中', paused: '暂停（有进行中对局或实时请求）', complete: '已完成', failed: '失败，需重试', none: '未入闲时队列' } as Record<string, string>)[data.background.state] ?? data.background.state}；进入复盘后优先实时演算。
                {data.background.error && ` ${data.background.error}`}
              </div>}
              {analysisError && <div className="text-xs text-rose-400">{analysisError} <button className="btn" onClick={() => setRetry((r) => r + 1)}>重试分析</button></div>}
              <h3 className="font-semibold">{next ? `${label(next.seat)} · 行动前 AI 提示` : '已到对局末尾'}</h3>
              {next?.main ? <HintPanel
                replay enabled myTurn={false} loading={!hint && !analysisError && analyzingSeq === next.seq} hint={hint}
                onRequest={() => {}} onFocus={setFocus} focus={focus} lastReview={null} seatName={label}
              /> : next ? <div className="text-xs text-slate-400">归还宝石或选择贵族阶段；主行动提示在前一手局面查看。</div> : null}
            </div>
            {reviews.length > 0 ? (
              <div className="card space-y-2">
                <h3 className="font-semibold">走后复盘（大师 AI 评估）</h3>
                <p className="text-xs text-slate-400">胜率为 AI 估计值，用于比较着法，并非真实胜率。</p>
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
