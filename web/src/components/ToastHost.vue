<script setup>
import { ui } from '../store'
</script>

<template>
  <div class="toasts" aria-live="polite">
    <div v-for="t in ui.toasts" :key="t.id" class="toast" :class="t.type">{{ t.message }}</div>
  </div>
</template>
