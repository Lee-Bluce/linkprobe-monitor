<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { closeDrawer, confirmAction, notify, notifyError, ui } from '../store'
import { fmtLoss, fmtMs, targetOf } from '../format'
import Icon from './Icon.vue'

const router = useRouter()
const editing = computed(() => ui.drawer?.mode === 'edit')
const source = ui.drawer?.node || null

const groups = ref([])
const advanced = ref(false)
const saving = ref(false)
const testing = ref(false)
const error = ref('')
const testResult = ref(null)
const testError = ref('')

const form = reactive({
  name: '',
  group_id: null,
  type: 'icmp',
  host: '',
  port: 443,
  interval_s: 30,
  count: 5,
  timeout_ms: 2000,
  family: 'auto',
  note: '',
  enabled: true,
})

function init() {
  if (editing.value && source) {
    form.name = source.name
    form.group_id = source.group_id ?? null
    form.type = source.type
    form.host = source.host
    form.port = source.port || 443
    form.interval_s = source.interval_s
    form.count = source.count
    form.timeout_ms = source.timeout_ms
    form.family = source.family || 'auto'
    form.note = source.note || ''
    form.enabled = source.enabled
    advanced.value =
      source.interval_s !== 30 ||
      source.count !== 5 ||
      source.timeout_ms !== 2000 ||
      (source.family || 'auto') !== 'auto' ||
      Boolean(source.note)
  } else {
    form.group_id = ui.drawer?.groupId ?? null
  }
}

function payload() {
  return {
    name: form.name.trim(),
    group_id: form.group_id,
    type: form.type,
    host: form.host.trim(),
    port: form.type === 'tcp' ? Number(form.port) : 0,
    interval_s: Number(form.interval_s),
    count: Number(form.count),
    timeout_ms: Number(form.timeout_ms),
    family: form.family,
    note: form.note.trim(),
    enabled: form.enabled,
  }
}

function switchType(type) {
  if (form.type === type) return
  form.type = type
  testResult.value = null
  if (type === 'tcp' && !form.port) form.port = 443
}

async function runTest() {
  testError.value = ''
  testResult.value = null
  testing.value = true
  try {
    testResult.value = await api.testDraft(payload())
  } catch (e) {
    testError.value = e.message
  } finally {
    testing.value = false
  }
}

async function save() {
  error.value = ''
  if (!form.name.trim()) {
    error.value = '给节点起个名字'
    return
  }
  if (!form.host.trim()) {
    error.value = '填写主机地址'
    return
  }
  saving.value = true
  try {
    if (editing.value) {
      await api.updateNode(source.id, payload())
      notify('已保存')
    } else {
      await api.createNode(payload())
      notify('节点已添加')
    }
    window.dispatchEvent(new Event('lp:changed'))
    closeDrawer()
  } catch (e) {
    error.value = e.message
  } finally {
    saving.value = false
  }
}

