<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, artUrl, fmtRemaining, fmtRelative, episodeLabel } from '../api.js'
import AppIcon from '../components/AppIcon.vue'
import BiliBadge from '../components/BiliBadge.vue'
import SmartImage from '../components/SmartImage.vue'
import EmptyState from '../components/EmptyState.vue'

/**
 * The profile menu's 观看记录 page. Two tabs share one query: 观看记录 lists every
 * record (in-progress included), 已观看 only what was finished. The tab is held
 * in the URL so a refresh — or a link someone copied — lands back on the same
 * list instead of silently resetting to the first one.
 */
const route = useRoute()
const router = useRouter()

const tab = ref(route.query.tab === 'watched' ? 'watched' : 'history')
const records = ref([])
const loading = ref(true)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const kind = tab.value === 'watched' ? 'watched' : 'all'
    const r = await api.get(`/api/history?kind=${kind}`)
    records.value = r.records || []
  } catch (e) {
    error.value = e.message
    records.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)

// Switching a tab edits the URL, and the browser's back button walks the tabs.
watch(tab, (v) => {
  const want = v === 'watched' ? 'watched' : ''
  if ((route.query.tab || '') !== want) router.replace({ query: want ? { tab: want } : {} })
  load()
})
watch(() => route.query.tab, (v) => { tab.value = v === 'watched' ? 'watched' : 'history' })

const empty = computed(() => !loading.value && !records.value.length)
</script>

<template>
  <div class="page history-page">
    <header class="hd">
      <div class="crumb">
        <router-link to="/" class="tiny">首页</router-link>
        <AppIcon name="chevronRight" :size="12" />
        <span class="tiny">观看记录</span>
      </div>
      <h1 class="display-1"><AppIcon name="history" :size="26" /> 观看记录</h1>
      <p class="muted">这里保存着你看过和正在看的影片，随时接着上次的进度继续。</p>
    </header>

    <div class="segmented tabs">
      <button :class="{ on: tab === 'history' }" @click="tab = 'history'">观看记录</button>
      <button :class="{ on: tab === 'watched' }" @click="tab = 'watched'">已观看</button>
    </div>

    <div v-if="error" class="flash err">
      <AppIcon name="alert" :size="16" /> {{ error }}
    </div>

    <!-- loading -->
    <div v-if="loading" class="hist-grid">
      <div v-for="n in 6" :key="n" class="sk wide-sk" />
    </div>

    <!-- empty -->
    <EmptyState
      v-else-if="empty"
      icon="history"
      :title="tab === 'watched' ? '还没有看完的影片' : '还没有观看记录'"
      :text="tab === 'watched' ? '看完一部影片后会出现在这里。' : '播放任意影片后这里会记录进度。'"
    >
      <router-link to="/" class="btn primary"><AppIcon name="play" :size="16" filled /> 去逛逛</router-link>
    </EmptyState>

    <!-- records -->
    <div v-else class="hist-grid">
      <router-link
        v-for="(c, i) in records" :key="c.item.id + '-' + c.file_id"
        :to="`/play/${c.file_id}`" class="wide-card"
        :style="{ '--i': Math.min(i, 12) }"
      >
        <div class="still">
          <SmartImage :src="artUrl(c.item)" :alt="c.item.name" :seed="c.item.name" />
          <BiliBadge v-if="c.item.source_type === 'bilibili'" mini />
          <span v-if="c.watched" class="watched-tag"><AppIcon name="check" :size="12" /> 已看完</span>
        </div>
        <span class="quick"><AppIcon name="play" :size="19" filled /></span>
        <div class="body">
          <div class="t truncate">{{ c.item.name }}</div>
          <div class="s">
            <span v-if="episodeLabel(c.item)" class="ep">{{ episodeLabel(c.item) }}</span>
            <template v-if="c.watched">已看完 · {{ fmtRelative(c.updated_at) }}</template>
            <template v-else>{{ fmtRemaining(c.position, c.duration) }} · {{ fmtRelative(c.updated_at) }}</template>
          </div>
        </div>
        <div class="progress">
          <i :style="{ width: Math.min(100, c.percent) + '%' }"></i>
        </div>
      </router-link>
    </div>
  </div>
</template>

<style scoped>
.history-page { padding-top: calc(var(--header-h) + clamp(20px, 4vw, 44px)); padding-bottom: var(--sp-8); }
.hd { margin-bottom: var(--sp-5); }
.hd .crumb { display: flex; align-items: center; gap: 6px; margin-bottom: 10px; color: var(--text-4); }
.hd .crumb a:hover { color: var(--text-1); }
.hd h1 { display: flex; align-items: center; gap: 12px; margin: 0 0 8px; }
.hd h1 svg { color: var(--brand); }
.hd p { margin: 0; }

.tabs { margin-bottom: var(--sp-5); }

.hist-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 300px), 1fr));
  gap: var(--sp-4);
}
.wide-sk { aspect-ratio: 16 / 9; border-radius: var(--r-m); }

.watched-tag {
  position: absolute;
  top: 8px;
  right: 8px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  border-radius: var(--r-pill);
  background: rgba(6, 7, 10, 0.72);
  border: 1px solid var(--line-2);
  color: #fff;
  font-size: var(--fs-11);
  font-weight: 650;
  backdrop-filter: blur(8px);
  z-index: 3;
}
.hist-grid .wide-card { animation: hist-in 320ms var(--ease-out) both; animation-delay: calc(var(--i, 0) * 24ms); }
@keyframes hist-in { from { opacity: 0; transform: translateY(10px); } to { opacity: 1; transform: none; } }
</style>
