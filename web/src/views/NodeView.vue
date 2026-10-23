<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import DualChart from '../components/DualChart.vue'
import Icon from '../components/Icon.vue'
import RangePicker from '../components/RangePicker.vue'
import { confirmAction, notify, notifyError, openDrawer } from '../store'
import {
  STATUS_TEXT,
  fmtLoss,
  fmtMs,
  fmtPercent,
  fmtTime,
  rangeQuery,
  targetOf,
  typeLabel,
} from '../format'

const route = useRoute()
const router = useRouter()

const data = ref(null)
const loading = ref(true)
const error = ref('')
const busy = ref(false)

let timer = null

const nodeId = computed(() => Number(route.params.id))
const range = computed(() => (route.query.from && route.query.to ? 'custom' : route.query.range || '24h'))
const from = computed(() => Number(route.query.from || 0))
const to = computed(() => Number(route.query.to || 0))
const query = computed(() =>
  route.query.from && route.query.to
    ? rangeQuery('custom', Number(route.query.from), Number(route.query.to))
    : rangeQuery(range.value, 0, 0),
)
const node = computed(() => data.value?.node || null)
const status = computed(() => data.value?.status || 'unknown')
const summary = computed(() => data.value?.summary || null)

async function load(skeleton = false) {
  if (skeleton) loading.value = true
  error.value = ''
  try {
    data.value = await api.series(nodeId.value, query.value)
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

function onBrush({ from: f, to: t }) {
  router.replace({ query: { from: String(f), to: String(t) } })
}

function edit() {
  openDrawer('edit', node.value)
}

async function toggle() {
  if (!node.value) return
  busy.value = true
  try {
    await api.updateNode(node.value.id, { enabled: !node.value.enabled })
    notify(node.value.enabled ? '已暂停探测' : '已恢复探测')
    await load()
  } catch (e) {
    notifyError(e)
  } finally {
    busy.value = false
  }
}

async function remove() {
  if (!node.value) return
  const ok = await confirmAction({
    title: '删除节点',
    message: `将删除「${node.value.name}」及其全部历史数据，操作不可撤销。`,
    confirmText: '删除',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteNode(node.value.id)
    notify('节点已删除')
    router.push('/')
  } catch (e) {
    notifyError(e)
  }
}

function onChanged() {
  load()
}

onMounted(() => {
  load(true)
  timer = window.setInterval(() => {
    if (!document.hidden) load()
  }, 30000)
  window.addEventListener('lp:changed', onChanged)
})

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer)
  window.removeEventListener('lp:changed', onChanged)
})

watch(
  () => [route.params.id, route.query],
  () => load(),
)
</script>

<template>
  <main class="page">
    <router-link to="/" class="back-link"><Icon name="back" :size="14" />返回总览</router-link>

    <div v-if="loading && !data" class="skeleton" style="height: 30px; width: 240px"></div>

    <div v-else-if="error && !data" class="empty">
      <p class="empty-title">无法读取这个节点</p>
      <p class="empty-body">{{ error }}</p>
      <button class="btn" style="margin-top: 16px" @click="load(true)">重试</button>
    </div>

    <template v-else-if="node">
      <div class="detail-head">
        <div>
          <div class="detail-title">
            <span class="dot" :class="status"></span>
            <span>{{ node.name }}</span>
          </div>
          <div class="detail-target">
            {{ targetOf(node) }} · {{ typeLabel(node.type) }}
            <template v-if="node.note"> · {{ node.note }}</template>
          </div>
        </div>
        <div class="detail-actions">
          <button class="btn" @click="edit"><Icon name="edit" :size="14" />编辑</button>
          <button class="btn" :disabled="busy" @click="toggle">
            <Icon :name="node.enabled ? 'pause' : 'play'" :size="14" />
            {{ node.enabled ? '暂停' : '恢复' }}
          </button>
          <button class="btn btn-ghost" @click="remove"><Icon name="trash" :size="14" />删除</button>
        </div>
      </div>

      <div
        style="
          display: flex;
          align-items: center;
          justify-content: space-between;
          gap: 12px;
          flex-wrap: wrap;
          margin-top: 20px;
        "
      >
        <div class="summary-line">
          <span class="status" :class="status">
            <span class="dot" :class="status"></span>{{ STATUS_TEXT[status] || status }}
          </span>
          <span class="sep"></span>
          <span>更新于 {{ fmtTime(data.generated_at, false) }}</span>
          <span class="sep"></span>
          <span>{{ data.layer === 'raw' ? '原始数据' : data.layer === '5m' ? '5 分钟聚合' : '1 小时聚合' }} · 粒度 {{ data.res >= 3600 ? data.res / 3600 + ' 小时' : data.res / 60 + ' 分钟' }}</span>
        </div>
        <RangePicker :range="range" :from="from || data.from" :to="to || data.to" @change="changeRange" />
      </div>

      <div class="stat-strip">
        <div class="stat">
          <div class="stat-label">丢包率</div>
          <div class="stat-value">{{ fmtLoss(summary?.loss) }}</div>
          <div class="stat-note">
            <template v-if="summary?.worst_loss > 0">
              最差 {{ fmtLoss(summary.worst_loss, 0) }} · {{ fmtTime(summary.worst_loss_ts) }}
            </template>
            <template v-else>范围内无丢包</template>
          </div>
        </div>
        <div class="stat">
          <div class="stat-label">平均延迟</div>
          <div class="stat-value">
            {{ fmtMs(summary?.avg_ms) }}<span v-if="summary?.avg_ms != null" class="unit">ms</span>
          </div>
          <div class="stat-note">按收包加权</div>
        </div>
        <div class="stat">
          <div class="stat-label">最低延迟</div>
          <div class="stat-value">
            {{ fmtMs(summary?.min_ms) }}<span v-if="summary?.min_ms != null" class="unit">ms</span>
          </div>
          <div class="stat-note">范围内单次最小值</div>
        </div>
        <div class="stat">
          <div class="stat-label">最高延迟</div>
          <div class="stat-value">
            {{ fmtMs(summary?.max_ms) }}<span v-if="summary?.max_ms != null" class="unit">ms</span>
          </div>
          <div class="stat-note">范围内单次最大值</div>
        </div>
        <div class="stat">
          <div class="stat-label">样本完整度</div>
          <div class="stat-value">{{ fmtPercent(summary?.coverage) }}</div>
          <div class="stat-note">
            {{ summary?.buckets || 0 }} / {{ summary?.expected_buckets || 0 }} 个时间片
          </div>
        </div>
      </div>

      <DualChart
        :points="data.points"
        :from="data.from"
        :to="data.to"
        :res="data.res"
        @brush="onBrush"
      />

      <p class="hint" style="margin-top: 12px">
        灰色斜纹为无数据区段（服务停止或盒子离线）；延迟线在完全丢包处断开，不会画成 0。
      </p>
    </template>
  </main>
</template>
