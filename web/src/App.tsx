import { useEffect } from 'react'
import { Link, Navigate, Route, Routes, useNavigate } from 'react-router-dom'
import { setUnauthorizedHandler } from './api/http'
import { useAuth } from './store/auth'
import { useGame } from './store/game'
import Login from './pages/Login'
import Lobby from './pages/Lobby'
import Room from './pages/Room'
import Replay from './pages/Replay'
import Stats from './pages/Stats'

function RequireAuth({ children }: { children: JSX.Element }) {
  const token = useAuth((s) => s.token)
  if (!token) return <Navigate to="/login" replace />
  return children
}

export default function App() {
  const { token, user, logout } = useAuth()
  const connect = useGame((s) => s.connect)
  const disconnect = useGame((s) => s.disconnect)
  const connected = useGame((s) => s.connected)
  const nav = useNavigate()

  useEffect(() => {
    setUnauthorizedHandler(() => {
      logout()
      nav('/login')
    })
  }, [logout, nav])

  useEffect(() => {
    if (token) connect(token)
    else disconnect()
  }, [token, connect, disconnect])

  return (
    <div className="app-shell">
      <header className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 sm:px-4 py-2 bg-slate-950/80 border-b border-slate-800">
        <Link to="/" className="flex min-h-[44px] w-full sm:w-auto items-center text-xl font-black tracking-wide text-amber-300">
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
            <span className="ml-auto min-w-0 max-w-[8rem] truncate text-sm text-slate-400" title={user.username}>
              <span className={`inline-block w-2 h-2 rounded-full mr-1 ${connected ? 'bg-emerald-400' : 'bg-rose-500'}`} />
              {user.username}
            </span>
            <button
              className="btn text-sm"
              onClick={() => {
                logout()
                nav('/login')
              }}
            >
              退出
            </button>
          </>
        )}
      </header>
      <main className="app-main min-w-0 p-2 sm:p-4">
        <Routes>
          <Route path="/login" element={<Login />} />
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
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  )
}
