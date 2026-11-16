import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api } from '../api/http'
import type { Stats as StatsData } from '../api/types'
import { pct } from '../lib/gems'
import { useAuth } from '../store/auth'
import { LEVEL_NAME } from './Lobby'

export default function Stats() {
  const { id = 'me' } = useParams()
  const me = useAuth((s) => s.user)
  const uid = id === 'me' ? (me?.id ?? 0) : Number(id)
  const [data, setData] = useState<StatsData | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!uid) return
    api
      .stats(uid)
      .then(setData)
      .catch((e) => setError(e.message))
  }, [uid])

  if (error) return <div className="text-rose-400 text-center mt-20">{error}</div>
  if (!data) return <div className="text-slate-400 text-center mt-20">加载中…</div>

  return (
    <div className="max-w-4xl mx-auto space-y-4">
      <div className="card grid grid-cols-2 sm:grid-cols-5 items-center gap-4 sm:gap-8">
        <div className="col-span-full sm:col-span-1 min-w-0">
          <div className="text-2xl font-bold break-words">{data.user.username}</div>
          <div className="text-xs text-slate-400">注册于 {new Date(data.user.createdAt).toLocaleDateString()}</div>
        </div>
        <Stat label="对局" value={String(data.games)} />
        <Stat label="胜场" value={String(data.wins)} />
        <Stat label="胜率" value={data.games ? pct(data.winRate) : '-'} />
        <Stat label="平均分" value={data.games ? data.avgPoints.toFixed(1) : '-'} />
      </div>
      <div className="card">
        <h3 className="font-semibold mb-2">最近对局</h3>
        {data.recent.length === 0 && <div className="text-sm text-slate-400">还没有对局记录。</div>}
        <div className="space-y-3 sm:hidden">
          {data.recent.map((g) => {
            const mine = g.players.find((p) => p.userId === data.user.id)
            return (
              <article key={g.id} className="border-t border-slate-700 pt-3 space-y-2">
                <div className="text-xs text-slate-400">{new Date(g.startedAt * 1000).toLocaleString()}</div>
                <div className="text-sm break-words">
                  {g.players.map((p) => (
                    <div key={p.seat} className={p.winner ? 'text-yellow-300' : ''}>
                      {p.username || `AI·${LEVEL_NAME[p.aiLevel ?? 'medium']}`} · {p.finalPoints ?? '-'} 分
                    </div>
                  ))}
                </div>
                <div className="flex items-center justify-between gap-2">
                  <span className={mine?.winner ? 'text-emerald-400' : 'text-slate-400'}>
                    {g.status === 'playing' ? '进行中' : g.status === 'aborted' ? '中止' : mine?.winner ? '胜' : '负'}
                  </span>
                  {g.status !== 'playing' && (
                    <Link to={`/games/${g.id}`} className="btn">
                      复盘
                    </Link>
                  )}
                </div>
              </article>
            )
          })}
        </div>
        <div className="hidden sm:block overflow-x-auto">
          <table className="w-full min-w-[560px] text-sm">
            <tbody>
              {data.recent.map((g) => {
                const mine = g.players.find((p) => p.userId === data.user.id)
                return (
                  <tr key={g.id} className="border-t border-slate-700">
                    <td className="py-2 text-slate-400">{new Date(g.startedAt * 1000).toLocaleString()}</td>
                    <td>
                      {g.players.map((p) => (
                        <span key={p.seat} className={`mr-2 ${p.winner ? 'text-yellow-300' : ''}`}>
                          {p.username || `AI·${LEVEL_NAME[p.aiLevel ?? 'medium']}`} {p.finalPoints ?? '-'}
                        </span>
                      ))}
                    </td>
                    <td className={mine?.winner ? 'text-emerald-400' : 'text-slate-400'}>
                      {g.status === 'playing' ? '进行中' : g.status === 'aborted' ? '中止' : mine?.winner ? '胜' : '负'}
                    </td>
                    <td className="text-right">
                      {g.status !== 'playing' && (
                        <Link to={`/games/${g.id}`} className="btn text-xs">
                          复盘
                        </Link>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="text-center">
      <div className="text-2xl font-black text-amber-300">{value}</div>
      <div className="text-xs text-slate-400">{label}</div>
    </div>
  )
}
