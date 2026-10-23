<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import Icon from '../components/Icon.vue'
import NodeCard from '../components/NodeCard.vue'
import RangePicker from '../components/RangePicker.vue'
import { notify, notifyError, confirmAction, openDrawer } from '../store'
import { fmtTime, rangeQuery } from '../format'

const route = useRoute()
const router = useRouter()

const data = ref(null)
const loading = ref(true)
const error = ref('')
const system = ref(null)
const renaming = ref(null)
const renameValue = ref('')
const dragging = ref(null)
const overGroup = ref(null)
const overNode = ref(null)

let timer = null

const range = computed(() => (route.query.from && route.query.to ? 'custom' : route.query.range || '24h'))
const from = computed(() => Number(route.query.from || data.value?.from || 0))
const to = computed(() => Number(route.query.to || data.value?.to || 0))
const query = computed(() =>
  route.query.from && route.query.to
    ? rangeQuery('custom', Number(route.query.from), Number(route.query.to))
    : rangeQuery(range.value, 0, 0),
)

const allGroups = computed(() => {
  if (!data.value) return []
  const groups = data.value.groups || []
  if ((data.value.ungrouped || []).length) {
    return [...groups, { id: null, name: '未分组', nodes: data.value.ungrouped }]
  }
  return groups
})

const hasNodes = computed(() => (data.value?.totals?.nodes || 0) > 0)

const COLLAPSE_KEY = 'lp.collapsed'
const collapsed = ref(readCollapsed())

function readCollapsed() {
  try {
    const raw = localStorage.getItem(COLLAPSE_KEY)
    return raw ? JSON.parse(raw) : {}
  } catch {
    return {}
  }
}

function groupKey(group) {
  return group.id === null || group.id === undefined ? 'ungrouped' : String(group.id)
}

function isCollapsed(group) {
  return !!collapsed.value[groupKey(group)]
}

function isRenaming(group) {
  return group.id !== null && group.id !== undefined && renaming.value === group.id
}

function toggleFromName(group) {
  if (isRenaming(group)) return
  toggleGroup(group)
}

function toggleGroup(group) {
  const key = groupKey(group)
  const next = { ...collapsed.value, [key]: !collapsed.value[key] }
  collapsed.value = next
  try {
    localStorage.setItem(COLLAPSE_KEY, JSON.stringify(next))
  } catch {
    /* storage disabled: keep the in-memory state */
  }
}

async function load(showSkeleton = false) {
  if (showSkeleton) loading.value = true
  error.value = ''
  try {
    data.value = await api.overview(query.value)
    system.value = await api.system().catch(() => null)
  } catch (e) {
    error.value = e.message || '加载失败'
  } finally {
    loading.value = false
  }
}

function changeRange(next) {
  const q = next.range === 'custom' ? { from: String(next.from), to: String(next.to) } : { range: next.range }
  router.replace({ query: q })
}

function nodesOf(groupId) {
  if (!data.value) return []
  if (groupId === null) return data.value.ungrouped || []
  const g = data.value.groups.find((x) => x.id === groupId)
  return g ? g.nodes : []
}

function startDrag(e, node) {
  dragging.value = node
  e.dataTransfer.effectAllowed = 'move'
  e.dataTransfer.setData('text/plain', String(node.id))
  e.currentTarget.classList.add('dragging')
}

function endDrag(e) {
  if (e?.currentTarget) e.currentTarget.classList.remove('dragging')
  dragging.value = null
  overGroup.value = null
  overNode.value = null
}

async function saveOrder(list, groupId) {
  const items = list.map((n, i) => ({ id: n.id, group_id: groupId, sort: i }))
  try {
    await api.orderNodes(items)
    notify('顺序已更新')
    await load()
  } catch (e) {
    notifyError(e)
  }
}

async function dropOnGroup(groupId) {
  if (!dragging.value) return
  const node = dragging.value
  if ((node.group_id ?? null) === groupId) {
    endDrag()
    return
  }
  const list = nodesOf(groupId).filter((n) => n.id !== node.id)
  list.push(node)
  await saveOrder(list, groupId)
  endDrag()
}

async function dropOnNode(target) {
  if (!dragging.value || dragging.value.id === target.id) return
  const groupId = target.group_id ?? null
  const list = nodesOf(groupId).filter((n) => n.id !== dragging.value.id)
  const idx = list.findIndex((n) => n.id === target.id)
  list.splice(idx < 0 ? list.length : idx, 0, dragging.value)
  await saveOrder(list, groupId)
  endDrag()
}

function beginRename(group) {
  renaming.value = group.id
  renameValue.value = group.name
}

async function commitRename(group) {
  const name = renameValue.value.trim()
  renaming.value = null
  if (!name || name === group.name) return
  try {
    await api.renameGroup(group.id, name)
    await load()
  } catch (e) {
    notifyError(e)
  }
}

