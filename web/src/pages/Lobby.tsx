import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../api/http'
import type { AILevel, RoomSummary } from '../api/types'

export const LEVEL_NAME: Record<AILevel, string> = { easy: '简单', medium: '中等', hard: '困难' }
const STATUS_NAME: Record<string, string> = { waiting: '等待中', playing: '对局中', finished: '已结束' }

type SeatChoice = 'human' | AILevel

export default function Lobby() {
  const nav = useNavigate()
  const [rooms, setRooms] = useState<RoomSummary[]>([])
  const [error, setError] = useState('')
  const [name, setName] = useState('')
  const [seats, setSeats] = useState(2)
  const [others, setOthers] = useState<SeatChoice[]>(['hard', 'medium', 'easy'])
  const [hints, setHints] = useState<'auto' | 'on' | 'off'>('auto')
  const [turnSeconds, setTurnSeconds] = useState(0)
  const [joinId, setJoinId] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let alive = true
    const load = () =>
      api
        .rooms()
        .then((r) => alive && setRooms(r))
        .catch((e) => alive && setError(e.message))
    load()
    const t = setInterval(load, 3000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])

  const create = async (preset?: { ai: AILevel[] }) => {
    setBusy(true)
    setError('')
    try {
      const chosen = others.slice(0, seats - 1)
      const ai = preset ? preset.ai : (chosen.filter((c) => c !== 'human') as AILevel[])
      const room = await api.createRoom({
        name: preset ? `练习：对战${ai.map((l) => LEVEL_NAME[l]).join('、')} AI` : name,
        seats: preset ? ai.length + 1 : seats,
        ai,
        hints: preset ? true : hints === 'auto' ? undefined : hints === 'on',
        turnSeconds: preset ? 0 : turnSeconds,
      })
      nav(`/room/${room.id}`)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="max-w-6xl mx-auto min-w-0 grid lg:grid-cols-[minmax(0,1fr)_380px] gap-4">
      <section className="card space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-xl font-bold">房间列表</h2>
          <form
            className="w-full sm:w-auto sm:ml-auto flex gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (joinId.trim()) nav(`/room/${joinId.trim().toUpperCase()}`)
            }}
          >
            <input
              aria-label="房间号"
              className="input flex-1 sm:flex-none sm:w-28 uppercase"
              placeholder="房间号"
              value={joinId}
              onChange={(e) => setJoinId(e.target.value)}
            />
            <button className="btn">加入</button>
          </form>
        </div>
        {rooms.length === 0 && <div className="text-slate-400 text-sm">暂无房间，创建一个吧。</div>}
        <div className="space-y-2 sm:hidden">
          {rooms.map((r) => (
            <article key={r.id} className="rounded-lg border border-slate-700 p-3 space-y-2">
              <div className="flex items-start gap-2">
                <h3 className="min-w-0 flex-1 font-semibold break-words">{r.name}</h3>
                <span className={`shrink-0 text-xs ${r.status === 'waiting' ? 'text-emerald-400' : 'text-slate-400'}`}>
                  {STATUS_NAME[r.status] ?? r.status}
                </span>
              </div>
              <div className="text-xs text-slate-400 break-words">
                #{r.id} · 房主 {r.ownerName}
              </div>
              <div className="text-sm">
                {r.seats} 人 · 真人 {r.humans} · AI {r.ais} · 空位 {r.open}
                {r.hints && <span className="ml-1 text-fuchsia-300">提示</span>}
              </div>
              <button className="btn w-full" onClick={() => nav(`/room/${r.id}`)}>
                {r.status === 'waiting' && r.open > 0 ? '加入房间' : '观战'}
              </button>
            </article>
          ))}
        </div>
        <div className="hidden sm:block overflow-x-auto">
          <table className="w-full min-w-[560px] text-sm">
            <tbody>
              {rooms.map((r) => (
                <tr key={r.id} className="border-t border-slate-700">
                  <td className="py-2 font-mono text-slate-400">{r.id}</td>
                  <td className="py-2">
                    <div className="font-semibold break-words">{r.name}</div>
                    <div className="text-xs text-slate-400 break-words">房主 {r.ownerName}</div>
                  </td>
                  <td className="text-xs">
                    {r.seats} 人 · 真人 {r.humans} · AI {r.ais} · 空位 {r.open}
                    {r.hints && <span className="ml-1 text-fuchsia-300">提示</span>}
                  </td>
                  <td className={`text-xs ${r.status === 'waiting' ? 'text-emerald-400' : 'text-slate-400'}`}>
                    {STATUS_NAME[r.status] ?? r.status}
                  </td>
                  <td className="text-right">
                    <button className="btn text-xs" onClick={() => nav(`/room/${r.id}`)}>
                      {r.status === 'waiting' && r.open > 0 ? '加入' : '观战'}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <aside className="space-y-4">
        <section className="card space-y-2">
          <h2 className="text-lg font-bold">快速练习</h2>
          <p className="text-xs text-slate-400">与 AI 一对一，默认开启提示与走后复盘。</p>
          <div className="flex gap-2">
            {(['easy', 'medium', 'hard'] as AILevel[]).map((l) => (
              <button key={l} className="btn-primary flex-1" disabled={busy} onClick={() => create({ ai: [l] })}>
                {LEVEL_NAME[l]}
              </button>
            ))}
          </div>
        </section>

        <section className="card space-y-3">
          <h2 className="text-lg font-bold">创建房间</h2>
          <input
            className="input w-full"
            placeholder="房间名（可选）"
            value={name}
            maxLength={30}
            onChange={(e) => setName(e.target.value)}
          />
          <div className="flex items-center gap-2 text-sm">
            人数
            {[2, 3, 4].map((n) => (
              <button key={n} className={n === seats ? 'btn-primary' : 'btn'} onClick={() => setSeats(n)}>
                {n}
              </button>
            ))}
          </div>
          <div className="space-y-1 text-sm">
            <div>座位 1：你（房主）</div>
            {others.slice(0, seats - 1).map((c, i) => (
              <div key={i} className="flex items-center gap-2">
                座位 {i + 2}：
                <select
                  className="input min-w-0 flex-1 py-1"
                  value={c}
                  onChange={(e) => setOthers(others.map((v, j) => (j === i ? (e.target.value as SeatChoice) : v)))}
                >
                  <option value="human">空位（等待真人）</option>
                  <option value="easy">AI · 简单</option>
                  <option value="medium">AI · 中等</option>
                  <option value="hard">AI · 困难</option>
                </select>
              </div>
            ))}
          </div>
          <div className="flex items-center gap-2 text-sm">
            提示
            <select className="input min-w-0 flex-1 py-1" value={hints} onChange={(e) => setHints(e.target.value as 'auto' | 'on' | 'off')}>
              <option value="auto">自动（全 AI 对手时开启）</option>
              <option value="on">允许提示</option>
              <option value="off">禁止提示</option>
            </select>
          </div>
          <div className="flex items-center gap-2 text-sm">
            回合计时
            <select className="input min-w-0 flex-1 py-1" value={turnSeconds} onChange={(e) => setTurnSeconds(Number(e.target.value))}>
              <option value={0}>不限</option>
              <option value={30}>30 秒</option>
              <option value={60}>60 秒</option>
              <option value={120}>120 秒</option>
            </select>
          </div>
          <button className="btn-primary w-full" disabled={busy} onClick={() => create()}>
            创建
          </button>
          {error && (
            <div role="alert" className="text-rose-400 text-sm break-words">
              {error}
            </div>
          )}
        </section>
        <div className="text-xs text-slate-500 text-center">
          查看{' '}
          <Link to="/users/me" className="text-sky-300">
            我的战绩
          </Link>
        </div>
      </aside>
    </div>
  )
}
