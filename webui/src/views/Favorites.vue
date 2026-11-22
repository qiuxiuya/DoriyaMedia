<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api.js'
import AppIcon from '../components/AppIcon.vue'
import PosterCard from '../components/PosterCard.vue'
import SkeletonGrid from '../components/SkeletonGrid.vue'
import EmptyState from '../components/EmptyState.vue'

/**
 * 我的收藏 — the profile menu's favorites page. It shares the Emby adapter's
 * favorites rows, so a title starred in an Emby client appears here and vice
 * versa. The tab lives in the URL so a refresh keeps the chosen list.
 */
const route = useRoute()
const router = useRouter()

const tab = ref(route.query.tab === 'show' ? 'show' : 'movie')
const items = ref([])
const loading = ref(true)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const r = await api.get(`/api/favorites?kind=${tab.value}`)
    items.value = r.items || []
  } catch (e) {
    error.value = e.message
    items.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)

watch(tab, (v) => {
  if ((route.query.tab || 'movie') !== v) router.replace({ query: { tab: v } })
  load()
})
watch(() => route.query.tab, (v) => { tab.value = v === 'show' ? 'show' : 'movie' })

const empty = computed(() => !loading.value && !items.value.length)
const isShow = computed(() => tab.value === 'show')
</script>

<template>
  <div class="page favorites-page">
    <header class="hd">
      <div class="crumb">
        <router-link to="/" class="tiny">首页</router-link>
        <AppIcon name="chevronRight" :size="12" />
        <span class="tiny">我的收藏</span>
      </div>
      <h1 class="display-1"><AppIcon name="heart" :size="26" filled /> 我的收藏</h1>
      <p class="muted">这里是你收藏的影片与剧集，与 Emby 客户端里的收藏保持同步。</p>
    </header>

    <div class="segmented tabs">
      <button :class="{ on: tab === 'movie' }" @click="tab = 'movie'">
        <AppIcon name="film" :size="15" /> 电影
      </button>
      <button :class="{ on: tab === 'show' }" @click="tab = 'show'">
        <AppIcon name="tv" :size="15" /> 剧集
      </button>
    </div>

    <div v-if="error" class="flash err">
      <AppIcon name="alert" :size="16" /> {{ error }}
    </div>

    <div v-if="loading" class="skeletons"><SkeletonGrid :count="12" /></div>

    <EmptyState
      v-else-if="empty"
      icon="heart"
      :title="isShow ? '还没有收藏剧集' : '还没有收藏电影'"
      :text="isShow ? '在剧集详情页点一下收藏，就会出现在这里。' : '在电影详情页点一下收藏，就会出现在这里。'"
    >
      <router-link to="/" class="btn primary"><AppIcon name="play" :size="16" filled /> 去逛逛</router-link>
    </EmptyState>

    <div v-else v-reveal.stagger class="grid-auto">
      <PosterCard
        v-for="(it, i) in items" :key="it.id" :item="it" :eager="i < 8"
        :style="{ '--i': Math.min(i, 11) }"
      />
    </div>
  </div>
</template>

<style scoped>
.favorites-page { padding-top: calc(var(--header-h) + clamp(20px, 4vw, 44px)); padding-bottom: var(--sp-8); }
.hd { margin-bottom: var(--sp-5); }
.hd .crumb { display: flex; align-items: center; gap: 6px; margin-bottom: 10px; color: var(--text-4); }
.hd .crumb a:hover { color: var(--text-1); }
.hd h1 { display: flex; align-items: center; gap: 12px; margin: 0 0 8px; }
.hd h1 svg { color: var(--brand); }
.hd p { margin: 0; }
.tabs { margin-bottom: var(--sp-5); }
.tabs button { display: inline-flex; align-items: center; gap: 6px; }
</style>
