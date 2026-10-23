import { reactive } from 'vue'

export const ui = reactive({
  toasts: [],
  confirm: null,
  drawer: null, // { mode: 'create' | 'edit', node, groupId }
  groupManager: false,
})

let toastSeq = 0

export function notify(message, type = 'info', ttl = 4000) {
  const id = ++toastSeq
  ui.toasts.push({ id, message, type })
  window.setTimeout(() => {
    const i = ui.toasts.findIndex((t) => t.id === id)
    if (i >= 0) ui.toasts.splice(i, 1)
  }, ttl)
}

export function notifyError(err) {
  const message = err?.message || '操作失败'
  notify(message, 'error')
}

export function confirmAction(options) {
  return new Promise((resolve) => {
    ui.confirm = { ...options, resolve }
  })
}

export function resolveConfirm(value) {
  const c = ui.confirm
  ui.confirm = null
  if (c) c.resolve(value)
}

export function openDrawer(mode, node = null, groupId = null) {
  ui.drawer = { mode, node, groupId }
}

export function closeDrawer() {
  ui.drawer = null
}
