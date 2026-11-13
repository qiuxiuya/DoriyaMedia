<script setup>
import { ref, watch, computed, nextTick, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from './AppIcon.vue'
import MediaRail from './MediaRail.vue'
import PosterCard from './PosterCard.vue'
import { api } from '../api.js'
import { isAdmin } from '../auth.js'
import { enrichSearchResults, searchSubtitle, searchTarget } from '../search.js'

/**
 * Command-palette style search across every media library.
 * The REST API filters per library, so we fan out and merge — a few libraries
 * at most, so this stays a single round trip in practice.
 */
const props = defineProps({
  open: { type: Boolean, default: false },
  libraries: { type: Array, default: () => [] },
})
const emit = defineEmits(['update:open'])

const router = useRouter()
const panel = ref(null)
const q = ref('')
const input = ref(null)
const loading = ref(false)
const results = ref([])
const recents = ref([])
const active = ref(0)
// Catalogue hits are titles that exist on the source but not in any library yet.
const catalog = ref([])
const catalogLoading = ref(false)
const catalogWarning = ref('')
let catalogSeq = 0
let searchSeq = 0

function cancelSearch() {
  clearTimeout(timer)
  clearTimeout(catalogTimer)
  searchSeq++
  catalogSeq++
}
onBeforeUnmount(cancelSearch)

watch(() => props.open, async (v) => {
  if (v) {
    await nextTick()
    input.value?.focus()
    if (!recents.value.length) loadRecents()
  } else {
    cancelSearch()
    q.value = ''
    results.value = []
    catalog.value = []
    loading.value = false
    catalogLoading.value = false
    catalogWarning.value = ''
  }
})

async function loadRecents() {
  try {
    const home = await api.get('/api/home')
    const rows = [...(home.recent_movies || []), ...(home.recent_shows || [])].slice(0, 6)
    recents.value = rows
  } catch {}
}

let timer = null
let catalogTimer = null
watch(q, () => {
  active.value = 0
  cancelSearch()
  catalogWarning.value = ''
  catalogLoading.value = false
  const term = q.value.trim()
  if (!term) {
    results.value = []
    catalog.value = []
    loading.value = false
    catalogLoading.value = false
    return
  }
  loading.value = true
  timer = setTimeout(run, 220)
  // The catalogue search is a second round trip (bilibili); let it lag the
  // local results by a keystroke's worth so typing never waits on it.
  clearTimeout(catalogTimer)
  catalogTimer = setTimeout(runCatalog, 340)
})

async function run() {
  const seq = searchSeq
  const term = q.value.trim()
  if (!props.open || !term) return
  const libs = props.libraries.length ? props.libraries : []
  try {
    const lists = await Promise.all(libs.map((l) =>
      api.get(`/api/items?library_id=${l.id}&search=${encodeURIComponent(term)}&limit=40&sort=name`)
        .then((r) => r.items || []).catch(() => []),
    ))
    const seen = new Set()
    const merged = []
    for (const it of lists.flat()) {
      if (seen.has(it.id)) continue
      seen.add(it.id)
      merged.push(it)
    }
    const low = term.toLowerCase()
    merged.sort((a, b) => {
      const as = a.name.toLowerCase().startsWith(low) ? 0 : 1
      const bs = b.name.toLowerCase().startsWith(low) ? 0 : 1
      return as - bs || (b.rating || 0) - (a.rating || 0) || a.name.localeCompare(b.name)
    })
    if (seq !== searchSeq) return
    const enriched = await enrichSearchResults(merged.slice(0, 60), (id) => api.get(`/api/items/${id}`))
    if (seq === searchSeq) results.value = enriched
  } finally {
    if (seq === searchSeq) loading.value = false
  }
}

async function runCatalog() {
  // Catalogue search reaches bilibili and leads to importing new items; that is
  // admin curation, so a viewer never triggers it (or sees the results section).
  if (!isAdmin.value) { catalog.value = []; catalogWarning.value = ''; return }
  const term = q.value.trim()
  // One character matches half of bilibili; wait for a second one.
  if ([...term].length < 2) { catalog.value = []; catalogWarning.value = ''; return }
  const seq = ++catalogSeq
  catalogLoading.value = true
  catalogWarning.value = ''
  try {
    const r = await api.get(`/api/catalog/search?q=${encodeURIComponent(term)}&limit=12`)
    if (seq !== catalogSeq) return // a newer keystroke already owns the box
    catalog.value = r.items || []
    catalogWarning.value = (r.warnings || []).join('; ')
  } catch (e) {
    if (seq === catalogSeq) {
      catalog.value = []
      catalogWarning.value = e.message
    }
  } finally {
    if (seq === catalogSeq) catalogLoading.value = false
  }
}

// bilibili serves covers over http, and its image CDN rejects a foreign
// referer. Upgrade the scheme, ask for the 320px rendition (the originals are
// multi-megabyte PNGs), and the <img> is rendered with referrerpolicy=no-referrer.
function coverOf(url) {
  if (!url) return ''
  let u = String(url).replace(/^http:/, 'https:')
  if (!u.includes('@')) u += '@320w_480h_1c.webp'
  return u
}

function openCatalog(hit) {
  emit('update:open', false)
  router.push({
    path: `/import/${hit.source_id}/${hit.season_id}`,
    query: {
      library: hit.library_id,
      title: hit.title,
      cover: coverOf(hit.cover),
      year: hit.year || '',
    },
  })
}

const groups = computed(() => {
  if (!q.value.trim()) return [{ key: 'recent', label: '', items: recents.value }]
  return [
    { key: 'titles', label: '电影 / 剧集', items: results.value.filter((it) => !['season', 'episode'].includes(it.type)) },
    { key: 'seasons', label: '季', items: results.value.filter((it) => it.type === 'season') },
    { key: 'episodes', label: '单集', items: results.value.filter((it) => it.type === 'episode') },
  ].filter((group) => group.items.length)
})
const shown = computed(() => groups.value.flatMap((group) => group.items))
const resultBadge = (item) => ({ season: '季', episode: '单集' }[item.type] || '')

// A new query starts a new result set: send every rail and the panel back to
// the top-left, or the user keeps looking at the old scroll offset. The rails
// scroll smoothly, so the jump to 0 has to opt out of that.
watch(groups, () => nextTick(() => {
  const root = panel.value
  if (!root) return
  root.querySelector('.body')?.scrollTo({ top: 0 })
  for (const track of root.querySelectorAll('.rail-track')) {
    track.style.scrollBehavior = 'auto'
    track.scrollLeft = 0
    track.style.scrollBehavior = ''
  }
}))

function go(item) {
  emit('update:open', false)
  router.push(searchTarget(item))
}

/** Cards in visual order — every group's two-row rail, catalogue excluded. */
function cards() {
  const root = panel.value
  if (!root) return []
  return [...root.querySelectorAll('.result-group .rail-track > .poster')]
}

/* Each rail scrolls sideways inside a panel that scrolls down, so an arrow key
   may have to move two containers. Scrolling them by hand (instead of
   scrollIntoView) keeps the page behind the mask from moving too. */
function reveal(el) {
  const pad = 8
  for (let node = el.parentElement; node && node !== document.documentElement; node = node.parentElement) {
    const r = el.getBoundingClientRect()
    const c = node.getBoundingClientRect()
    if (node.scrollWidth > node.clientWidth + 1) {
      if (r.left < c.left + pad) node.scrollLeft -= c.left + pad - r.left
      else if (r.right > c.right - pad) node.scrollLeft += r.right - (c.right - pad)
    }
    if (node.scrollHeight > node.clientHeight + 1) {
      if (r.top < c.top + pad) node.scrollTop -= c.top + pad - r.top
      else if (r.bottom > c.bottom - pad) node.scrollTop += r.bottom - (c.bottom - pad)
    }
  }
}

function focusCard(index) {
  active.value = index
  nextTick(() => {
    const el = cards()[index]
    if (el) reveal(el)
  })
}

/* Directional rather than index arithmetic: groups hold different counts and
   wrap into columns, so pick the nearest card in the pressed direction. */
function move(dx, dy) {
  const list = cards()
  if (!list.length) return
  const cur = list[active.value]
  if (!cur) return focusCard(0)
  const cr = cur.getBoundingClientRect()
  const cx = cr.left + cr.width / 2
  const cy = cr.top + cr.height / 2
  let best = -1
  let bestScore = Infinity
  list.forEach((el, i) => {
    if (i === active.value) return
    const r = el.getBoundingClientRect()
    const sx = r.left + r.width / 2 - cx
    const sy = r.top + r.height / 2 - cy
    const along = dx * sx + dy * sy
    if (along <= 4) return // not in that direction
    // weight the off-axis drift so arrows stay on their row / column
    const score = along + (dx ? Math.abs(sy) : Math.abs(sx)) * 4
    if (score < bestScore) { bestScore = score; best = i }
  })
  if (best >= 0) focusCard(best)
}

function onKeydown(e) {
  if (e.key === 'Escape') { emit('update:open', false); return }
  if (!shown.value.length) return
  if (e.key === 'ArrowRight') { e.preventDefault(); move(1, 0) }
  if (e.key === 'ArrowLeft') { e.preventDefault(); move(-1, 0) }
  if (e.key === 'ArrowDown') { e.preventDefault(); move(0, 1) }
  if (e.key === 'ArrowUp') { e.preventDefault(); move(0, -1) }
  if (e.key === 'Enter') { e.preventDefault(); go(shown.value[active.value] || shown.value[0]) }
}
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="search-mask" @click.self="emit('update:open', false)">
        <div ref="panel" class="search-panel" @keydown="onKeydown">
          <div class="bar">
            <AppIcon name="search" :size="20" />
            <input
              ref="input" v-model="q" class="q"
              placeholder="搜索电影、剧集、番剧…" autocomplete="off" spellcheck="false"
            />
            <span v-if="loading" class="tiny">搜索中…</span>
            <button class="icon-btn" aria-label="关闭" @click="emit('update:open', false)">
              <AppIcon name="close" :size="18" />
            </button>
          </div>

          <div class="body no-scrollbar">
            <div v-if="!q.trim() && recents.length" class="hint-row">
              <AppIcon name="history" :size="14" /> 最近添加
            </div>
            <div v-else-if="q.trim() && !loading && !catalogLoading && !results.length && !catalog.length && !catalogWarning" class="none">
              <AppIcon name="search" :size="26" />
              <div class="t">没有找到「{{ q }}」</div>
              <div class="tiny">媒体库与 B 站片库均无结果</div>
            </div>

            <section v-for="group in groups" :key="group.key" class="result-group">
              <div v-if="group.label" class="hint-row">{{ group.label }} · {{ group.items.length }}</div>
              <MediaRail class="results-rail">
                <PosterCard
                  v-for="(it, i) in group.items" :key="it.id" :item="it"
                  :to="searchTarget(it)"
                  :subtitle="searchSubtitle(it)" :badge="resultBadge(it)"
                  :class="{ active: shown[active]?.id === it.id }"
                  :style="{ '--i': Math.min(i, 11) }"
                  @mouseenter="active = shown.findIndex((item) => item.id === it.id)"
                  @click="emit('update:open', false)"
                />
              </MediaRail>
            </section>

            <!-- Catalogue results: on bilibili, not in any library. Clicking one
                 imports its whole series, with a live loading page. -->
            <section v-if="isAdmin && q.trim() && (catalogLoading || catalog.length || catalogWarning)" class="catalog">
              <div class="catalog-head">
                <span class="catalog-label"><AppIcon name="cloud" :size="15" /> B 站片库</span>
                <span class="tiny">未入库 · 点开自动入库</span>
                <span v-if="catalogLoading" class="tiny cat-loading">搜索中…</span>
              </div>
              <div v-if="catalogWarning" class="cat-warn tiny">
                <AppIcon name="alert" :size="12" /> {{ catalogWarning }}
              </div>
              <MediaRail class="catalog-rail">
                <button
                  v-for="(c, i) in catalog" :key="c.source_id + ':' + c.season_id"
                  class="catalog-card" :style="{ '--i': Math.min(i, 11) }"
                  @click="openCatalog(c)"
                >
                  <span class="cat-art">
                    <span class="cat-ph">{{ c.title.slice(0, 2) }}</span>
                    <img
                      v-if="coverOf(c.cover)" :src="coverOf(c.cover)" :alt="c.title"
                      referrerpolicy="no-referrer"
                      @error="$event.target.style.display = 'none'"
                    />
                    <span class="cat-add"><AppIcon name="plus" :size="14" /></span>
                  </span>
                  <span class="cat-title truncate">{{ c.title }}</span>
                  <span class="cat-meta tiny">
                    {{ [c.year, c.index_show || (c.episodes ? c.episodes + ' 集' : '')].filter(Boolean).join(' · ') || c.library_name }}
                  </span>
                </button>
              </MediaRail>
            </section>

            <div v-if="!q.trim() && !recents.length" class="none">
              <AppIcon name="sparkles" :size="26" />
              <div class="t">还没有内容</div>
              <div class="tiny">请先在设置里添加存储源与媒体库</div>
            </div>
          </div>

          <div class="foot tiny">
            <span><kbd>←</kbd><kbd>→</kbd><kbd>↑</kbd><kbd>↓</kbd> 移动</span>
            <span><kbd>↵</kbd> 打开</span>
            <span><kbd>Esc</kbd> 关闭</span>
            <span class="spacer"></span>
            <span v-if="q.trim()">{{ results.length }} 个库内结果<span v-if="isAdmin && catalog.length"> · {{ catalog.length }} 个未入库</span></span>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.search-mask {
  position: fixed;
  inset: 0;
  z-index: 1500;
  background: rgba(3, 4, 7, 0.74);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: clamp(12px, 8vh, 96px) 16px 16px;
}
.search-panel {
  /* one knob for the rail card width, and the panel colour the rails fade to */
  --hit-w: 148px;
  --panel-bg: rgba(13, 15, 20, 0.97);
  width: min(1180px, 100%);
  max-height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--panel-bg);
  border: 1px solid var(--line-2);
  border-radius: var(--r-l);
  box-shadow: var(--sh-4);
  overflow: hidden;
  animation: panelIn 340ms var(--ease-out) backwards;
}
.bar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--line-1);
  color: var(--text-3);
}
.q {
  flex: 1;
  min-width: 0;
  border: none;
  background: none;
  outline: none;
  font-size: var(--fs-16);
  color: var(--text-1);
}
.q::placeholder { color: var(--text-4); }
.body { padding: var(--sp-5); overflow: auto; overflow-wrap: anywhere; }
.result-group + .result-group { margin-top: var(--sp-5); }
.hint-row { display: flex; align-items: center; gap: 7px; font-size: var(--fs-12); color: var(--text-4); margin-bottom: var(--sp-4); }

