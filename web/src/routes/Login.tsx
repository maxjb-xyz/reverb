import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'

export default function Login() {
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const nav = useNavigate()
  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    try {
      await api.post('/auth/login', { password: pw })
      nav('/search')
      window.location.reload()
    } catch {
      setErr('Invalid password')
    }
  }
  return (
    <form onSubmit={submit} className="max-w-sm mx-auto mt-24 space-y-4">
      <h1 className="text-2xl font-bold">Log in to Crate</h1>
      <input
        type="password"
        value={pw}
        onChange={(e) => setPw(e.target.value)}
        className="w-full rounded bg-neutral-900 border border-neutral-700 px-3 py-2"
        placeholder="Admin password"
      />
      {err && <p className="text-accent text-sm">{err}</p>}
      <button type="submit" className="w-full rounded bg-accent py-2 font-medium text-white">Log in</button>
    </form>
  )
}
