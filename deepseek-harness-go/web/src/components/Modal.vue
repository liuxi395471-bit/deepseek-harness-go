<script setup lang="ts">
// Modal.vue — 通用模态框（轻量、自托管；不引入 portal 库）。
//
// 用法：
//   <Modal :open="show" title="..." @close="show = false" @confirm="onConfirm">
//     <input v-model="..." />
//     <template #footer>
//       <button class="btn btn-primary" @click="onConfirm">确定</button>
//     </template>
//   </Modal>

import { X } from 'lucide-vue-next'

defineProps<{
  open: boolean
  title: string
}>()

const emit = defineEmits<{
  close: []
  confirm: []
}>()
</script>

<template>
  <div v-if="open" class="modal-mask" @click.self="emit('close')">
    <div class="modal-card">
      <div class="flex items-center justify-between mb-2">
        <div class="modal-title">{{ title }}</div>
        <button class="btn" @click="emit('close')" aria-label="Close">
          <X :size="14" />
        </button>
      </div>
      <div class="modal-body">
        <slot />
      </div>
      <div class="modal-footer">
        <slot name="footer">
          <button class="btn" @click="emit('close')">取消</button>
          <button class="btn btn-primary" @click="emit('confirm')">确定</button>
        </slot>
      </div>
    </div>
  </div>
</template>
