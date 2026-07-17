import { useState, useEffect } from 'react'
import GrafanaChart from '../components/GrafanaChart'

const API = 'http://localhost:8080'

export default function EmployeeDashboard({ auth, onLogout }) {
  const [tab, setTab] = useState('dashboard')
  const [data, setData] = useState(null)
  const [logs, setLogs] = useState([])
  const [loading, setLoading] = useState(true)
  const [copied, setCopied] = useState(null)
  const [visibleKeys, setVisibleKeys] = useState({})

  const headers = { Authorization: `Bearer ${auth.token}` }

  const fetchData = async () => {
    setLoading(true)
    try {
      const [d, l] = await Promise.all([
        fetch(`${API}/api/employee/dashboard`, { headers }).then(r => r.json()),
        fetch(`${API}/api/employee/logs`, { headers }).then(r => r.json()),
      ])
      setData(d)
      setLogs(Array.isArray(l) ? l : [])
    } catch (e) { console.error(e) }
    setLoading(false)
  }

  useEffect(() => { fetchData() }, [])

  const copyKey = (key) => {
    navigator.clipboard.writeText(key)
    setCopied(key)
    setTimeout(() => setCopied(null), 2000)
  }

  const keys = data?.keys || []
  const totalBudget = keys.reduce((s, k) => s + (k.BudgetUSD || 0), 0)
  const totalSpend = keys.reduce((s, k) => s + (k.SpendUSD || 0), 0)
  const budgetPct = totalBudget > 0 ? Math.min(100, (totalSpend / totalBudget) * 100) : 0
  const budgetCls = budgetPct >= 90 ? 'danger' : budgetPct >= 70 ? 'warn' : 'safe'

  // Model breakdown from logs
  const modelCounts = logs.reduce((acc, l) => {
    if (l.Model) acc[l.Model] = (acc[l.Model] || 0) + 1
    return acc
  }, {})
  const modelEntries = Object.entries(modelCounts).sort((a, b) => b[1] - a[1])
  const totalModelLogs = modelEntries.reduce((s, [, c]) => s + c, 0)

  const nav = [
    { id: 'dashboard', label: 'My Dashboard' },
    { id: 'logs', label: 'My Requests' },
  ]

  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="sidebar-logo"><div className="logo-icon"></div>Aegis Gateway</div>
        <div className="sidebar-section">Employee Portal</div>
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
            <div className="avatar">{auth.user.name?.[0] || 'E'}</div>
            <div className="user-info">
              <div className="user-name">{auth.user.name || auth.user.email}</div>
              <div className="user-role tag-employee">Employee</div>
            </div>
            <button className="logout-btn" onClick={onLogout} title="Logout">Logout</button>
          </div>
        </div>
      </aside>

      <main className="main-content">
        {loading ? (
          <div style={{ textAlign:'center',paddingTop:80 }}><span className="spinner" style={{width:32,height:32,borderWidth:3}} /></div>
        ) : (
          <>
            {tab === 'dashboard' && (
              <>
                <div className="page-header">
                  <div className="page-title">Welcome, {auth.user.name || auth.user.email}</div>
                  <div className="page-subtitle">Here's your API usage overview</div>
                </div>

                {/* Budget Overview */}
                <div className="stats-grid">
                  <div className="stat-card">
                    <div className="stat-label">Active Keys</div>
                    <div className="stat-value">{keys.filter(k => k.IsActive).length}</div>
                  </div>
                  <div className="stat-card">
                    <div className="stat-label">Total Budget</div>
                    <div className="stat-value">${totalBudget.toFixed(2)}</div>
                  </div>
                  <div className="stat-card">
                    <div className="stat-label">Spent</div>
                    <div className="stat-value" style={{color: budgetPct > 70 ? 'var(--yellow)' : 'inherit'}}>
                      ${totalSpend.toFixed(4)}
                    </div>
                  </div>
                  <div className="stat-card">
                    <div className="stat-label">Total Requests</div>
                    <div className="stat-value">{logs.length}</div>
                  </div>
                </div>

                {/* Grafana Live Metrics Panels */}
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: '16px', marginBottom: '20px' }}>
                  <GrafanaChart 
                    title="My Request Latency Trend (ms)" 
                    type="line" 
                    color="#2f81f7"
                    data={logs.slice().reverse().map(l => ({
                      label: new Date(l.CreatedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
                      value: l.LatencyMS || 0
                    }))} 
                  />
                  <GrafanaChart 
                    title="My Request Volume by Provider" 
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

                {/* Budget Meter */}
                <div className="card">
                  <div className="card-header">
                    <div>
                      <div className="card-title">Budget Consumption</div>
                      <div className="card-subtitle">Across all your virtual keys</div>
                    </div>
                    <span className={`badge ${budgetPct >= 90 ? 'badge-red' : budgetPct >= 70 ? 'badge-yellow' : 'badge-green'}`}>
                      {budgetPct.toFixed(1)}% used
                    </span>
                  </div>
                  <div className="meter-bar" style={{height:12}}>
                    <div className={`meter-fill ${budgetCls}`} style={{width:`${budgetPct}%`}} />
                  </div>
                  <div className="meter-labels">
                    <span>$0.00 spent</span>
                    <span style={{fontWeight:600,color:'var(--text-primary)'}}>${totalSpend.toFixed(6)} of ${totalBudget.toFixed(2)}</span>
                    <span>${(totalBudget - totalSpend).toFixed(4)} remaining</span>
                  </div>
                </div>

                {/* My Keys */}
                <div className="card">
                  <div className="card-header">
                    <div className="card-title">My Virtual Keys</div>
                    <div style={{fontSize:12,color:'var(--text-secondary)'}}>Use these in your API calls</div>
                  </div>
                  {keys.length === 0 ? (
                    <div className="empty-state"><div className="empty-state-icon">🔑</div><p>No keys assigned yet. Contact your admin.</p></div>
                  ) : keys.map((k, i) => {
                    const pct = k.BudgetUSD > 0 ? Math.min(100, (k.SpendUSD / k.BudgetUSD) * 100) : 0
                    const isShown = !!visibleKeys[k.KeyPreview]
                    const displayText = isShown ? k.KeyPreview : `${k.KeyPreview.slice(0, 8)}••••••••`
                    return (
                      <div key={i} style={{background:'var(--bg-hover)',borderRadius:8,padding:16,marginBottom:12}}>
                        <div style={{display:'flex',justifyContent:'space-between',alignItems:'center',marginBottom:10}}>
                          <div className="key-row">
                            <div className="key-display">{displayText}</div>
                            <button className="btn btn-outline btn-sm" onClick={() => setVisibleKeys({
                              ...visibleKeys,
                              [k.KeyPreview]: !isShown
                            })}>
                              {isShown ? '👁️ Hide' : '👁️ Show'}
                            </button>
                            <button className="btn btn-outline btn-sm copy-btn" onClick={() => copyKey(k.KeyPreview)}>
                              {copied === k.KeyPreview ? '✅ Copied!' : '📋 Copy'}
                            </button>
                          </div>
                          <span className={`badge ${k.IsActive ? 'badge-green' : 'badge-red'}`}>{k.IsActive ? 'Active' : 'Revoked'}</span>
                        </div>
                        <div style={{display:'flex',gap:16,fontSize:12,color:'var(--text-secondary)',marginBottom:8}}>
                          <span>Budget: <strong style={{color:'var(--text-primary)'}}>${k.BudgetUSD?.toFixed(2)}</strong></span>
                          <span>Spent: <strong style={{color: pct > 70 ? 'var(--yellow)' : 'var(--text-primary)'}}>${k.SpendUSD?.toFixed(4)}</strong></span>
                          <span>Remaining: <strong style={{color:'var(--green)'}}>${((k.BudgetUSD||0)-(k.SpendUSD||0)).toFixed(4)}</strong></span>
                        </div>
                        <div className="meter-bar">
                           <div className={"meter-fill " + (pct>=90?'danger':pct>=70?'warn':'safe')} style={{width:`${pct}%`}} />
                        </div>
                      </div>
                    )
                  })}
                </div>

                {/* Model Usage */}
                {modelEntries.length > 0 && (
                  <div className="card">
                    <div className="card-header"><div className="card-title">Model Usage Breakdown</div></div>
                    {modelEntries.map(([model, count]) => {
                      const pct = totalModelLogs > 0 ? (count / totalModelLogs) * 100 : 0
                      return (
                        <div key={model} style={{marginBottom:12}}>
                          <div style={{display:'flex',justifyContent:'space-between',marginBottom:4}}>
                            <span style={{fontSize:13,fontFamily:'var(--mono)'}}>{model}</span>
                            <span style={{fontSize:12,color:'var(--text-secondary)'}}>{count} reqs ({pct.toFixed(0)}%)</span>
                          </div>
                          <div className="meter-bar">
                            <div className="meter-fill safe" style={{width:`${pct}%`}} />
                          </div>
                        </div>
                      )
                    })}
                  </div>
                )}
              </>
            )}

            {tab === 'logs' && (
              <>
                <div className="page-header"><div className="page-title">My Request History</div><div className="page-subtitle">All API calls made with your keys</div></div>
                <div className="card">
                  <div className="table-wrap">
                    <table>
                      <thead><tr><th>Timestamp</th><th>Model</th><th>Provider</th><th>Latency</th><th>Cost</th><th>Status</th></tr></thead>
                      <tbody>
                        {logs.length === 0 ? (
                           <tr><td colSpan={6}><div className="empty-state">No requests yet</div></td></tr>
                        ) : logs.map((l, i) => (
                          <tr key={i}>
                            <td className="mono">{new Date(l.CreatedAt).toLocaleString()}</td>
                            <td>{l.Model || '—'}</td>
                            <td>{l.Provider || '—'}</td>
                            <td>{l.LatencyMS}ms</td>
                            <td className="mono">${(l.CostUSD || 0).toFixed(6)}</td>
                             <td><span className={"badge " + (l.Status === 'success' ? 'badge-green' : 'badge-red')}>{l.Status}</span></td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              </>
            )}
          </>
        )}
      </main>
    </div>
  )
}
