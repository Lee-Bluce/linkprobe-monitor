<script setup>
import { ui, resolveConfirm } from '../store'

function cancel() {
  resolveConfirm(false)
}
</script>

<template>
  <div class="overlay" @click.self="cancel"></div>
  <div class="modal" role="dialog" aria-modal="true">
    <div class="modal-head">{{ ui.confirm.title || '确认操作' }}</div>
    <div class="modal-body">
      <p class="dim">{{ ui.confirm.message }}</p>
    </div>
    <div class="modal-foot">
      <button class="btn" @click="cancel">取消</button>
      <button
        class="btn"
        :class="ui.confirm.danger ? 'btn-danger' : 'btn-primary'"
        @click="resolveConfirm(true)"
      >
        {{ ui.confirm.confirmText || '确定' }}
      </button>
    </div>
  </div>
</template>
