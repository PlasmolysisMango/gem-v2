import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api/http'
import type { RoomSummary } from '../api/types'

const STATUS_NAME: Record<string, string> = { waiting: '等待中', playing: '对局中', finished: '已结束' }

export default function Admin() {
  const [hours, setHours] = useState('')
  const [rooms, setRooms] = useState<RoomSummary[]>([])
  const [confirming, setConfirming] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let alive = true
    const loadRooms = () =>
      api
        .rooms()
        .then((r) => alive && setRooms(r))
        .catch((e) => alive && setError(e.message))
    api
      .adminSettings()
      .then((s) => alive && setHours(String(s.idleCloseSeconds / 3600)))
      .catch((e) => alive && setError(e.message))
    loadRooms()
    const t = setInterval(loadRooms, 3000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])

  const save = async (h: number) => {
    if (!Number.isFinite(h) || h < 0) {
      setError('请输入不小于 0 的小时数')
      return
    }
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const s = await api.setAdminSettings({ idleCloseSeconds: Math.round(h * 3600) })
      setHours(String(s.idleCloseSeconds / 3600))
      setNotice(
        s.idleCloseSeconds === 0 ? '已保存：不自动清理空闲房间。' : `已保存：空闲房间 ${s.idleCloseSeconds / 3600} 小时后自动清理。`,
      )
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const closeRoom = async (id: string) => {
    if (confirming !== id) {
      setConfirming(id)
      return
    }
    setConfirming('')
    setError('')
    setNotice('')
    try {
      await api.closeRoom(id)
      setRooms((rs) => rs.filter((r) => r.id !== id))
      setNotice(`房间 ${id} 已关闭。`)
    } catch (e) {
      setError((e as Error).message)
    }
  }

  return (
    <div className="mx-auto max-w-5xl min-w-0 space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="text-xl font-bold">管理</h1>
        <Link to="/" className="btn ml-auto text-sm">
          返回大厅
        </Link>
      </div>
      {error && (
        <div role="alert" className="text-rose-400 text-sm break-words">
          {error}
        </div>
      )}
      {notice && (
        <div role="status" className="text-emerald-400 text-sm break-words">
          {notice}
        </div>
      )}

      <section className="card space-y-3">
        <h2 className="text-lg font-bold">空闲房间清理</h2>
        <p className="text-xs text-slate-400">
          没有在线玩家的房间超过该时长后自动关闭，进行中的对局会被标记为中止。0 表示不自动清理。修改立即生效并保存到数据库。
        </p>
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            save(Number(hours))
          }}
        >
          <label className="text-sm">
            <span className="mb-1 block text-xs text-slate-400">小时</span>
            <input
              className="input w-32"
              type="number"
              min={0}
              step={1}
              value={hours}
              onChange={(e) => setHours(e.target.value)}
              aria-label="空闲清理时长（小时）"
            />
          </label>
          <button className="btn-primary" disabled={busy}>
            保存
          </button>
          {([24, 168, 720] as const).map((h) => (
            <button key={h} type="button" className="btn text-xs" disabled={busy} onClick={() => save(h)}>
              {h === 24 ? '1 天' : h === 168 ? '7 天' : '30 天'}
            </button>
          ))}
          <button type="button" className="btn text-xs" disabled={busy} onClick={() => save(0)}>
            不清理
          </button>
        </form>
        <div className="text-xs text-slate-500">
          当前：{hours === '' ? '读取中…' : Number(hours) === 0 ? '不自动清理' : `${hours} 小时`}
        </div>
      </section>

      <section className="card space-y-3">
        <h2 className="text-lg font-bold">房间</h2>
        {rooms.length === 0 && <div className="text-sm text-slate-400">当前没有房间。</div>}
        {rooms.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[560px] text-sm">
              <thead>
                <tr className="text-left text-slate-400">
                  <th className="py-1">房间号</th>
                  <th>名称</th>
                  <th>房主</th>
                  <th>状态</th>
                  <th>人数</th>
                  <th className="text-right">操作</th>
                </tr>
              </thead>
              <tbody>
                {rooms.map((r) => (
                  <tr key={r.id} className="border-t border-slate-700 align-top">
                    <td className="py-2 font-mono text-slate-400">{r.id}</td>
                    <td className="py-2 break-words">{r.name}</td>
                    <td className="py-2 break-words">{r.ownerName}</td>
                    <td className="py-2 text-xs">{STATUS_NAME[r.status] ?? r.status}</td>
                    <td className="whitespace-nowrap py-2 text-xs">
                      真人 {r.humans} · AI {r.ais} · 空位 {r.open}
                    </td>
                    <td className="py-2 text-right">
                      <button
                        className="btn-danger text-xs"
                        onClick={() => closeRoom(r.id)}
                        aria-label={confirming === r.id ? `确认关闭房间 ${r.id}` : `关闭房间 ${r.id}`}
                      >
                        {confirming === r.id ? '确认关闭' : '关闭房间'}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}
