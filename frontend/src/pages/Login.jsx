import { useState } from 'react'

const API = 'http://localhost:8080'

export default function Login({ onLogin }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e) => {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      const res = await fetch(`${API}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password })
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'Login failed')
      onLogin(data.token, data.user)
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-card card">
        <div className="login-logo">
          <h1>Aegis Gateway</h1>
          <p>Control Portal — Sign in to continue</p>
        </div>
        <form className="login-form" onSubmit={handleSubmit}>
          <div className="form-group">
            <label>Email</label>
            <input type="email" placeholder="admin@aegis.dev" value={email} onChange={e => setEmail(e.target.value)} required />
          </div>
          <div className="form-group">
            <label>Password</label>
            <input type="password" placeholder="••••••••" value={password} onChange={e => setPassword(e.target.value)} required />
          </div>
          {error && <div style={{ color: 'var(--red)', fontSize: 13, textAlign: 'center' }}>Error: {error}</div>}
          <button className="btn btn-primary btn-full" type="submit" disabled={loading}>
            {loading ? <span className="spinner" /> : null} Sign In
          </button>
        </form>
        <div style={{ marginTop: 20, padding: 14, background: 'var(--bg-hover)', borderRadius: 8, fontSize: 12, color: 'var(--text-secondary)' }}>
          <div><strong style={{color:'var(--accent)'}}>Admin:</strong> admin@aegis.dev / admin123</div>
          <div style={{marginTop:4}}><strong style={{color:'var(--green)'}}>Employee:</strong> employee@aegis.dev / employee123</div>
        </div>
      </div>
    </div>
  )
}
