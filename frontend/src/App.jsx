import { useState } from 'react'
import Login from './pages/Login'
import AdminDashboard from './pages/AdminDashboard'
import EmployeeDashboard from './pages/EmployeeDashboard'
import './index.css'

export default function App() {
  const [auth, setAuth] = useState(() => {
    const token = localStorage.getItem('aegis_token')
    const user = localStorage.getItem('aegis_user')
    return token && user ? { token, user: JSON.parse(user) } : null
  })

  const handleLogin = (token, user) => {
    localStorage.setItem('aegis_token', token)
    localStorage.setItem('aegis_user', JSON.stringify(user))
    setAuth({ token, user })
  }

  const handleLogout = () => {
    localStorage.removeItem('aegis_token')
    localStorage.removeItem('aegis_user')
    setAuth(null)
  }

  if (!auth) return <Login onLogin={handleLogin} />
  if (auth.user.role === 'admin') return <AdminDashboard auth={auth} onLogout={handleLogout} />
  return <EmployeeDashboard auth={auth} onLogout={handleLogout} />
}
