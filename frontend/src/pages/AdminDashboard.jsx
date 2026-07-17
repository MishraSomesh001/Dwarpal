import { useState, useEffect } from 'react'
import GrafanaChart from '../components/GrafanaChart'

const API = 'http://localhost:8080'

export default function AdminDashboard({ auth, onLogout }) {
  const [tab, setTab] = useState('overview')
  const [keys, setKeys] = useState([])
  const [users, setUsers] = useState([])
  const [logs, setLogs] = useState([])
  const [alerts, setAlerts] = useState([])
  const [providers, setProviders] = useState([])
  const [loading, setLoading] = useState(true)
  const [newKeyForm, setNewKeyForm] = useState({ user_id: '', budget_usd: '10' })
  const [newUserForm, setNewUserForm] = useState({ email: '', name: '', role: 'employee', password: '' })
  const [providerForm, setProviderForm] = useState({ name: 'openai', api_key: '', endpoint_url: '' })
  const [toast, setToast] = useState(null)

  const headers = { Authorization: `Bearer ${auth.token}`, 'Content-Type': 'application/json' }

  const showToast = (msg, type = 'success') => {
    setToast({ msg, type })
    setTimeout(() => setToast(null), 3000)
  }

  const fetchAll = async () => {
    setLoading(true)
    try {
      const [k, u, l, a, p] = await Promise.all([
        fetch(`${API}/api/admin/keys`, { headers }).then(r => r.json()),
        fetch(`${API}/api/admin/users`, { headers }).then(r => r.json()),
        fetch(`${API}/api/admin/logs?limit=50`, { headers }).then(r => r.json()),
        fetch(`${API}/api/admin/alerts`, { headers }).then(r => r.json()),
        fetch(`${API}/api/admin/providers`, { headers }).then(r => r.json()),
      ])
      setKeys(Array.isArray(k) ? k : [])
      setUsers(Array.isArray(u) ? u : [])
      setLogs(Array.isArray(l) ? l : [])
      setAlerts(Array.isArray(a) ? a : [])
      setProviders(Array.isArray(p) ? p : [])
    } catch (e) { console.error(e) }
    setLoading(false)
  }

  useEffect(() => { fetchAll() }, [])

  const createKey = async (e) => {
    e.preventDefault()
    const res = await fetch(`${API}/api/admin/keys`, { method: 'POST', headers, body: JSON.stringify({ user_id: parseInt(newKeyForm.user_id), budget_usd: parseFloat(newKeyForm.budget_usd) }) })
    const data = await res.json()
    if (res.ok) { showToast(`Key created: ${data.key}`); fetchAll() }
    else showToast(data.error, 'error')
  }

  const createUser = async (e) => {
    e.preventDefault()
    const res = await fetch(`${API}/api/admin/users`, { method: 'POST', headers, body: JSON.stringify(newUserForm) })
    if (res.ok) { showToast('User created!'); fetchAll(); setNewUserForm({ email: '', name: '', role: 'employee', password: '' }) }
    else showToast((await res.json()).error, 'error')
  }

  const revokeKey = async (id) => {
    await fetch(`${API}/api/admin/keys?id=${id}`, { method: 'DELETE', headers })
    showToast('Key revoked')
    fetchAll()
  }

  const upsertProvider = async (e) => {
    e.preventDefault()
    const res = await fetch(`${API}/api/admin/providers`, { method: 'POST', headers, body: JSON.stringify(providerForm) })
    if (res.ok) { showToast('Provider updated!'); fetchAll() }
    else showToast((await res.json()).error, 'error')
  }

  const totalBudget = keys.reduce((s, k) => s + (k.BudgetUSD || 0), 0)
  const totalSpend = keys.reduce((s, k) => s + (k.SpendUSD || 0), 0)
  const activeKeys = keys.filter(k => k.IsActive).length
  const successLogs = logs.filter(l => l.Status === 'success').length
  const errorLogs = logs.filter(l => l.Status === 'error').length

  const nav = [
    { id: 'overview', label: 'Overview' },
    { id: 'keys', label: 'Virtual Keys' },
    { id: 'users', label: 'Users' },
    { id: 'providers', label: 'Providers' },
    { id: 'logs', label: 'Audit Logs' },
    { id: 'alerts', label: `Alerts ${alerts.length ? `(${alerts.length})` : ''}` },
  ]

  return (
    <div className="layout">
      {/* Toast */}
      {toast && (
        <div style={{ position:'fixed',top:20,right:20,zIndex:999,padding:'12px 20px',borderRadius:8,background: toast.type==='error' ? 'rgba(239,68,68,0.15)':'rgba(34,197,94,0.15)', border: toast.type==='error' ? '1px solid var(--red)' : '1px solid var(--green)', color: toast.type==='error'?'var(--red)':'var(--green)', fontWeight:500 }}>
          {toast.msg}
        </div>
      )}

      {/* Sidebar */}
      <aside className="sidebar">
        <div className="sidebar-logo">
          <div className="logo-icon"></div>
          Aegis Gateway
        </div>
        <div className="sidebar-section">Admin Panel</div>
        <ul className="sidebar-nav">
          {nav.map(n => (
            <li key={n.id}>
              <button className={tab === n.id ? 'active' : ''} onClick={() => setTab(n.id)}>
                {n.label}
              </button>
            </li>
          ))}
        </ul>
        <div className="sidebar-bottom">
          <div className="sidebar-user">
            <div className="avatar">{auth.user.name?.[0] || 'A'}</div>
            <div className="user-info">
              <div className="user-name">{auth.user.name || auth.user.email}</div>
              <div className="user-role tag-admin">Admin</div>
            </div>
            <button className="logout-btn" onClick={onLogout} title="Logout">Logout</button>
          </div>
        </div>
      </aside>

      {/* Main */}
      <main className="main-content">
        {loading && tab === 'overview' ? (
          <div style={{ textAlign:'center', paddingTop:80 }}><span className="spinner" style={{width:32,height:32,borderWidth:3}} /></div>
        ) : (
          <>
            {/* Overview */}
            {tab === 'overview' && (
              <>
                <div className="page-header">
                  <div className="page-title">Dashboard Overview</div>
                  <div className="page-subtitle">Live metrics across all tenants and virtual keys</div>
                </div>
                <div className="stats-grid">
                  <div className="stat-card"><div className="stat-label">Active Keys</div><div className="stat-value">{activeKeys}</div></div>
                  <div className="stat-card"><div className="stat-label">Total Users</div><div className="stat-value">{users.length}</div></div>
                  <div className="stat-card"><div className="stat-label">Total Spend</div><div className="stat-value">${totalSpend.toFixed(4)}</div></div>
                  <div className="stat-card"><div className="stat-label">Total Budget</div><div className="stat-value">${totalBudget.toFixed(2)}</div></div>
                  <div className="stat-card"><div className="stat-label">Success Requests</div><div className="stat-value">{successLogs}</div></div>
                  <div className="stat-card"><div className="stat-label">Error Requests</div><div className="stat-value" style={{color:'var(--red)'}}>{errorLogs}</div></div>
                </div>

                {/* Grafana Live Metrics Panels */}
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: '16px', marginBottom: '20px' }}>
                  <GrafanaChart 
                    title="Live Request Latency (ms)" 
                    type="line" 
                    color="#2f81f7"
                    data={logs.slice().reverse().map(l => ({
                      label: new Date(l.CreatedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
                      value: l.LatencyMS || 0
                    }))} 
                  />
                  <GrafanaChart 
                    title="Request Count by Provider" 
                    type="bar" 
                    color="#3fb950"
                    data={Object.entries(
                      logs.reduce((acc, l) => {
                        const prov = l.Provider || 'unknown';
                        acc[prov] = (acc[prov] || 0) + 1;
                        return acc;
                      }, {})
                    ).map(([prov, count]) => ({
                      label: prov.toUpperCase(),
                      value: count
                    }))} 
                  />
                </div>

                {alerts.length > 0 && (
                  <div className="card">
                    <div className="card-header"><div className="card-title">Budget Alerts</div></div>
                    {alerts.map((a, i) => {
                      const pct = a.BudgetUSD > 0 ? (a.SpendUSD / a.BudgetUSD) * 100 : 0
                      return (
                        <div key={i} className={"alert-banner " + (pct >= 100 ? "danger" : "")}>
                          <span className="alert-banner-icon">{pct >= 100 ? '●' : '▲'}</span>
                          <div className="alert-banner-text">
                            <strong>{a.UserEmail?.String || 'Unknown'}</strong> — Key <span className="mono">{a.KeyPreview}</span> is at <strong>{pct.toFixed(0)}%</strong> of ${a.BudgetUSD} budget (${a.SpendUSD.toFixed(4)} spent)
                          </div>
                        </div>
                      )
                    })}
                  </div>
                )}
                <div className="card">
                  <div className="card-header"><div className="card-title">Recent Requests</div></div>
                  <div className="table-wrap">
                    <table>
                      <thead><tr><th>Time</th><th>Key</th><th>Model</th><th>Provider</th><th>Latency</th><th>Status</th></tr></thead>
                      <tbody>
                        {logs.slice(0, 10).map((l, i) => (
                          <tr key={i}>
                            <td className="mono">{new Date(l.CreatedAt).toLocaleTimeString()}</td>
                            <td className="mono">{l.VirtualKey?.slice(0,8)}…</td>
                            <td>{l.Model}</td>
                            <td>{l.Provider}</td>
                            <td>{l.LatencyMS}ms</td>
                            <td><span className={"badge " + (l.Status === 'success' ? 'badge-green' : 'badge-red')}>{l.Status}</span></td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              </>
            )}

            {/* Keys */}
            {tab === 'keys' && (
              <>
                <div className="page-header"><div className="page-title">Virtual Keys</div><div className="page-subtitle">Manage API keys for your employees</div></div>
                <div className="card">
                  <div className="card-title" style={{marginBottom:16}}>Provision New Key</div>
                  <form onSubmit={createKey}>
                    <div className="form-grid">
                      <div className="form-group">
                        <label>Assign to User</label>
                        <select value={newKeyForm.user_id} onChange={e => setNewKeyForm({...newKeyForm, user_id: e.target.value})} required>
                          <option value="">Select user…</option>
                          {users.map(u => <option key={u.ID} value={u.ID}>{u.Name} ({u.Email})</option>)}
                        </select>
                      </div>
                      <div className="form-group">
                        <label>Budget (USD)</label>
                        <input type="number" step="0.01" min="0.01" value={newKeyForm.budget_usd} onChange={e => setNewKeyForm({...newKeyForm, budget_usd: e.target.value})} required />
                      </div>
                    </div>
                    <button className="btn btn-primary" type="submit" style={{marginTop:14}}>Generate Key</button>
                  </form>
                </div>
                <div className="card">
                  <div className="card-title" style={{marginBottom:16}}>All Virtual Keys</div>
                  <div className="table-wrap">
                    <table>
                      <thead><tr><th>Key</th><th>Employee</th><th>Budget</th><th>Spent</th><th>Usage</th><th>Status</th><th>Action</th></tr></thead>
                      <tbody>
                        {keys.map((k, i) => {
                          const pct = k.BudgetUSD > 0 ? Math.min(100, (k.SpendUSD / k.BudgetUSD) * 100) : 0
                          const cls = pct >= 90 ? 'danger' : pct >= 70 ? 'warn' : 'safe'
                          return (
                            <tr key={i}>
                              <td className="mono">
                                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                                  <span>{k.KeyPreview.slice(0, 12)}...</span>
                                  <button className="btn btn-outline btn-sm" style={{ padding: '2px 6px', fontSize: 10 }} onClick={() => {
                                    navigator.clipboard.writeText(k.KeyPreview)
                                    showToast('Copied full key!')
                                  }}>📋 Copy</button>
                                </div>
                              </td>
                              <td>{k.UserEmail?.String || '—'}</td>
                              <td>${k.BudgetUSD?.toFixed(2)}</td>
                              <td>${k.SpendUSD?.toFixed(4)}</td>
                              <td style={{minWidth:120}}>
                                <div className="meter-bar"><div className={`meter-fill ${cls}`} style={{width:`${pct}%`}} /></div>
                                <div style={{fontSize:10,color:'var(--text-muted)',marginTop:3}}>{pct.toFixed(0)}%</div>
                              </td>
                              <td><span className={`badge ${k.IsActive ? 'badge-green' : 'badge-red'}`}>{k.IsActive ? 'Active' : 'Revoked'}</span></td>
                              <td>{k.IsActive && <button className="btn btn-danger btn-sm" onClick={() => revokeKey(k.ID)}>Revoke</button>}</td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                </div>
              </>
            )}

            {/* Users */}
            {tab === 'users' && (
              <>
                <div className="page-header"><div className="page-title">Users</div><div className="page-subtitle">Manage employees and their access</div></div>
                <div className="card">
                  <div className="card-title" style={{marginBottom:16}}>Add New User</div>
                  <form onSubmit={createUser}>
                    <div className="form-grid">
                      <div className="form-group"><label>Name</label><input value={newUserForm.name} onChange={e => setNewUserForm({...newUserForm,name:e.target.value})} required /></div>
                      <div className="form-group"><label>Email</label><input type="email" value={newUserForm.email} onChange={e => setNewUserForm({...newUserForm,email:e.target.value})} required /></div>
                      <div className="form-group"><label>Role</label><select value={newUserForm.role} onChange={e => setNewUserForm({...newUserForm,role:e.target.value})}><option value="employee">Employee</option><option value="admin">Admin</option></select></div>
                      <div className="form-group"><label>Password</label><input type="password" value={newUserForm.password} onChange={e => setNewUserForm({...newUserForm,password:e.target.value})} required /></div>
                    </div>
                    <button className="btn btn-primary" style={{marginTop:14}} type="submit">Add User</button>
                  </form>
                </div>
                <div className="card">
                  <div className="table-wrap">
                    <table>
                      <thead><tr><th>ID</th><th>Name</th><th>Email</th><th>Role</th></tr></thead>
                      <tbody>
                        {users.map((u, i) => (
                          <tr key={i}>
                            <td className="mono">#{u.ID}</td>
                            <td>{u.Name}</td>
                            <td>{u.Email}</td>
                            <td><span className={"badge " + (u.Role === 'admin' ? 'badge-purple' : 'badge-green')}>{u.Role}</span></td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              </>
            )}

            {/* Providers */}
            {tab === 'providers' && (
              <>
                <div className="page-header"><div className="page-title">Provider Configuration</div><div className="page-subtitle">Manage upstream LLM API keys</div></div>
                <div className="card">
                  <div className="card-title" style={{marginBottom:16}}>Update Provider Key</div>
                  <form onSubmit={upsertProvider}>
                    <div className="form-grid">
                      <div className="form-group">
                        <label>Provider</label>
                        <select value={providerForm.name} onChange={e => setProviderForm({...providerForm,name:e.target.value})}>
                          <option value="openai">OpenAI</option>
                          <option value="gemini">Gemini</option>
                          <option value="ollama">Ollama</option>
                        </select>
                      </div>
                      <div className="form-group"><label>API Key</label><input value={providerForm.api_key} onChange={e => setProviderForm({...providerForm,api_key:e.target.value})} placeholder="sk-..." /></div>
                      <div className="form-group" style={{gridColumn:'1/-1'}}><label>Endpoint URL</label><input value={providerForm.endpoint_url} onChange={e => setProviderForm({...providerForm,endpoint_url:e.target.value})} placeholder="https://api.openai.com" /></div>
                    </div>
                    <button className="btn btn-primary" style={{marginTop:14}} type="submit">Save Provider</button>
                  </form>
                </div>
                {providers.length > 0 && (
                  <div className="card">
                    <div className="card-title" style={{marginBottom:16}}>Configured Providers</div>
                    <div className="table-wrap">
                      <table>
                        <thead><tr><th>Name</th><th>API Key</th><th>Endpoint</th></tr></thead>
                        <tbody>
                          {providers.map((p, i) => (
                            <tr key={i}>
                              <td><span className="badge badge-purple">{p.Name}</span></td>
                              <td className="mono">{p.APIKey ? `${p.APIKey.slice(0,8)}••••••` : '—'}</td>
                              <td>{p.EndpointURL || '—'}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}
              </>
            )}

            {/* Logs */}
            {tab === 'logs' && (
              <>
                <div className="page-header"><div className="page-title">Audit Log</div><div className="page-subtitle">Every request through the Aegis gateway</div></div>
                <div className="card">
                  <div className="table-wrap">
                    <table>
                      <thead><tr><th>Timestamp</th><th>Key</th><th>Model</th><th>Provider</th><th>Latency</th><th>Cost</th><th>Status</th></tr></thead>
                      <tbody>
                        {logs.length === 0 ? (
                          <tr><td colSpan={7}><div className="empty-state">No requests logged yet</div></td></tr>
                        ) : logs.map((l, i) => (
                          <tr key={i}>
                            <td className="mono">{new Date(l.CreatedAt).toLocaleString()}</td>
                            <td className="mono">{l.VirtualKey?.slice(0,12)}…</td>
                            <td>{l.Model || '—'}</td>
                            <td>{l.Provider || '—'}</td>
                            <td>{l.LatencyMS}ms</td>
                            <td className="mono">${(l.CostUSD || 0).toFixed(6)}</td>
                            <td><span className={`badge ${l.Status === 'success' ? 'badge-green' : 'badge-red'}`}>{l.Status}</span></td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              </>
            )}

            {/* Alerts */}
            {tab === 'alerts' && (
              <>
                <div className="page-header"><div className="page-title">Budget Alerts</div><div className="page-subtitle">Keys that have consumed ≥80% of their budget</div></div>
                {alerts.length === 0 ? (
                  <div className="card"><div className="empty-state">All keys are within budget</div></div>
                ) : alerts.map((a, i) => {
                  const pct = a.BudgetUSD > 0 ? (a.SpendUSD / a.BudgetUSD) * 100 : 0
                  return (
                    <div className="card" key={i}>
                      <div className="card-header">
                        <div>
                          <div className="card-title">{a.UserEmail?.String || 'Unknown User'}</div>
                          <div className="card-subtitle" style={{fontFamily:'var(--mono)'}}>{a.KeyPreview}</div>
                        </div>
                        <span className={`badge ${pct >= 100 ? 'badge-red' : 'badge-yellow'}`}>{pct.toFixed(0)}% used</span>
                      </div>
                      <div className="meter-bar"><div className={`meter-fill ${pct >= 100 ? 'danger' : 'warn'}`} style={{width:`${Math.min(100,pct)}%`}} /></div>
                      <div className="meter-labels"><span>$0</span><span>${a.SpendUSD.toFixed(4)} spent</span><span>${a.BudgetUSD.toFixed(2)} limit</span></div>
                    </div>
                  )
                })}
              </>
            )}
          </>
        )}
      </main>
    </div>
  )
}
