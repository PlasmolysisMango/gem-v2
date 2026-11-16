import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { api } from '../api/http'
import { useAuth } from '../store/auth'

export default function Login() {
  const { token, login } = useAuth()
  const nav = useNavigate()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (token) return <Navigate to="/" replace />

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (mode === 'register' && password !== confirm) {
      setError('两次输入的密码不一致')
      return
    }
    setBusy(true)
    try {
      const res = mode === 'login' ? await api.login(username, password) : await api.register(username, password)
      login(res.token, res.user)
      nav('/')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="max-w-sm mx-auto mt-6 sm:mt-16 card space-y-4">
      <h1 className="text-2xl font-bold text-center">{mode === 'login' ? '登录' : '注册'}</h1>
      <form className="space-y-3" onSubmit={submit}>
        <input
          aria-label="用户名"
          className="input w-full"
          placeholder="用户名（3-20 位）"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
        />
        <input
          aria-label="密码"
          className="input w-full"
          type="password"
          placeholder="密码（至少 6 位）"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
        />
        {mode === 'register' && (
          <input
            aria-label="确认密码"
            className="input w-full"
            type="password"
            placeholder="确认密码"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
          />
        )}
        {error && <div className="text-rose-400 text-sm">{error}</div>}
        <button className="btn-primary w-full" disabled={busy || !username || !password}>
          {mode === 'login' ? '登录' : '注册并登录'}
        </button>
      </form>
      <div className="text-center text-sm">
        {mode === 'login' ? (
          <button className="min-h-[44px] px-2 text-sky-300 hover:underline" onClick={() => setMode('register')}>
            没有账号？注册
          </button>
        ) : (
          <button className="min-h-[44px] px-2 text-sky-300 hover:underline" onClick={() => setMode('login')}>
            已有账号？登录
          </button>
        )}
      </div>
    </div>
  )
}
