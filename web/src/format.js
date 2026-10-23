export function fmtMs(ms, digits = 1) {
  if (ms === null || ms === undefined || Number.isNaN(ms)) return '—'
  if (ms >= 1000) return (ms / 1000).toFixed(2) + ' s'
  if (ms >= 100) return ms.toFixed(0)
  if (ms >= 10) return ms.toFixed(1)
  return ms.toFixed(digits)
}

export function fmtLoss(ratio, digits = 1) {
  if (ratio === null || ratio === undefined || Number.isNaN(ratio)) return '—'
  const pct = ratio * 100
  if (pct === 0) return '0%'
  if (pct < 0.05) return '<0.1%'
  if (pct < 10) return pct.toFixed(digits) + '%'
  return pct.toFixed(0) + '%'
}

export function fmtPercent(ratio) {
  if (ratio === null || ratio === undefined) return '—'
  const pct = ratio * 100
  if (pct > 0 && pct < 10) return pct.toFixed(1) + '%'
  return Math.round(pct) + '%'
}

export function pad(n) {
  return n < 10 ? '0' + n : String(n)
}

export function fmtTime(ts, withDate = true) {
  if (!ts) return '—'
  const d = new Date(ts * 1000)
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  if (!withDate) return hm
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hm}`
}

export function fmtDateTime(ts) {
  if (!ts) return '—'
  const d = new Date(ts * 1000)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function toLocalInput(ts) {
  const d = new Date(ts * 1000)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`
}

export function fromLocalInput(value) {
  if (!value) return null
  const t = new Date(value).getTime()
  if (Number.isNaN(t)) return null
  return Math.floor(t / 1000)
}

export const RANGES = [
  { key: '1h', label: '1 小时' },
  { key: '6h', label: '6 小时' },
  { key: '24h', label: '24 小时' },
  { key: '3d', label: '3 天' },
  { key: '7d', label: '7 天' },
  { key: '30d', label: '30 天' },
  { key: '90d', label: '90 天' },
  { key: '1y', label: '1 年' },
]

export const STATUS_TEXT = {
  ok: '正常',
  degraded: '劣化',
  offline: '离线',
  unknown: '无数据',
  paused: '已暂停',
}

export function targetOf(node) {
  if (!node) return ''
  if (node.type === 'tcp') return `${node.host}:${node.port}`
  return node.host
}

export function typeLabel(type) {
  return type === 'tcp' ? 'TCP' : 'ICMP'
}

export function rangeQuery(range, from, to) {
  if (range === 'custom' && from && to) return `from=${from}&to=${to}`
  return `range=${range || '24h'}`
}

export function rangeLabel(plan) {
  if (!plan) return ''
  if (plan.range === 'custom' || plan.from) {
    return `${fmtTime(plan.from)} — ${fmtTime(plan.to)}`
  }
  const found = RANGES.find((r) => r.key === plan.range)
  return found ? found.label : '24 小时'
}
