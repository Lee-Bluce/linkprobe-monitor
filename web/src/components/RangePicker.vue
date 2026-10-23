<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RANGES, fromLocalInput, toLocalInput } from '../format'

const props = defineProps({
  range: { type: String, default: '24h' },
  from: { type: Number, default: 0 },
  to: { type: Number, default: 0 },
})
const emit = defineEmits(['change'])

const open = ref(false)
const f = ref('')
const t = ref('')
const error = ref('')
const wrap = ref(null)

function syncInputs() {
  if (props.range === 'custom' && props.from && props.to) {
    f.value = toLocalInput(props.from)
    t.value = toLocalInput(props.to)
  } else if (!f.value) {
    const now = Math.floor(Date.now() / 1000)
    f.value = toLocalInput(now - 86400)
    t.value = toLocalInput(now)
  }
}

watch(() => [props.range, props.from, props.to], syncInputs, { immediate: true })

function pick(key) {
  open.value = false
  error.value = ''
  emit('change', { range: key })
}

function toggleCustom() {
  if (open.value) {
    open.value = false
    return
  }
  syncInputs()
  const now = Math.floor(Date.now() / 1000)
  if (!props.from) {
    f.value = toLocalInput(now - 86400)
    t.value = toLocalInput(now)
  }
  open.value = true
}

function apply() {
  const from = fromLocalInput(f.value)
  const to = fromLocalInput(t.value)
  if (!from || !to) {
    error.value = '请填写起止时间'
    return
  }
  if (to <= from) {
    error.value = '结束时间需晚于开始时间'
    return
  }
  if (to - from < 60) {
    error.value = '范围至少一分钟'
    return
  }
  error.value = ''
  open.value = false
  emit('change', { range: 'custom', from, to })
}

function onDocClick(e) {
  if (open.value && wrap.value && !wrap.value.contains(e.target)) open.value = false
}

onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <div ref="wrap" class="range-wrap" @click.stop>
    <div class="range" role="group" aria-label="时间范围">
      <button
        v-for="r in RANGES"
        :key="r.key"
        :aria-pressed="range === r.key"
        @click="pick(r.key)"
      >
        {{ r.label }}
      </button>
      <button class="custom-btn" :aria-pressed="range === 'custom'" @click="toggleCustom">
        自定义
      </button>
    </div>

    <div v-if="open" class="range-panel">
      <div class="row">
        <div class="field">
          <label class="field-label">开始</label>
          <input v-model="f" class="input" type="datetime-local" />
        </div>
        <div class="field">
          <label class="field-label">结束</label>
          <input v-model="t" class="input" type="datetime-local" />
        </div>
      </div>
      <p v-if="error" class="hint hint-error">{{ error }}</p>
      <p v-else class="hint">在详情页的图上拖动也可框选范围</p>
      <div class="actions">
        <button class="btn btn-sm" @click="open = false">取消</button>
        <button class="btn btn-sm btn-primary" @click="apply">应用</button>
      </div>
    </div>
  </div>
</template>
