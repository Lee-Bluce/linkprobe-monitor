<script setup>
import { onMounted, ref } from 'vue'
import { api } from '../api'
import { confirmAction, notify, notifyError, ui } from '../store'
import Icon from './Icon.vue'

const list = ref([])
const newName = ref('')
const dragIndex = ref(-1)
const overIndex = ref(-1)
const busy = ref(false)

async function load() {
  try {
    const res = await api.groups()
    list.value = (res.groups || []).map((g) => ({ ...g, draft: g.name }))
  } catch (e) {
    notifyError(e)
  }
}

async function commitRename(group) {
  const name = group.draft.trim()
  if (!name || name === group.name) {
    group.draft = group.name
    return
  }
  try {
    await api.renameGroup(group.id, name)
    group.name = name
    notify('分组已重命名')
    window.dispatchEvent(new Event('lp:changed'))
  } catch (e) {
    group.draft = group.name
    notifyError(e)
  }
}

async function add() {
  const name = newName.value.trim()
  if (!name) return
  busy.value = true
  try {
    await api.createGroup(name)
    newName.value = ''
    notify('分组已创建')
    await load()
    window.dispatchEvent(new Event('lp:changed'))
  } catch (e) {
    notifyError(e)
  } finally {
    busy.value = false
  }
}

async function remove(group) {
  const ok = await confirmAction({
    title: '删除分组',
    message: `「${group.name}」内的节点会移到未分组，节点和历史数据都会保留。`,
    confirmText: '删除分组',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteGroup(group.id)
    notify('分组已删除')
    await load()
    window.dispatchEvent(new Event('lp:changed'))
  } catch (e) {
    notifyError(e)
  }
}

function onDragStart(index) {
  dragIndex.value = index
}

function onDragOver(index) {
  overIndex.value = index
}

async function onDrop(index) {
  overIndex.value = -1
  const from = dragIndex.value
  dragIndex.value = -1
  if (from < 0 || from === index) return
  const items = [...list.value]
  const [moved] = items.splice(from, 1)
  items.splice(index, 0, moved)
  list.value = items
  try {
    await api.orderGroups(items.map((g) => g.id))
    notify('分组顺序已更新')
    window.dispatchEvent(new Event('lp:changed'))
  } catch (e) {
    notifyError(e)
    await load()
  }
}

onMounted(load)
</script>

<template>
  <div class="overlay" @click.self="ui.groupManager = false"></div>
  <div class="modal" role="dialog" aria-modal="true" aria-label="分组管理">
    <div class="modal-head">分组管理</div>
    <div class="modal-body">
      <p v-if="!list.length" class="hint">还没有分组。新建一个，把节点归类。</p>
      <div
        v-for="(g, i) in list"
        :key="g.id"
        class="group-row"
        :class="{ 'drag-over': overIndex === i && dragIndex !== i }"
        draggable="true"
        @dragstart="onDragStart(i)"
        @dragover.prevent="onDragOver(i)"
        @dragleave="overIndex = -1"
        @drop.prevent="onDrop(i)"
      >
        <span class="handle" title="拖动排序"><Icon name="grip" :size="15" /></span>
        <input
          v-model="g.draft"
          maxlength="40"
          @keyup.enter="commitRename(g)"
          @keyup.esc="g.draft = g.name"
          @blur="commitRename(g)"
        />
        <button class="icon-btn" title="删除分组" @click="remove(g)">
          <Icon name="trash" :size="14" />
        </button>
      </div>

      <div style="display: flex; gap: 8px; margin-top: 4px">
        <input
          v-model="newName"
          class="input"
          placeholder="新分组名称"
          maxlength="40"
          @keyup.enter="add"
        />
        <button class="btn" :disabled="busy || !newName.trim()" @click="add">
          <Icon name="plus" :size="14" />新建
        </button>
      </div>
      <p class="hint">拖动左侧手柄调整顺序；节点也可以直接拖到另一个分组。</p>
    </div>
    <div class="modal-foot">
      <button class="btn" @click="ui.groupManager = false">关闭</button>
    </div>
  </div>
</template>
