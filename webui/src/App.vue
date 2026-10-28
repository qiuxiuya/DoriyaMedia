<script setup>
import { ref, computed, onMounted, onErrorCaptured, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from './api.js'
import { me, loadMe } from './auth.js'
import AppHeader from './components/AppHeader.vue'
import AppFooter from './components/AppFooter.vue'
import SiteToolbar from './components/SiteToolbar.vue'
import ToastHost from './components/ToastHost.vue'
import { ctx } from './site/store.js'

const route = useRoute()
const version = ref('')

const fullscreen = computed(() => !!route.meta.fullscreen)
const bare = computed(() => route.meta.chrome === false)
const overlay = computed(() => !!route.meta.overlay)

/**
 * A page that throws while rendering leaves the shell intact and the content
 * area empty: header and footer on screen, nothing in between, and the only
 * trace is a line in the console nobody is looking at. Catching it here turns
 * that silent blank into the actual message, with a way out.
 */
const pageError = ref(null)
onErrorCaptured((err, _instance, info) => {
  pageError.value = err
  console.error(`[page] ${info}`, err)
  return false
})
watch(() => route.fullPath, () => { pageError.value = null })
function reload() { location.reload() }

onMounted(async () => {
  // The router guard has usually loaded this already; forcing a read keeps the
  // first paint after a reload honest.
  await loadMe(true)
  if (!me.value) return
  try {
    // The footer names the binary it is talking to. /api/system/info is
    // authenticated, so it is asked for only once there is a session; if it
    // fails the footer simply omits the version.
    version.value = (await api.get('/api/system/info')).version || ''
    ctx.version = version.value
    ctx.user = me.value.name || ''
  } catch {}
})
</script>

<template>
  <!-- player: its own chrome, letterbox black, no site header -->
  <div v-if="fullscreen" class="shell fullscreen">
    <router-view />
  </div>

  <!-- login: full-bleed page with its own layout -->
  <div v-else-if="bare" class="shell bare">
    <router-view />
    <ToastHost />
  </div>

  <div v-else class="shell">
    <AppHeader :me="me" :overlay="overlay" />
    <main class="content" :class="{ overlay }">
      <div v-if="pageError" class="page-error">
        <h1>这个页面没能显示出来</h1>
        <p class="muted">页面在渲染时抛出了错误，具体信息如下（浏览器控制台里也有同样的记录）：</p>
        <pre>{{ pageError && (pageError.stack || pageError.message || String(pageError)) }}</pre>
        <div class="row-flex wrap">
          <button class="btn primary" @click="reload">重新加载</button>
          <router-link to="/" class="btn">回到首页</router-link>
        </div>
      </div>
      <router-view v-else v-slot="{ Component, route: r }">
        <Transition name="page" mode="out-in">
          <!--
            The transition animates this wrapper, not the page itself. A page is
            free to render a fragment root — Settings has its QR dialog beside
            the page — and a fragment root cannot be animated. Under
            mode="out-in" that is not merely a lost animation: the leave never
            reports back, the transition stays in its leaving state, and every
            later route renders as an empty comment — header and footer on
            screen, nothing in between, until a full reload.
          -->
          <div :key="r.path" class="view">
            <component :is="Component" />
          </div>
        </Transition>
      </router-view>
    </main>
    <AppFooter :me="me" :version="version" />
    <ToastHost />
  </div>

  <!-- the owner's editing toolbar: admin only, absent on the player -->
  <SiteToolbar />
</template>

<style scoped>
.shell { min-height: 100dvh; display: flex; flex-direction: column; }
.shell.fullscreen { background: #000; }
.shell.bare { display: block; }
.content { flex: 1; }
.content.overlay { margin-top: calc(var(--header-h) * -1); }
.content:not(.overlay) { padding-top: var(--header-h); }

/* The error panel is a page in its own right: it needs the header's height back
   even when the route it replaced was an overlay page. */
.page-error {
  max-width: 780px;
  margin: 0 auto;
  padding: calc(var(--header-h) + var(--sp-6)) var(--gutter) var(--sp-6);
}
.page-error h1 { font-size: var(--fs-22); margin: 0 0 var(--sp-2); }
.page-error pre {
  margin: var(--sp-4) 0;
  padding: var(--sp-4);
  max-height: 40vh;
  overflow: auto;
  border: 1px solid var(--line-2);
  border-radius: var(--r-m);
  background: var(--bg-1);
  color: var(--text-2);
  font-family: var(--font-mono);
  font-size: var(--fs-12);
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>

<style>
/* route transition — quiet, no layout jank */
.page-enter-active { transition: opacity 260ms var(--ease-out), transform 260ms var(--ease-out); }
.page-leave-active { transition: opacity 140ms ease-in; }
.page-enter-from { opacity: 0; transform: translateY(10px); }
.page-leave-to { opacity: 0; }
</style>
