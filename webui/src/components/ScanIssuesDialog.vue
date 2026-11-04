<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../api.js'
import AppIcon from './AppIcon.vue'

const props = defineProps({
  library: { type: Object, required: true },
})
const emit = defineEmits(['close'])

const dialog = ref(null)
const loading = ref(true)
const error = ref('')
const issues = ref([])
const counts = ref({ mismatch: 0, scrape: 0 })
const keyword = ref('')
const kind = ref('')
let previousFocus

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  return issues.value.filter((is) => {
    if (kind.value && is.kind !== kind.value) return false
    if (!k) return true
    return (is.path || '').toLowerCase().includes(k) || (is.name || '').toLowerCase().includes(k)
  })
})

const kinds = computed(() => [
  { value: '', label: `全部 ${counts.value.mismatch + counts.value.scrape}` },
  { value: 'mismatch', label: `类型不匹配 ${counts.value.mismatch}` },
  { value: 'scrape', label: `刮削失败 ${counts.value.scrape}` },
])

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await api.get(`/api/libraries/${props.library.id}/issues`)
    issues.value = data?.issues || []
    counts.value = { mismatch: data?.mismatch || 0, scrape: data?.scrape || 0 }
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function copyAll() {
  const text = filtered.value
    .map((is) => (is.path ? `${is.path}${is.detail ? `\t${is.detail}` : ''}` : `${is.name}\t${is.detail || ''}`))
    .join('\n')
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    error.value = '复制失败,浏览器拒绝了剪贴板访问'
  }
}

function close() { emit('close') }
function keydown(event) {
  if (event.key === 'Escape') { event.preventDefault(); close() }
  if (event.key !== 'Tab') return
  const controls = [...(dialog.value?.querySelectorAll('button:not(:disabled), input:not(:disabled)') || [])]
  const first = controls[0], last = controls.at(-1)
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}

onMounted(async () => {
  previousFocus = document.activeElement
  await load()
  await nextTick()
  dialog.value?.querySelector('input')?.focus()
})
onBeforeUnmount(() => { previousFocus?.focus() })
</script>

<template>
  <div class="modal-mask" @click.self="close" @keydown="keydown">
    <section ref="dialog" class="modal issues" role="dialog" aria-modal="true" aria-labelledby="issues-title">
      <div class="modal-head">
        <div>
          <div id="issues-title" class="card-title">
            <AppIcon name="alert" :size="16" />
            「{{ library.name }}」的扫描问题
          </div>
          <p class="tiny">
            这里列出上次扫描没能入库的文件路径,以及没有刮到信息的条目。
            <template v-if="counts.mismatch || counts.scrape">
              类型不匹配 {{ counts.mismatch }} 个,刮削失败 {{ counts.scrape }} 个。
            </template>
          </p>
        </div>
        <button class="icon-btn close" aria-label="关闭" @click="close"><AppIcon name="close" :size="16" /></button>
      </div>

      <div class="tools">
        <div class="search-box">
          <AppIcon name="search" :size="14" />
          <input v-model="keyword" class="input" placeholder="按路径或名称筛选…" />
          <button v-if="keyword" class="clear" aria-label="清空" @click="keyword = ''"><AppIcon name="close" :size="12" /></button>
        </div>
        <div class="segmented">
          <button v-for="k in kinds" :key="k.value" :class="{ on: kind === k.value }" @click="kind = k.value">{{ k.label }}</button>
        </div>
      </div>

      <div v-if="error" class="flash err"><AppIcon name="alert" :size="14" /> {{ error }}</div>

      <div v-if="loading" class="tiny pad">正在读取…</div>

      <template v-else-if="filtered.length">
        <div class="count-line tiny">
          显示 {{ filtered.length }} / {{ issues.length }} 条
          <button class="link" @click="copyAll">复制路径</button>
        </div>
        <ul class="issue-list">
          <li v-for="is in filtered" :key="is.id" :class="['issue', is.kind]">
            <span class="tag">{{ is.kind === 'mismatch' ? '类型不匹配' : '刮削失败' }}</span>
            <div class="body">
              <div class="path">{{ is.path || is.name || '(未知路径)' }}</div>
              <div v-if="is.name && is.path" class="name tiny">{{ is.name }}</div>
              <div v-if="is.detail" class="detail tiny">{{ is.detail }}</div>
            </div>
          </li>
        </ul>
      </template>

      <div v-else class="empty">
        <AppIcon name="check" :size="22" />
        <p>{{ issues.length ? '没有符合筛选条件的问题' : '没有发现问题,扫描结果都正常。' }}</p>
      </div>

      <div class="btn-row actions">
        <button class="btn" @click="load"><AppIcon name="refresh" :size="14" /> 刷新</button>
        <button class="btn primary" @click="close">关闭</button>
      </div>
    </section>
  </div>
</template>

<style scoped>
.issues { width: min(760px, calc(100vw - 32px)); max-height: calc(100dvh - 48px); display: flex; flex-direction: column; }
.modal-head { align-items: flex-start; }
.modal-head .close { margin-left: auto; }
.icon-btn { display: grid; place-items: center; width: 30px; height: 30px; border-radius: 50%; color: var(--text-4); }
.icon-btn:hover { background: rgba(255, 255, 255, 0.08); color: var(--text-1); }
.card-title { display: flex; align-items: center; gap: 8px; }

.tools { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; margin-bottom: var(--sp-3); }
.search-box { flex: 1; min-width: 200px; }
.search-box .input { height: 34px; min-height: 34px; font-size: var(--fs-13); }
.search-box .clear { position: absolute; right: 8px; display: grid; place-items: center; width: 20px; height: 20px; border-radius: 50%; color: var(--text-4); }
.search-box .clear:hover { background: rgba(255, 255, 255, 0.1); color: var(--text-1); }

.count-line { display: flex; align-items: center; gap: 10px; margin-bottom: var(--sp-2); color: var(--text-4); }
.link { color: var(--brand); font-size: var(--fs-12); font-weight: 600; }
.link:hover { text-decoration: underline; }

.issue-list { list-style: none; margin: 0; padding: 0; overflow-y: auto; min-height: 0; max-height: 52dvh; display: flex; flex-direction: column; gap: 6px; }
.issue {
  display: flex; align-items: flex-start; gap: 10px;
  padding: 9px 11px; border-radius: var(--r-s);
  border: 1px solid var(--line-1); background: var(--bg-0);
}
.issue .tag { flex: none; font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: var(--r-pill); white-space: nowrap; }
.issue.mismatch .tag { color: #ffb3b3; background: var(--danger-soft); border: 1px solid rgba(255, 107, 107, 0.3); }
.issue.scrape .tag { color: #fcd34d; background: var(--warn-soft); border: 1px solid rgba(251, 191, 36, 0.28); }
.issue .body { min-width: 0; flex: 1; }
.issue .path { font-size: var(--fs-13); overflow-wrap: anywhere; }
.issue .name { color: var(--text-3); }
.issue .detail { color: var(--text-4); }

.empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: var(--sp-5) 0; color: var(--text-4); text-align: center; }
.empty svg { color: var(--ok); }

.actions { display: flex; justify-content: flex-end; gap: var(--sp-2); margin-top: var(--sp-4); }
.flash { margin-bottom: var(--sp-3); }
.pad { padding: var(--sp-4) 0; color: var(--text-4); }
</style>