/* Every group is a two-row rail: cards fill downwards first, then the row
   scrolls sideways. The panel itself keeps scrolling vertically, so a long
   result list stays a short panel. */
.results-rail :deep(.rail-track),
.catalog-rail :deep(.rail-track) {
  display: grid;
  grid-auto-flow: column;
  grid-template-rows: repeat(2, auto);
  align-items: start;
  margin: 0;
  padding: 8px 2px 10px;
  gap: var(--sp-4);
  scroll-padding-inline: 2px;
}
.results-rail :deep(.rail-track) { grid-auto-columns: var(--hit-w, 148px); }
.catalog-rail :deep(.rail-track) {
  grid-auto-columns: calc(var(--hit-w, 148px) * 0.86);
  gap: var(--sp-4);
}
.results-rail :deep(.rail-track > .poster) { width: auto; }
/* fade the rail's edges instead of hugging the panel border with the scrim */
.results-rail :deep(.rail-arrow),
.catalog-rail :deep(.rail-arrow) { top: 0; bottom: 0; width: 64px; }
.results-rail :deep(.rail-arrow.left),
.catalog-rail :deep(.rail-arrow.left) {
  left: 0;
  padding-left: 2px;
  background: linear-gradient(90deg, var(--panel-bg) 32%, transparent);
}
.results-rail :deep(.rail-arrow.right),
.catalog-rail :deep(.rail-arrow.right) {
  right: 0;
  padding-right: 2px;
  background: linear-gradient(270deg, var(--panel-bg) 32%, transparent);
}
.results-rail :deep(.poster.active) { transform: translateY(-4px); }
.results-rail :deep(.poster.active .art) {
  border-color: var(--brand);
  box-shadow: 0 0 0 2px var(--brand), var(--sh-3);
}

