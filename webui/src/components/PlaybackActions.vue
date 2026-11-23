<script setup>
import { computed } from 'vue'
import AppIcon from './AppIcon.vue'
import { fmtTime } from '../api.js'

const props = defineProps({
  fileId: { type: Number, default: 0 },
  progress: { type: Object, default: null },
  size: { type: String, default: '' },
  label: { type: String, default: '播放' },
})
const emit = defineEmits(['play'])
const position = computed(() => Math.max(0, Number(props.progress?.position) || 0))
</script>

<template>
  <div class="playback-actions">
    <button class="btn play" :class="size" :disabled="!fileId" @click="emit('play', false)">
      <AppIcon name="play" :size="size === 'lg' ? 17 : 14" filled />
      {{ position > 0 ? '继续播放' : label }}
      <small v-if="position > 0" class="playback-time num">已播放 {{ fmtTime(position) }}</small>
    </button>
    <button v-if="position > 0" class="btn" :class="size" :disabled="!fileId" @click="emit('play', true)">
      <AppIcon name="refresh" :size="size === 'lg' ? 16 : 14" /> 从头播放
    </button>
  </div>
</template>

<style scoped>
.playback-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; min-width: 0; }
.playback-time { font-size: var(--fs-12); font-weight: 400; opacity: 0.75; white-space: nowrap; }
</style>
