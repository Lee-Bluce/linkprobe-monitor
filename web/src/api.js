const JSON_HEADERS = { 'Content-Type': 'application/json' }

export class ApiError extends Error {
  constructor(message, code, status) {
    super(message)
    this.code = code
    this.status = status
  }
}

async function request(path, options = {}) {
  const opts = {
    credentials: 'same-origin',
    ...options,
    headers: { ...(options.body ? JSON_HEADERS : {}), ...(options.headers || {}) },
  }
  let res
  try {
    res = await fetch(path, opts)
  } catch {
    throw new ApiError('无法连接到服务', 'offline', 0)
  }
  if (res.status === 401) {
    window.dispatchEvent(new Event('lp:unauthorized'))
    throw new ApiError('登录已失效', 'unauthorized', 401)
  }
  const text = await res.text()
  let data = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }
  if (!res.ok) {
    const message = data?.error?.message || `请求失败（${res.status}）`
    throw new ApiError(message, data?.error?.code, res.status)
  }
  return data
}

export const api = {
  session: () => request('/api/session'),
  login: (password) => request('/api/login', { method: 'POST', body: JSON.stringify({ password }) }),
  logout: () => request('/api/logout', { method: 'POST' }),

  overview: (query) => request(`/api/overview?${query}`),
  series: (id, query) => request(`/api/nodes/${id}/series?${query}`),
  system: () => request('/api/system'),

  groups: () => request('/api/groups'),
  createGroup: (name) => request('/api/groups', { method: 'POST', body: JSON.stringify({ name }) }),
  renameGroup: (id, name) =>
    request(`/api/groups/${id}`, { method: 'PATCH', body: JSON.stringify({ name }) }),
  deleteGroup: (id) => request(`/api/groups/${id}`, { method: 'DELETE' }),
  orderGroups: (ids) =>
    request('/api/groups/order', { method: 'PUT', body: JSON.stringify({ ids }) }),

  nodes: () => request('/api/nodes'),
  node: (id) => request(`/api/nodes/${id}`),
  createNode: (payload) => request('/api/nodes', { method: 'POST', body: JSON.stringify(payload) }),
  updateNode: (id, payload) =>
    request(`/api/nodes/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),
  deleteNode: (id) => request(`/api/nodes/${id}`, { method: 'DELETE' }),
  orderNodes: (items) =>
    request('/api/nodes/order', { method: 'PUT', body: JSON.stringify({ items }) }),
  testNode: (id) => request(`/api/nodes/${id}/test`, { method: 'POST' }),
  testDraft: (payload) => request('/api/probe/test', { method: 'POST', body: JSON.stringify(payload) }),
}
