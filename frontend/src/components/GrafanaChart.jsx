import React from 'react'

export default function GrafanaChart({ title, data, type = 'line', color = '#2f81f7', height = 140 }) {
  // data format: Array of { label: string, value: number }
  if (!data || data.length === 0) {
    return (
      <div style={{ height, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-secondary)', background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: 'var(--radius)', fontSize: 12 }}>
        No metric data active
      </div>
    );
  }

  const padding = 20;
  const svgWidth = 500;
  const svgHeight = height;
  const chartWidth = svgWidth - padding * 2;
  const chartHeight = svgHeight - padding * 2.5;

  const values = data.map(d => d.value);
  const maxValue = Math.max(...values, 5); // Fallback to 5 to avoid division by zero
  const minValue = 0;
  const range = maxValue - minValue;

  // Calculate coordinates
  const points = data.map((d, index) => {
    const x = padding + (index / (data.length - 1 || 1)) * chartWidth;
    const y = padding + chartHeight - ((d.value - minValue) / range) * chartHeight;
    return { x, y, label: d.label, value: d.value };
  });

  const linePath = points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x} ${p.y}`).join(' ');
  const areaPath = points.length > 0
    ? `${linePath} L ${points[points.length - 1].x} ${padding + chartHeight} L ${points[0].x} ${padding + chartHeight} Z`
    : '';

  return (
    <div style={{ background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: 'var(--radius)', padding: '16px', marginBottom: '16px' }}>
      <div style={{ fontSize: 12, fontWeight: 600, color: '#fff', marginBottom: 12, textTransform: 'uppercase', letterSpacing: '0.5px' }}>{title}</div>
      <svg viewBox={`0 0 ${svgWidth} ${svgHeight}`} style={{ width: '100%', height: 'auto', display: 'block' }}>
        
        {/* Y-axis Gridlines & Labels */}
        {[0, 0.5, 1].map((p, i) => {
          const y = padding + chartHeight * p;
          const val = maxValue - (range * p);
          return (
            <g key={i}>
              <line x1={padding} y1={y} x2={svgWidth - padding} y2={y} stroke="var(--border)" strokeDasharray="2,2" />
              <text x={padding - 6} y={y + 3} fill="var(--text-secondary)" fontSize="9" textAnchor="end">{val.toFixed(0)}</text>
            </g>
          );
        })}

        {/* Chart Render */}
        {type === 'line' && (
          <>
            {/* Area Fill */}
            <path d={areaPath} fill={color} fillOpacity="0.08" />
            
            {/* Trend Line */}
            <path d={linePath} fill="none" stroke={color} strokeWidth="1.5" />
            
            {/* Data Nodes */}
            {points.map((p, i) => (
              <g key={i}>
                <circle cx={p.x} cy={p.y} r="2.5" fill="#fff" stroke={color} strokeWidth="1.5" />
                <title>{p.label}: {p.value}</title>
              </g>
            ))}
          </>
        )}

        {type === 'bar' && points.map((p, i) => {
          const barWidth = Math.max(8, (chartWidth / points.length) * 0.4);
          const barX = p.x - barWidth / 2;
          const barY = p.y;
          const barH = padding + chartHeight - p.y;
          return (
            <g key={i}>
              <rect x={barX} y={barY} width={barWidth} height={Math.max(2, barH)} fill={color} fillOpacity="0.7" rx="1" />
              <text x={p.x} y={padding + chartHeight + 12} fill="var(--text-secondary)" fontSize="8" textAnchor="middle">{p.label}</text>
              <title>{p.label}: {p.value}</title>
            </g>
          );
        })}

        {/* X-axis Labels (Start, Middle, End) for Line Chart */}
        {type === 'line' && points.length > 1 && [0, Math.floor(points.length / 2), points.length - 1].map((idx) => {
          const p = points[idx];
          if (!p) return null;
          return (
            <text key={idx} x={p.x} y={padding + chartHeight + 12} fill="var(--text-secondary)" fontSize="8" textAnchor={idx === 0 ? 'start' : idx === points.length - 1 ? 'end' : 'middle'}>
              {p.label}
            </text>
          );
        })}

      </svg>
    </div>
  );
}
