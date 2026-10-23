<script setup>
import { onMounted, ref } from 'vue'
import { api } from '../api'
import Icon from '../components/Icon.vue'

const emit = defineEmits(['success'])
const password = ref('')
const busy = ref(false)
const error = ref('')
const version = ref('')
const input = ref(null)

onMounted(async () => {
  input.value?.focus()
  try {
    const s = await api.session()
    version.value = s?.version || ''
  } catch {
    /* ignore */
  }
})

async function submit() {
  if (busy.value) return
  error.value = ''
  if (!password.value) {
    error.value = '请输入管理密码'
    return
  }
  busy.value = true
  try {
    await api.login(password.value)
    password.value = ''
    emit('success')
  } catch (e) {
    error.value = e.message || '登录失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <div class="login-card">
      <div class="login-brand">
        <div class="brand">
          <span class="brand-mark">链路观测</span>
          <span class="brand-sub">linkprobe</span>
        </div>
        <div class="login-sub">局域网内的链路质量记录。输入管理密码继续。</div>
      </div>

      <form class="login-form" @submit.prevent="submit">
        <div class="field">
          <label class="field-label" for="pwd">管理密码</label>
          <input
            id="pwd"
            ref="input"
            v-model="password"
            class="input"
            :class="{ 'input-error': error }"
            type="password"
            autocomplete="current-password"
            autofocus
          />
        </div>
        <p v-if="error" class="hint hint-error">{{ error }}</p>
        <button class="btn btn-primary" type="submit" :disabled="busy" style="height: 36px">
          {{ busy ? '验证中…' : '进入' }}
        </button>
      </form>

      <div class="login-foot">
        <span>自建服务 · 数据仅保存在本机</span>
        <span v-if="version">{{ version }}</span>
      </div>
    </div>
  </div>
</template>