async function remove() {
  const ok = await confirmAction({
    title: '删除节点',
    message: `将删除「${source.name}」及其全部历史数据，操作不可撤销。`,
    confirmText: '删除',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteNode(source.id)
    notify('节点已删除')
    window.dispatchEvent(new Event('lp:changed'))
    closeDrawer()
    router.push('/')
  } catch (e) {
    notifyError(e)
  }
}

function onKey(e) {
  if (e.key === 'Escape') closeDrawer()
}

onMounted(async () => {
  init()
  document.addEventListener('keydown', onKey)
  try {
    const res = await api.groups()
    groups.value = res.groups || []
  } catch {
    groups.value = []
  }
})
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="overlay" @click.self="closeDrawer"></div>
  <aside class="drawer" role="dialog" aria-modal="true" :aria-label="editing ? '编辑节点' : '添加节点'">
    <div class="drawer-head">
      <span class="drawer-title">{{ editing ? '编辑节点' : '添加节点' }}</span>
      <span v-if="editing" class="mono muted" style="font-size: 11.5px">{{ targetOf(source) }}</span>
      <div style="flex: 1"></div>
      <button class="icon-btn" title="关闭" @click="closeDrawer"><Icon name="close" /></button>
    </div>

    <div class="drawer-body">
      <div class="field">
        <label class="field-label" for="node-name">名称</label>
        <input
          id="node-name"
          v-model="form.name"
          class="input"
          maxlength="64"
          placeholder="例如 家里网关、东京 VPS"
          v-focus
        />
      </div>

      <div class="field">
        <label class="field-label" for="node-group">分组</label>
        <select id="node-group" v-model="form.group_id" class="select">
          <option :value="null">未分组</option>
          <option v-for="g in groups" :key="g.id" :value="g.id">{{ g.name }}</option>
        </select>
      </div>

      <div class="field">
        <span class="field-label">探测方式</span>
        <div class="seg" style="align-self: flex-start">
          <button :aria-pressed="form.type === 'icmp'" @click="switchType('icmp')">ICMP</button>
          <button :aria-pressed="form.type === 'tcp'" @click="switchType('tcp')">TCP 端口</button>
        </div>
      </div>

      <div class="form-row">
        <div class="field" :style="{ gridColumn: form.type === 'tcp' ? '' : '1 / -1' }">
          <label class="field-label" for="node-host">主机</label>
          <input
            id="node-host"
            v-model="form.host"
            class="input mono"
            placeholder="192.168.10.1 或 example.com"
            spellcheck="false"
          />
        </div>
        <div v-if="form.type === 'tcp'" class="field">
          <label class="field-label" for="node-port">端口</label>
          <input id="node-port" v-model.number="form.port" class="input mono" type="number" min="1" max="65535" />
        </div>
      </div>

      <p class="hint">
        ICMP 需要服务具备 CAP_NET_RAW 权限；若目标屏蔽 ICMP，请改用 TCP 端口探测。
      </p>

      <button class="advanced-toggle" @click="advanced = !advanced">
        <Icon :name="advanced ? 'chevron-down' : 'chevron-right'" :size="14" />
        高级设置
      </button>

      <div v-if="advanced" class="advanced">
        <div class="form-row">
          <div class="field">
            <label class="field-label">间隔（秒）</label>
            <input v-model.number="form.interval_s" class="input num" type="number" min="10" max="300" />
          </div>
          <div class="field">
            <label class="field-label">每轮包数</label>
            <input v-model.number="form.count" class="input num" type="number" min="3" max="10" />
          </div>
        </div>
        <div class="form-row">
          <div class="field">
            <label class="field-label">超时（毫秒）</label>
            <input v-model.number="form.timeout_ms" class="input num" type="number" min="1000" max="5000" step="100" />
          </div>
          <div class="field">
            <label class="field-label">协议族</label>
            <select v-model="form.family" class="select">
              <option value="auto">自动</option>
              <option value="4">IPv4</option>
              <option value="6">IPv6</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label class="field-label">备注</label>
          <textarea v-model="form.note" class="textarea" maxlength="200" rows="2"></textarea>
        </div>
        <div class="field">
          <span class="field-label">探测状态</span>
          <button
            class="switch"
            role="switch"
            :aria-checked="form.enabled"
            @click="form.enabled = !form.enabled"
          ></button>
        </div>
        <p class="hint">对 22 等有登录日志的端口，建议间隔 60 秒、每轮 3 包。</p>
      </div>

      <div class="test-box">
        <button class="btn btn-sm" :disabled="testing" @click="runTest">
          <Icon name="activity" :size="14" />
          {{ testing ? '探测中…' : '测试一次' }}
        </button>
        <template v-if="testResult">
          <span v-if="testResult.recv > 0" class="num">
            {{ testResult.recv }}/{{ testResult.sent }} 包 · 平均
            <b>{{ fmtMs(testResult.rtt_avg_ms) }}</b> ms · 丢包
            <b>{{ fmtLoss(testResult.loss) }}</b>
          </span>
          <span v-else class="bad">
            {{ testResult.sent }} 个包均未收到（超时、被屏蔽或被拒绝）
          </span>
        </template>
        <span v-else-if="testError" class="bad">{{ testError }}</span>
        <span v-else class="muted">可以先测试连通性，再保存。</span>
      </div>

      <p v-if="error" class="hint hint-error">{{ error }}</p>
    </div>

    <div class="drawer-foot">
      <button v-if="editing" class="btn btn-danger" @click="remove">
        <Icon name="trash" :size="14" />删除
      </button>
      <div class="spacer"></div>
      <button class="btn" @click="closeDrawer">取消</button>
      <button class="btn btn-primary" :disabled="saving" @click="save">
        {{ saving ? '保存中…' : '保存' }}
      </button>
    </div>
  </aside>
</template>
