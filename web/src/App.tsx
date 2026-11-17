import { useEffect } from 'react'
import { Link, Navigate, Route, Routes } from 'react-router-dom'
import { api } from './api/http'
import { useAuth } from './store/auth'
import { useGame } from './store/game'
import Lobby from './pages/Lobby'
import Room from './pages/Room'
import Replay from './pages/Replay'
import Stats from './pages/Stats'
import Admin from './pages/Admin'

function RequireAuth({ children }: { children: JSX.Element }) {
  const { user, ready } = useAuth()
  if (!ready) return null
  if (!user) {
    window.location.assign(`/login?next=${encodeURIComponent(location.pathname + location.search)}`)
    return null
  }
  return children
}

export default function App() {
  const { user, hydrate, setUser, logout } = useAuth()
  const connect = useGame((s) => s.connect)
  const disconnect = useGame((s) => s.disconnect)
  const connected = useGame((s) => s.connected)

  useEffect(() => {
    void hydrate()
  }, [hydrate])

  useEffect(() => {
    if (user) connect()
    else disconnect()
  }, [user, connect, disconnect])

  // Refresh the profile so role changes (and users cached by older clients) are picked up.
  useEffect(() => {
    if (!user) return
    let alive = true
    api
      .me()
      .then((u) => alive && setUser(u))
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [user?.id, setUser])

  return (
    <div className="app-shell">
      <header className="shrink-0 flex flex-wrap items-center gap-x-2 sm:gap-x-3 gap-y-1 px-2 sm:px-4 py-1 sm:py-2 bg-slate-950/80 border-b border-slate-800">
        <Link to="/" className="flex min-h-[44px] items-center text-lg sm:text-xl font-black tracking-wide text-amber-300">
          💎 璀璨宝石
        </Link>
        {user && (
          <>
            <Link to="/" className="inline-flex min-h-[44px] min-w-[44px] items-center text-sm hover:text-sky-300">
              大厅
            </Link>
            <Link to={`/users/${user.id}`} className="inline-flex min-h-[44px] min-w-[44px] items-center text-sm hover:text-sky-300">
              战绩
            </Link>
            {user.isAdmin && (
              <Link to="/admin" className="inline-flex min-h-[44px] min-w-[44px] items-center text-sm text-amber-300 hover:text-amber-200">
                管理
              </Link>
            )}
            <span className="ml-auto min-w-0 max-w-[5rem] sm:max-w-[10rem] truncate text-sm text-slate-400" title={user.username}>
              <span className={`inline-block w-2 h-2 rounded-full mr-1 ${connected ? 'bg-emerald-400' : 'bg-rose-500'}`} />
              {user.username}
            </span>
            <a href="/" className="hidden sm:inline text-sm text-slate-400 hover:text-sky-300">
              统一门户
            </a>
            <button className="btn text-sm" onClick={logout}>
              退出
            </button>
          </>
        )}
      </header>
      <main className="app-main min-w-0 p-2 sm:p-4">
        <Routes>
          <Route
            path="/"
            element={
              <RequireAuth>
                <Lobby />
              </RequireAuth>
            }
          />
          <Route
            path="/room/:id"
            element={
              <RequireAuth>
                <Room />
              </RequireAuth>
            }
          />
          <Route
            path="/games/:id"
            element={
              <RequireAuth>
                <Replay />
              </RequireAuth>
            }
          />
          <Route
            path="/users/:id"
            element={
              <RequireAuth>
                <Stats />
              </RequireAuth>
            }
          />
          <Route
            path="/admin"
            element={
              <RequireAuth>
                <Admin />
              </RequireAuth>
            }
          />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  )
}