/* catalogue row — deliberately lighter than the library grid: these cards are
   not in the library yet, and the plus chip says clicking will add them. */
.catalog { margin-top: var(--sp-6); padding-top: var(--sp-5); border-top: 1px solid var(--line-1); }
.catalog-head { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-bottom: var(--sp-4); }
.catalog-label {
  display: inline-flex; align-items: center; gap: 7px;
  font-size: var(--fs-13); font-weight: 700; color: var(--text-1);
}
.catalog-label svg { color: var(--brand); }
.cat-loading { margin-left: auto; }
.cat-warn {
  display: flex; align-items: center; gap: 6px;
  margin: -4px 0 var(--sp-4);
  color: #e5a04b;
}
.catalog-card {
  display: flex; flex-direction: column; gap: 5px;
  text-align: left; min-width: 0;
  animation: catalogIn 360ms var(--ease-out) backwards;
  animation-delay: calc(var(--i, 0) * 28ms);
}
.cat-art {
  position: relative; display: block;
  aspect-ratio: 2/3; border-radius: var(--r-s);
  overflow: hidden; background: var(--bg-3);
  border: 1px solid var(--line-1);
  transition: border-color var(--t-fast), transform var(--t-fast), box-shadow var(--t-fast);
}
.catalog-card:hover .cat-art { border-color: var(--brand); transform: translateY(-3px); box-shadow: var(--sh-3); }
.cat-art img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; display: block; }
.cat-ph {
  position: absolute; inset: 0;
  display: grid; place-items: center;
  font-size: var(--fs-20); font-weight: 800; color: var(--text-4);
  background: linear-gradient(150deg, var(--bg-2), var(--bg-1));
}
.cat-add {
  position: absolute; right: 7px; bottom: 7px;
  display: grid; place-items: center;
  width: 26px; height: 26px; border-radius: 50%;
  background: var(--brand); color: var(--brand-ink);
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.45);
  opacity: 0; transform: translateY(5px) scale(0.9);
  transition: all var(--t-fast);
}
.catalog-card:hover .cat-add { opacity: 1; transform: none; }
.cat-title { font-size: var(--fs-13); font-weight: 650; color: var(--text-1); }
.cat-meta { color: var(--text-4); }
@keyframes catalogIn { from { opacity: 0; transform: translateY(8px); } }
.none {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 60px 0;
  color: var(--text-4);
  text-align: center;
}
.none .t { font-size: var(--fs-15); font-weight: 650; color: var(--text-2); }
.foot {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 16px;
  border-top: 1px solid var(--line-1);
  color: var(--text-4);
}
kbd {
  display: inline-grid;
  place-items: center;
  min-width: 18px;
  height: 18px;
  margin-right: 3px;
  padding: 0 4px;
  border-radius: 4px;
  border: 1px solid var(--line-2);
  background: var(--bg-2);
  font-family: var(--font-mono);
  font-size: 10px;
}
@media (max-width: 900px) {
  .search-panel { --hit-w: 118px; }
}
@media (max-width: 720px) {
  .foot { display: none; }
  .body { padding: var(--sp-4); }
  .search-panel { --hit-w: 104px; }
  .results-rail :deep(.rail-track),
  .catalog-rail :deep(.rail-track) { gap: var(--sp-4) var(--sp-3); }
}
</style>
