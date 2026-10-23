<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { confirmAction, notify, notifyError, openDrawer } from '../store'
import { STATUS_TEXT, fmtLoss, fmtMs, rangeQuery, targetOf } from '../format'
import Icon from './Icon.vue'
import Sparkline from './Sparkline.vue'

const props = defineProps({
  node: { type: Object, required: true },
  from: { type: Number, required: true },
  to: { type: Number, required: true },
  range: { type: String, default: '24h' },
})

const router = useRouter()
const menuOpen = ref(false)
const busy = ref(false)
const root = ref(null)

function open() {
  router.push({ path: `/node/${props.node.id}`, query: parseQuery() })
}

function parseQuery() {
  const q = {}
  const parts = rangeQuery(props.range, props.from, props.to).split('&')
  for (const part of parts) {
    const [k, v] = part.split('=')
    q[k] = v
  }
  return q
}

function edit() {
  menuOpen.value = false
  openDrawer('edit', props.node)
}

async function toggle() {
  menuOpen.value = false
  busy.value = true
  try {
    await api.updateNode(props.node.id, { enabled: !props.node.enabled })
    notify(props.node.enabled ? '已暂停探测' : '已恢复探测')
    window.dispatchEvent(new Event('lp:changed'))
  } catch (e) {
    notifyError(e)
  } finally {
    busy.value = false
  }
}

async function remove() {
  menuOpen.value = false
  const ok = await confirmAction({
    title: '删除节点',
    message: `将删除「${props.node.name}」及其全部历史数据，操作不可撤销。`,
    confirmText: '删除',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteNode(props.node.id)
    notify('节点已删除')
    window.dispatchEvent(new Event('lp:changed'))
  } catch (e) {
    notifyError(e)
  }
}

function onDocClick(e) {
  if (menuOpen.value && root.value && !root.value.contains(e.target)) menuOpen.value = false
}

onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <article
    ref="root"
    class="node-card"
    :draggable="true"
    role="link"
    tabindex="0"
    :aria-label="`${node.name}，${STATUS_TEXT[node.status] || node.status}，打开详情`"
    @click="open"
    @keydown.enter="open"
    @keydown.space.prevent="open"
    @dragstart="$emit('drag-start', $event, node)"
    @dragend="$emit('drag-end', $event)"
  >
    <div class="node-card-head">
      <span class="dot" :class="node.status"></span>
      <span class="node-name" :title="node.name">{{ node.name }}</span>
      <div class="card-menu" @click.stop>
        <button
          class="icon-btn"
          title="更多操作"
          aria-haspopup="menu"
          :aria-expanded="menuOpen"
          @click="menuOpen = !menuOpen"
        >
          <Icon name="more" />
        </button>
        <div v-if="menuOpen" class="menu">
          <button @click="edit"><Icon name="edit" :size="14" />编辑</button>
          <button :disabled="busy" @click="toggle">
            <Icon :name="node.enabled ? 'pause' : 'play'" :size="14" />
            {{ node.enabled ? '暂停探测' : '恢复探测' }}
          </button>
          <div class="menu-sep"></div>
          <button class="danger" @click="remove"><Icon name="trash" :size="14" />删除</button>
        </div>
      </div>
    </div>
    <div class="node-target" :title="targetOf(node)">{{ targetOf(node) }}</div>

    <div class="node-body">
      <div class="node-rtt num" :class="{ none: node.last_rtt_ms === null }">
        <template v-if="node.last_rtt_ms !== null">
          {{ fmtMs(node.last_rtt_ms) }}<span class="unit">ms</span>
        </template>
        <template v-else>—</template>
      </div>
      <div class="node-stats">
        <span>丢包 <b>{{ fmtLoss(node.summary?.loss) }}</b></span>
        <span>平均 <b>{{ fmtMs(node.summary?.avg_ms) }}</b><template v-if="node.summary?.avg_ms !== null && node.summary?.avg_ms !== undefined"> ms</template></span>
      </div>
    </div>

    <Sparkline :points="node.spark || []" :from="from" :to="to" />

    <div class="status" :class="node.status" style="margin-top: 4px">
      {{ STATUS_TEXT[node.status] || node.status }}
      <span v-if="node.note" class="muted" style="overflow: hidden; text-overflow: ellipsis"
        >· {{ node.note }}</span
      >
    </div>
  </article>
</template>
