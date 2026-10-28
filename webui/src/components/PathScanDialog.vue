<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '../api.js'
import { buildPathScanRequest, isScanPathWithin, libraryScanSources, normalizeScanPath } from '../libraryScan.js'
import AppIcon from './AppIcon.vue'

const props = defineProps({
  library: { type: Object, required: true },
  sources: { type: Array, default: () => [] },
  busy: Boolean,
  running: Boolean,
  error: { type: String, default: '' },
})
const emit = defineEmits(['close', 'submit'])
const dialog = ref(null)
const places = computed(() => libraryScanSources(props.library))
const selected = ref(places.value.length === 1 ? '0' : '')
const place = computed(() => selected.value === '' ? null : places.value[Number(selected.value)])
const path = ref('')
const browsePath = ref('')
const directories = ref([])
const browsing = ref(false)
const browseError = ref('')
let browseVersion = 0
let previousFocus
const validation = computed(() => {
  try { return { body: buildPathScanRequest(props.library, place.value, path.value), error: '' } }
  catch (e) { return { body: null, error: e.message } }
})
const blocked = computed(() => props.busy || props.running)
const root = computed(() => {
  try { return place.value ? normalizeScanPath(place.value.path || '/') : '' }
  catch { return '' }
})
const parentPath = computed(() => {
  if (!browsePath.value || browsePath.value === root.value) return ''
  const parent = browsePath.value.slice(0, browsePath.value.lastIndexOf('/')) || '/'
  return isScanPathWithin(parent, root.value) ? parent : ''
})
function sourceLabel(entry) {
  const source = props.sources.find((s) => Number(s.id) === Number(entry.source_id))
  return `${source?.name || `存储源 #${entry.source_id}`} · ${entry.path || '/'}`
}
watch(selected, () => {
  browseVersion++
  path.value = ''
  browsePath.value = ''
  directories.value = []
  browseError.value = ''
  browsing.value = false
})
async function browse(value) {
  if (blocked.value || !place.value) return
  let body
  try { body = buildPathScanRequest(props.library, place.value, value) }
  catch (e) { browseError.value = e.message; return }
  const version = ++browseVersion
  browsing.value = true
  browseError.value = ''
  try {
    const data = await api.get(`/api/browse/${body.source_id}?path=${encodeURIComponent(body.path)}`)
    if (version !== browseVersion) return
    directories.value = (data?.entries || []).filter((entry) => {
      if (!entry.is_dir) return false
      try {
        const candidate = normalizeScanPath(entry.path)
        return candidate !== body.path && isScanPathWithin(candidate, root.value)
      } catch { return false }
    }).sort((a, b) => a.name.localeCompare(b.name, 'zh-Hans-CN', { numeric: true }))
    browsePath.value = body.path
    path.value = body.path
  } catch (e) {
    if (version === browseVersion) { browseError.value = e.message; directories.value = []; browsePath.value = '' }
  } finally {
    if (version === browseVersion) browsing.value = false
  }
}
function submit() {
  if (blocked.value || !validation.value.body) return
  emit('submit', { place: place.value, ...validation.value.body })
}
function close() { if (!props.busy) emit('close') }
function keydown(event) {
  if (event.key === 'Escape') { event.preventDefault(); close() }
  if (event.key !== 'Tab') return
  const controls = [...dialog.value.querySelectorAll('button:not(:disabled), input:not(:disabled), select:not(:disabled)')]
  const first = controls[0], last = controls.at(-1)
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
onMounted(async () => {
  previousFocus = document.activeElement
  await nextTick()
  dialog.value?.querySelector(places.value.length > 1 ? 'select' : 'input')?.focus()
})
onBeforeUnmount(() => { browseVersion++; previousFocus?.focus() })
</script>

<template>
  <div class="modal-mask" @click.self="close" @keydown="keydown">
    <section ref="dialog" class="modal path-scan" role="dialog" aria-modal="true" aria-labelledby="path-scan-title" :aria-busy="busy">
      <div class="modal-head">
        <div>
          <div id="path-scan-title" class="card-title"><AppIcon name="folder" :size="16" /> 按路径扫描「{{ library.name }}」</div>
          <p class="tiny">只扫描选定源内的这个目录及其子目录，不扫描媒体库其他路径。</p>
        </div>
      </div>
      <form @submit.prevent="submit">
        <div class="field">
          <label for="path-scan-source">媒体库内的存储源 / 配置目录</label>
          <select id="path-scan-source" v-model="selected" :disabled="blocked || browsing">
            <option disabled value="">请选择一个存储源及配置目录</option>
            <option v-for="(entry, index) in places" :key="index" :value="String(index)">{{ sourceLabel(entry) }}</option>
          </select>
        </div>
        <div class="field">
          <label for="path-scan-path">源内完整目录路径</label>
          <input id="path-scan-path" v-model="path" type="text" placeholder="例如 /电影/新片" autocomplete="off" :disabled="!place || blocked || browsing" :aria-invalid="!!path && !!validation.error" aria-describedby="path-scan-hint path-scan-validation" />
          <span id="path-scan-hint" class="hint">填写存储源内的路径（driver path），不是服务器本机挂载路径。必须是所选配置目录或其子目录；留空不会启动全量扫描。</span>
          <span v-if="path && validation.error" id="path-scan-validation" class="path-scan-error" role="alert">{{ validation.error }}</span>
        </div>
        <div class="btn-row">
          <button type="button" class="btn sm" :disabled="!root || blocked || browsing" @click="browse(root)"><AppIcon name="folder" :size="14" /> 浏览配置目录</button>
          <button type="button" class="btn sm" :disabled="!validation.body || blocked || browsing" @click="browse(path)">浏览输入目录</button>
        </div>
        <p v-if="browsing" class="tiny" role="status">正在读取目录…</p>
        <p v-if="browseError" class="path-scan-error" role="alert">读取目录失败：{{ browseError }}。也可直接输入目录后提交。</p>
        <div v-if="browsePath" class="path-scan-browser">
          <div class="path-scan-location"><span class="tiny">当前选择：{{ browsePath }}</span><button v-if="parentPath" type="button" class="btn sm" :disabled="blocked || browsing" @click="browse(parentPath)">上一级</button></div>
          <ul v-if="directories.length" class="path-scan-dirs">
            <li v-for="entry in directories" :key="entry.path"><button type="button" :disabled="blocked || browsing" @click="browse(entry.path)"><AppIcon name="folder" :size="15" /> {{ entry.name }}<AppIcon name="chevronRight" :size="14" /></button></li>
          </ul>
          <p v-else class="tiny">此目录下没有可选择的子目录。</p>
        </div>
        <p v-if="validation.body" class="hint path-scan-preview">将扫描：{{ sourceLabel(place) }} → {{ validation.body.path }}（含子目录）</p>
        <p v-if="running" class="path-scan-error" role="alert">该媒体库正在扫描，请等待完成或先停止扫描。</p>
        <p v-if="error" class="path-scan-error" role="alert">{{ error }}</p>
        <div class="btn-row path-scan-actions">
          <button type="button" class="btn" :disabled="busy" @click="close">取消</button>
          <button class="btn primary" :disabled="!validation.body || blocked || browsing"><AppIcon name="scan" :size="15" /> {{ busy ? '正在提交…' : '只扫描此目录' }}</button>
        </div>
      </form>
    </section>
  </div>
</template>

<style scoped>
.btn-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.path-scan { width: min(620px, calc(100vw - 32px)); max-height: calc(100dvh - 48px); overflow-y: auto; }
.path-scan-error { color: var(--danger, #ef6464); font-size: var(--fs-12); overflow-wrap: anywhere; }
.path-scan-browser { margin-top: var(--sp-3); padding: var(--sp-3); border: 1px solid var(--line-2); border-radius: var(--r-m); }
.path-scan-location { display: flex; align-items: center; justify-content: space-between; gap: 8px; overflow-wrap: anywhere; }
.path-scan-dirs { list-style: none; margin: var(--sp-2) 0 0; padding: 0; max-height: 220px; overflow-y: auto; }
.path-scan-dirs button { display: flex; align-items: center; gap: 8px; width: 100%; padding: 9px 4px; text-align: left; background: none; border: none; color: inherit; cursor: pointer; overflow-wrap: anywhere; }
.path-scan-dirs button:hover { color: var(--accent); }
.path-scan-dirs button svg:last-child { margin-left: auto; flex-shrink: 0; }
.path-scan-preview { display: block; margin-top: var(--sp-3); font-size: var(--fs-12); color: var(--text-4); line-height: 1.55; overflow-wrap: anywhere; }
.path-scan-actions { margin-top: var(--sp-4); justify-content: flex-end; }
</style>
