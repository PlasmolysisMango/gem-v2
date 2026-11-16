import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import type { RoomState, SeatInfo } from '../api/types'
import { GameBoard, seatLabel } from '../components/GameBoard'
import { HintPanel } from '../components/HintPanel'
import { Chat, EventLog, GameOverModal, PlayerPanel, ReviewSummary } from '../components/Panels'
import { useAuth } from '../store/auth'
import { useGame } from '../store/game'
import { LEVEL_NAME } from './Lobby'

export default function Room() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const g = useGame()
  const user = useAuth((s) => s.user)
  const [focus, setFocus] = useState(0)
  const [showOver, setShowOver] = useState(true)
  const [mobilePanel, setMobilePanel] = useState<'board' | 'players' | 'hint' | 'log' | 'chat'>('board')

  useEffect(() => setMobilePanel('board'), [id])
  useEffect(() => {
    const v = g.view
    if (v && v.current === v.viewer && (v.phase === 'discard' || v.phase === 'noble')) setMobilePanel('board')
  }, [g.view?.phase, g.view?.current, g.view?.viewer])

  useEffect(() => {
    g.joinRoom(id.toUpperCase())
    return () => {
      const st = useGame.getState().room?.status
      if (st !== 'playing') useGame.getState().leaveRoom()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])

  useEffect(() => setFocus(0), [g.hint])
  useEffect(() => setShowOver(true), [g.gameOver])
  useEffect(() => {
    if (!g.error) return
    const t = setTimeout(g.clearError, 4000)
    return () => clearTimeout(t)
  }, [g.error, g.clearError])

  const room = g.room
  const leave = () => {
    g.leaveRoom()
    nav('/')
  }

  const toast = g.error && (
    <div role="alert" className="flex items-start gap-2 bg-rose-900 text-white p-3 rounded break-words">
      <span className="min-w-0 flex-1">{g.error}</span>
      <button className="btn shrink-0 text-sm" onClick={g.clearError} aria-label="关闭错误提示">
        关闭
      </button>
    </div>
  )

  if (!room) {
    return (
      <div className="text-center mt-20 text-slate-400 space-y-3">
        {toast}
        <div>{g.connected ? `正在进入房间 ${id}…` : '正在连接服务器…'}</div>
        <button className="btn" onClick={leave}>
          返回大厅
        </button>
      </div>
    )
  }

  if (room.status === 'waiting' || !g.view) {
    return (
      <>
        {toast}
        <WaitingRoom room={room} isOwner={user?.id === room.ownerId} onLeave={leave} />
      </>
    )
  }

  const view = g.view
  const myTurn = view.viewer >= 0 && view.current === view.viewer && view.phase !== 'over'
  const mySeat = view.viewer >= 0 ? room.seats[view.viewer] : undefined
  const hintAction = g.hint?.candidates[focus]?.action
  const order =
    view.viewer >= 0 ? [...Array(view.nPlayers).keys()].map((i) => (i + view.viewer) % view.nPlayers) : [...Array(view.nPlayers).keys()]

  return (
    <div className="max-w-[1500px] mx-auto min-w-0 space-y-3">
      {toast}
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="w-full sm:w-auto min-w-0 break-words font-bold text-lg">{room.name}</span>
        <span className="font-mono text-slate-400">#{room.id}</span>
        <span>第 {view.round} 轮</span>
        {view.lastRound && <span className="text-amber-300 font-semibold">最后一轮！</span>}
        <TurnBanner room={room} current={view.current} myTurn={myTurn} phase={view.phase} />
        {room.deadline ? <Countdown deadline={room.deadline} /> : null}
        <button className="btn ml-auto text-xs" onClick={leave}>
          {view.phase === 'over' || view.viewer < 0 ? '离开' : '认输离开（AI 托管）'}
        </button>
      </div>
      <nav aria-label="对局分区" className={`grid ${view.viewer >= 0 ? 'grid-cols-5' : 'grid-cols-4'} gap-1 xl:hidden`}>
        {(
          [
            ['board', '棋盘'],
            ['players', '玩家'],
            ['hint', '提示'],
            ['log', '日志'],
            ['chat', '聊天'],
          ] as const
        )
          .filter(([key]) => key !== 'hint' || view.viewer >= 0)
          .map(([key, label]) => (
            <button
              key={key}
              className={`${mobilePanel === key ? 'btn-primary' : 'btn'} px-1 text-sm`}
              aria-pressed={mobilePanel === key}
              aria-controls={`room-${key}`}
              onClick={() => setMobilePanel(key)}
            >
              {label}
            </button>
          ))}
      </nav>
      <div className="grid xl:grid-cols-[minmax(0,1fr)_420px] gap-4">
        <section id="room-board" className={`min-w-0 space-y-3 ${mobilePanel === 'board' ? '' : 'hidden xl:block'}`}>
          <GameBoard view={view} room={room} onAct={view.viewer >= 0 && !mySeat?.bot ? g.act : undefined} hintAction={hintAction} />
          {g.gameOver && !showOver && (
            <button className="btn-primary" onClick={() => setShowOver(true)}>
              查看对局结果
            </button>
          )}
        </section>
        <aside className={`min-w-0 space-y-3 ${mobilePanel === 'board' ? 'hidden xl:block' : ''}`}>
          <div
            id="room-players"
            className={`grid grid-cols-1 min-[400px]:grid-cols-2 gap-2 ${mobilePanel === 'players' ? '' : 'hidden xl:grid'}`}
          >
            {order.map((s) => (
              <PlayerPanel key={s} view={view} room={room} seat={s} />
            ))}
          </div>
          {view.viewer >= 0 && (
            <div id="room-hint" className={`card ${mobilePanel === 'hint' ? '' : 'hidden xl:block'}`}>
              <HintPanel
                enabled={room.hints}
                myTurn={myTurn && view.phase === 'main' && !mySeat?.bot}
                loading={g.hintLoading}
                hint={g.hint}
                onRequest={g.requestHint}
                onFocus={(i) => {
                  setFocus(i)
                  setMobilePanel('board')
                }}
                focus={focus}
                lastReview={g.lastReview}
                seatName={(s) => seatLabel(room, s)}
              />
              {room.hints && g.reviews.length > 0 && (
                <div className="mt-2">
                  <ReviewSummary reviews={g.reviews} />
                </div>
              )}
            </div>
          )}
          <div id="room-log" className={mobilePanel === 'log' ? '' : 'hidden xl:block'}>
            <h3 className="text-sm text-slate-400 mb-2">操作日志</h3>
            <EventLog log={g.log} />
          </div>
          <div id="room-chat" className={mobilePanel === 'chat' ? '' : 'hidden xl:block'}>
            <h3 className="text-sm text-slate-400 mb-2">房间聊天</h3>
            <Chat chat={g.chat} onSend={g.sendChat} />
          </div>
        </aside>
      </div>
      {g.gameOver && showOver && (
        <GameOverModal
          over={g.gameOver}
          room={room}
          onClose={() => setShowOver(false)}
          onReplay={() => nav(`/games/${g.gameOver!.gameId}`)}
        />
      )}
    </div>
  )
}