async function removeGroup(group) {
  const ok = await confirmAction({
    title: '删除分组',
    message: `「${group.name}」内的节点会移到未分组，节点本身和历史数据都会保留。`,
    confirmText: '删除分组',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteGroup(group.id)
    notify('分组已删除')
    await load()
  } catch (e) {
    notifyError(e)
  }
}

function addNode(groupId) {
  openDrawer('create', null, groupId)
}

function onChanged() {
  load()
}

function onVisibility() {
  if (!document.hidden) load()
}

onMounted(() => {
  load(true)
  timer = window.setInterval(() => {
    if (!document.hidden) load()
  }, 30000)
  window.addEventListener('lp:changed', onChanged)
  document.addEventListener('visibilitychange', onVisibility)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
  window.removeEventListener('lp:changed', onChanged)
  document.removeEventListener('visibilitychange', onVisibility)
})

watch(
  () => route.query,
  () => load(),
)
</script>

<template>
  <main class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">总览</h1>
        <p class="page-sub summary-line">
          <template v-if="data">
            <span><strong>{{ data.totals.nodes }}</strong> 个节点</span>
            <span class="sep"></span>
            <span v-if="data.totals.degraded"><strong>{{ data.totals.degraded }}</strong> 劣化</span>
            <span v-if="data.totals.offline"><strong>{{ data.totals.offline }}</strong> 离线</span>
            <span v-if="!data.totals.degraded && !data.totals.offline">全部正常</span>
            <span class="sep"></span>
            <span>更新于 {{ fmtTime(data.generated_at, false) }}</span>
          </template>
          <template v-else>正在读取…</template>
        </p>
      </div>
      <RangePicker :range="range" :from="from" :to="to" @change="changeRange" />
    </div>

    <div v-if="data?.warning" class="panel" style="padding: 10px 14px; margin-bottom: 16px">
      <span class="hint" style="display: flex; gap: 8px; align-items: center">
        <Icon name="info" :size="14" />{{ data.warning }}
      </span>
    </div>

    <div v-if="loading && !data" class="node-grid">
      <div v-for="i in 6" :key="i" class="skeleton" style="height: 148px"></div>
    </div>

    <div v-else-if="error && !data" class="empty">
      <p class="empty-title">数据加载失败</p>
      <p class="empty-body">{{ error }}</p>
      <button class="btn" style="margin-top: 16px" @click="load(true)">重试</button>
    </div>

    <div v-else-if="!hasNodes" class="empty">
      <p class="empty-title">还没有节点</p>
      <p class="empty-body">添加一个服务器或网关，开始记录它的延迟与丢包。</p>
      <button class="btn btn-primary" style="margin-top: 18px" @click="addNode(null)">
        <Icon name="plus" :size="15" />添加节点
      </button>
    </div>

    <template v-else>
      <section v-for="group in allGroups" :key="group.id ?? 'ungrouped'" class="group-block">
        <div class="group-head">
          <button
            class="group-toggle"
            :aria-expanded="!isCollapsed(group)"
            :aria-label="isCollapsed(group) ? `展开 ${group.name}` : `收起 ${group.name}`"
            :title="isCollapsed(group) ? '展开分组' : '收起分组'"
            @click="toggleGroup(group)"
          >
            <Icon :name="isCollapsed(group) ? 'chevron-right' : 'chevron-down'" :size="14" />
          </button>
          <span class="group-name" :class="{ clickable: !isRenaming(group) }" @click="toggleFromName(group)">
            <input
              v-if="isRenaming(group)"
              v-model="renameValue"
              class="input"
              style="height: 26px; width: 160px; font-size: 13px"
              @click.stop
              @keyup.enter="commitRename(group)"
              @keyup.esc="renaming = null"
              @blur="commitRename(group)"
              v-focus
            />
            <template v-else>{{ group.name }}</template>
          </span>
          <span class="group-count">{{ group.nodes.length }}</span>
          <div class="group-tools">
            <button class="icon-btn" title="添加到此分组" @click="addNode(group.id)">
              <Icon name="plus" :size="14" />
            </button>
            <template v-if="group.id !== null">
              <button class="icon-btn" title="重命名" @click="beginRename(group)">
                <Icon name="edit" :size="14" />
              </button>
              <button class="icon-btn" title="删除分组" @click="removeGroup(group)">
                <Icon name="trash" :size="14" />
              </button>
            </template>
          </div>
        </div>

        <div
          v-show="!isCollapsed(group)"
          class="group-drop"
          :class="{ over: overGroup === (group.id ?? null) }"
          @dragover.prevent="overGroup = group.id ?? null"
          @dragleave="overGroup = null"
          @drop.prevent="dropOnGroup(group.id ?? null)"
        >
          <div v-if="!group.nodes.length" class="node-grid empty">
            空分组，把节点拖到这里，或点击右上角添加。
          </div>
          <div v-else class="node-grid">
            <div
              v-for="node in group.nodes"
              :key="node.id"
              class="node-slot"
              :class="{ 'drop-before': overNode === node.id }"
              @dragover.prevent.stop="overNode = node.id"
              @dragleave="overNode = null"
              @drop.prevent.stop="dropOnNode(node)"
            >
              <NodeCard
                :node="node"
                :from="data.from"
                :to="data.to"
                :range="range"
                @drag-start="startDrag"
                @drag-end="endDrag"
              />
            </div>
          </div>
        </div>
      </section>

      <footer class="summary-line" style="margin-top: 40px">
        <span v-if="system">数据 {{ (system.db_size / 1048576).toFixed(1) }} MB</span>
        <span v-if="system" class="sep"></span>
        <span v-if="system">版本 {{ system.version }}</span>
        <span class="sep"></span>
        <span>每 30 秒自动刷新</span>
      </footer>
    </template>
  </main>
</template>
