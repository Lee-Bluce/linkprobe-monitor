<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from './api'
import AppHeader from './components/AppHeader.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import GroupManager from './components/GroupManager.vue'
import NodeDrawer from './components/NodeDrawer.vue'
import ToastHost from './components/ToastHost.vue'
import LoginView from './views/LoginView.vue'
import { ui } from './store'

const checking = ref(true)
const authed = ref(false)

async function checkSession() {
  try {
    const s = await api.session()
    authed.value = Boolean(s?.authenticated)
  } catch {
    authed.value = false
  } finally {
    checking.value = false
  }
}

function onUnauthorized() {
  authed.value = false
  ui.drawer = null
  ui.groupManager = false
}

async function logout() {
  try {
    await api.logout()
  } catch {
    /* ignore */
  }
  authed.value = false
}

onMounted(() => {
  checkSession()
  window.addEventListener('lp:unauthorized', onUnauthorized)
})
onBeforeUnmount(() => window.removeEventListener('lp:unauthorized', onUnauthorized))
</script>

<template>
  <div v-if="checking" class="login-wrap">
    <div class="skeleton" style="width: 220px; height: 12px"></div>
  </div>

  <template v-else-if="!authed">
    <LoginView @success="authed = true" />
    <ToastHost />
  </template>

  <div v-else class="shell">
    <AppHeader @logout="logout" />
    <router-view />
  </div>

  <NodeDrawer v-if="authed && ui.drawer" />
  <GroupManager v-if="authed && ui.groupManager" />
  <ConfirmDialog v-if="ui.confirm" />
  <ToastHost />
</template>
