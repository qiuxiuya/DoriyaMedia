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
// `played` is the explicit watched flag, not a position/duration ratio: an item
// marked watched on a client carries it even when this browser never reached the
// end. A finished item has no place to continue from, so its position is ignored.
const watched = computed(() => !!props.progress?.played)
const position = computed(() => {
  if (watched.value) return 0
  return Math.max(0, Number(props.progress?.position) || 0)
})
</script>

<template>
  <div class="playback-actions">
    <!-- Finished: state on the left, the white action on the right. There is
         nothing to continue, so the only sensible action is a fresh start. -->
    <template v-if="watched">
      <span class="watched-label" :class="size">
        <AppIcon name="check" :size="size === 'lg' ? 16 : 14" /> 已观看
      </span>
      <button class="btn play" :class="size" :disabled="!fileId" @click="emit('play', true)">
        <AppIcon name="refresh" :size="size === 'lg' ? 17 : 14" /> 从头播放
      </button>
    </template>
    <template v-else>
      <button class="btn play" :class="size" :disabled="!fileId" @click="emit('play', false)">
        <AppIcon name="play" :size="size === 'lg' ? 17 : 14" filled />
        {{ position > 0 ? '继续播放' : label }}
        <small v-if="position > 0" class="playback-time num">已播放 {{ fmtTime(position) }}</small>
      </button>
      <button v-if="position > 0" class="btn" :class="size" :disabled="!fileId" @click="emit('play', true)">
        <AppIcon name="refresh" :size="size === 'lg' ? 16 : 14" /> 从头播放
      </button>
    </template>
  </div>
</template>

<style scoped>
.playback-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; min-width: 0; }
.playback-time { font-size: var(--fs-12); font-weight: 400; opacity: 0.75; white-space: nowrap; }
.watched-label {
  display: inline-flex; align-items: center; gap: 6px; white-space: nowrap;
  color: var(--text-3); font-size: var(--fs-13); font-weight: 500;
}
.watched-label.lg { font-size: var(--fs-15); }
</style>
