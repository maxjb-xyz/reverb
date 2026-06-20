import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'

export default function Setup() {
  const [pw, setPw] = useState('')
  const nav = useNavigate()
  async function submit(e: React.FormEvent) {
    e.preventDefault()
    await api.post('/setup/admin', { password: pw })
    nav('/search')
    window.location.reload()
  }
  return (
    <form onSubmit={submit} className="max-w-sm mx-auto mt-24 space-y-4">
      <h1 className="text-2xl font-bold">Welcome to Crate</h1>
      <p className="text-neutral-400 text-sm">Set an admin password to get started.</p>
      <input
        type="password"
        value={pw}
        onChange={(e) => setPw(e.target.value)}
        className="w-full rounded bg-neutral-900 border border-neutral-700 px-3 py-2"
        placeholder="Choose a password"
      />
      <button className="w-full rounded bg-accent py-2 font-medium text-white">Continue</button>
    </form>
  )
}