function TurnBanner({ room, current, myTurn, phase }: { room: RoomState; current: number; myTurn: boolean; phase: string }) {
  if (phase === 'over') return <span className="text-yellow-300 font-semibold">对局结束</span>
  if (myTurn) {
    const t = phase === 'discard' ? '请归还多余筹码' : phase === 'noble' ? '请选择贵族' : '轮到你行动'
    return <span className="px-2 py-0.5 rounded bg-yellow-500 text-slate-900 font-bold animate-pulse">{t}</span>
  }
  const s = room.seats[current]
  return (
    <span className="text-slate-300">
      等待 {s?.name || `玩家${current + 1}`} {s?.kind === 'ai' || s?.bot ? '（AI 思考中…）' : ''}
    </span>
  )
}

function Countdown({ deadline }: { deadline: number }) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 500)
    return () => clearInterval(t)
  }, [])
  const left = Math.max(0, Math.ceil((deadline - now) / 1000))
  return <span className={`font-mono ${left <= 10 ? 'text-rose-400' : 'text-slate-300'}`}>⏱ {left}s</span>
}

function seatText(s: SeatInfo): string {
  switch (s.kind) {
    case 'human':
      return s.name + (s.connected ? '' : '（离线）')
    case 'ai':
      return `AI · ${LEVEL_NAME[s.level ?? 'medium']}`
    case 'closed':
      return '已关闭'
    default:
      return '空位'
  }
}

function WaitingRoom({ room, isOwner, onLeave }: { room: RoomState; isOwner: boolean; onLeave: () => void }) {
  const g = useGame()
  const players = room.seats.filter((s) => s.kind === 'human' || s.kind === 'ai').length
  return (
    <div className="max-w-xl mx-auto card space-y-4 mt-8">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="w-full sm:w-auto break-words text-xl font-bold">{room.name}</h2>
        <span className="font-mono text-slate-400">房间号 {room.id}</span>
        <span className={`ml-auto text-xs ${room.hints ? 'text-fuchsia-300' : 'text-slate-500'}`}>
          {room.hints ? '允许提示' : '禁止提示'}
        </span>
      </div>
      <div className="space-y-2">
        {room.seats.map((s, i) => (
          <div key={i} className="flex flex-wrap items-center gap-2 bg-slate-900/60 rounded p-2">
            <span className="w-14 shrink-0 text-slate-400">座位 {i + 1}</span>
            <span className="flex-1 min-w-0 break-words">
              {seatText(s)}
              {s.userId === room.ownerId && <span className="ml-1 text-xs text-amber-300">房主</span>}
              {i === room.yourSeat && <span className="ml-1 text-xs text-sky-300">(你)</span>}
            </span>
            {isOwner && s.userId !== room.ownerId && !(s.kind === 'human' && s.connected) && (
              <select
                className="input w-full sm:w-auto py-0.5 text-sm"
                value={s.kind === 'ai' ? `ai:${s.level}` : s.kind}
                onChange={(e) => {
                  const v = e.target.value
                  if (v.startsWith('ai:')) g.setSeat(i, 'ai', v.slice(3))
                  else g.setSeat(i, v as 'open' | 'closed')
                }}
              >
                {s.kind === 'human' && <option value="human">{s.name}</option>}
                <option value="open">空位</option>
                <option value="closed">关闭</option>
                <option value="ai:easy">AI · 简单</option>
                <option value="ai:medium">AI · 中等</option>
                <option value="ai:hard">AI · 困难</option>
              </select>
            )}
          </div>
        ))}
      </div>
      <div className="text-xs text-slate-400">
        {room.turnSeconds > 0 ? `回合限时 ${room.turnSeconds} 秒，超时由 AI 代走。` : '不限回合时间。'}
        {room.spectators > 0 && ` 观战 ${room.spectators} 人。`} 把房间号分享给好友即可加入。
      </div>
      <div className="flex flex-wrap gap-2 justify-end">
        <button className="btn" onClick={onLeave}>
          离开
        </button>
        {isOwner ? (
          <button className="btn-primary" disabled={players < 2} onClick={g.start}>
            开始对局（{players} 人）
          </button>
        ) : (
          <span className="text-sm text-slate-400 self-center">等待房主开始…</span>
        )}
      </div>
    </div>
  )
}
