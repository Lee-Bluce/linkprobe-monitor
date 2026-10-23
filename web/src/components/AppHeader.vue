<script setup>
import Icon from './Icon.vue'
import { ui, openDrawer } from '../store'

const emit = defineEmits(['logout'])

function addNode() {
  openDrawer('create', null, null)
}
</script>

<template>
  <header class="topbar">
    <div class="topbar-inner">
      <router-link to="/" class="brand" aria-label="返回总览">
        <span class="brand-mark">链路观测</span>
        <span class="brand-sub">linkprobe</span>
      </router-link>
      <div class="topbar-spacer"></div>
      <div class="topbar-actions">
        <button class="btn btn-ghost" @click="ui.groupManager = true">分组</button>
        <button class="btn btn-primary" @click="addNode">
          <Icon name="plus" :size="15" />
          <span>添加节点</span>
        </button>
        <button class="icon-btn" title="退出登录" @click="emit('logout')">
          <Icon name="logout" />
        </button>
      </div>
    </div>
  </header>
</template>
