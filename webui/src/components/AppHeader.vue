<script setup>
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from './AppIcon.vue'
import SearchOverlay from './SearchOverlay.vue'
import AccountDialog from './AccountDialog.vue'
import SiteText from './SiteText.vue'
import { api } from '../api.js'
import { logout as signOut } from '../auth.js'
import { site, ctx, hoverKey, toolbarOn } from '../site/store.js'
import { fill } from '../site/schema.js'
import { resolveHeaderItems } from '../site/header.js'

const props = defineProps({
  me: { type: Object, default: null },
  overlay: { type: Boolean, default: false }, // page has a full-bleed hero under the bar
})

const route = useRoute()
const router = useRouter()
const libraries = ref([])
const scrolled = ref(false)
const menuOpen = ref(false)
const browseOpen = ref('')
const searching = ref(false)
const acctOpen = ref(false)

const isAdmin = computed(() => props.me?.role === 'admin')
const solid = computed(() => !props.overlay || scrolled.value || menuOpen.value || browseOpen.value)

// The editor changes documents, not permissions: resolve using the actual session.
const menuItems = (section) => resolveHeaderItems(site.doc.header, section, isAdmin.value, libraries.value)
  .map((item) => ({ ...item, label: fill(item.label, ctx) }))
const nav = computed(() => menuItems('desktop'))
const tabs = computed(() => menuItems('mobile'))
const accountItems = computed(() => menuItems('account'))
const highlight = (item) => site.canEdit && toolbarOn.value && hoverKey.value === item.copyKey
const showManageLibraries = computed(() => isAdmin.value && site.doc.header.roles.admin.showManageLibraries !== false)

function onScroll() { scrolled.value = window.scrollY > 24 }

function onDocClick(e) {
  if (!e.target.closest?.('.menu-wrap')) { menuOpen.value = false; browseOpen.value = '' }
}
function onKey(e) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); searching.value = true }
  if (e.key === 'Escape') { menuOpen.value = false; browseOpen.value = '' }
}

onMounted(async () => {
  window.addEventListener('scroll', onScroll, { passive: true })
  document.addEventListener('click', onDocClick)
  window.addEventListener('keydown', onKey)
  onScroll()
  try { libraries.value = (await api.get('/api/libraries')) || [] } catch {}
})
onBeforeUnmount(() => {
  window.removeEventListener('scroll', onScroll)
  document.removeEventListener('click', onDocClick)
  window.removeEventListener('keydown', onKey)
})

watch(() => route.fullPath, () => { menuOpen.value = false; browseOpen.value = '' })

async function onAccountAction(action) {
  menuOpen.value = false
  if (action === 'account') acctOpen.value = true
  else if (action === 'logout') await signOut()
}

function pickLibrary(lib) {
  browseOpen.value = ''
  router.push(`/library/${lib.id}${lib.type === 'series' ? '?type=show' : '?type=movie'}`)
}

defineExpose({ openSearch: () => { searching.value = true } })
</script>

<template>
  <header class="topbar" :class="{ solid, overlay }">
    <div class="inner">
      <router-link to="/" class="brand" aria-label="首页">
        <span class="logo"><AppIcon name="play" :size="13" filled /></span>
        <span class="wordmark">
          <SiteText k="header.brand.name" passive /><SiteText k="header.brand.suffix" class="wm-suffix" passive />
        </span>
      </router-link>

      <nav class="nav desktop-only">
        <template v-for="n in nav" :key="n.key">
          <router-link v-if="!n.action" :to="n.to" class="nav-link" exact-active-class="nav-active">
            <span :data-site-key="n.copyKey" :class="{ 'copy-hot': highlight(n) }">{{ n.label }}</span>
          </router-link>
          <div v-else-if="n.action === 'libraries'" class="menu-wrap">
            <button class="nav-link browse" :class="{ on: browseOpen === n.key }" :aria-expanded="browseOpen === n.key" @click="browseOpen = browseOpen === n.key ? '' : n.key">
              <span :data-site-key="n.copyKey" :class="{ 'copy-hot': highlight(n) }">{{ n.label }}</span>
              <AppIcon name="chevronDown" :size="14" :class="{ flip: browseOpen === n.key }" />
            </button>
            <Transition name="fade">
              <div v-if="browseOpen === n.key" class="dropdown">
                <div class="dd-title"><SiteText k="header.nav.allLibraries" as="span" passive /></div>
                <button v-for="l in libraries" :key="l.id" class="dd-item" @click="pickLibrary(l)">
                  <AppIcon :name="l.type === 'series' ? 'tv' : 'film'" :size="16" />
                  <span class="dd-name">{{ l.name }}</span>
                  <span class="tiny">{{ l.type === 'series' ? '剧集' : '电影' }}</span>
                </button>
                <div v-if="!libraries.length" class="dd-empty tiny">还没有媒体库</div>
                <router-link v-if="showManageLibraries" to="/settings" class="dd-item accent" @click="browseOpen = ''">
                  <AppIcon name="plus" :size="16" /><span class="dd-name"><SiteText k="header.nav.manageLibraries" passive /></span>
                </router-link>
              </div>
            </Transition>
          </div>
        </template>
      </nav>

      <div class="spacer"></div>

      <button class="icon-btn round" title="搜索 (Ctrl/⌘ + K)" aria-label="搜索" @click="searching = true">
        <AppIcon name="search" :size="19" />
      </button>

      <div class="menu-wrap">
        <button class="avatar" :aria-expanded="menuOpen" @click="menuOpen = !menuOpen">
          <span class="av">{{ (me?.name || 'D').slice(0, 1).toUpperCase() }}</span>
          <AppIcon name="chevronDown" :size="14" :class="{ flip: menuOpen }" />
        </button>
        <Transition name="fade">
          <div v-if="menuOpen" class="dropdown right">
            <div class="dd-user">
              <span class="av lg">{{ (me?.name || 'D').slice(0, 1).toUpperCase() }}</span>
              <div>
                <div class="dd-name">{{ me?.name || '未登录' }}</div>
                <div class="tiny">{{ isAdmin ? '管理员' : '用户' }}</div>
              </div>
            </div>
            <template v-for="item in accountItems" :key="item.key">
              <button v-if="item.action" class="dd-item" @click="onAccountAction(item.action)">
                <AppIcon :name="item.icon" :size="16" /><span class="dd-name" :data-site-key="item.copyKey" :class="{ 'copy-hot': highlight(item) }">{{ item.label }}</span>
              </button>
              <router-link v-else :to="item.to" class="dd-item" @click="menuOpen = false">
                <AppIcon :name="item.icon" :size="16" /><span class="dd-name" :data-site-key="item.copyKey" :class="{ 'copy-hot': highlight(item) }">{{ item.label }}</span>
              </router-link>
            </template>
          </div>
        </Transition>
      </div>
    </div>
  </header>

  <SearchOverlay v-model:open="searching" :libraries="libraries" />
  <AccountDialog v-model:open="acctOpen" />

  <nav v-if="tabs.length" class="tabbar">
    <router-link
      v-for="t in tabs" :key="t.key" :to="t.to" class="tab"
      :class="{ exact: t.exact }"
    >
      <AppIcon :name="t.icon" :size="21" />
      <span :data-site-key="t.copyKey" :class="{ 'copy-hot': highlight(t) }">{{ t.label }}</span>
    </router-link>
  </nav>
</template>

<style scoped>
.topbar {
  position: fixed;
  inset: 0 0 auto 0;
  z-index: 900;
  height: var(--header-h);
  display: flex;
  align-items: center;
  transition: background var(--t-mid), border-color var(--t-mid), backdrop-filter var(--t-mid);
  border-bottom: 1px solid transparent;
  background: transparent;
}
.topbar.overlay:not(.solid) { background: linear-gradient(to bottom, rgba(4, 5, 8, 0.82), rgba(4, 5, 8, 0)); }
.topbar.solid {
  background: rgba(9, 10, 14, 0.82);
  backdrop-filter: blur(18px) saturate(1.4);
  -webkit-backdrop-filter: blur(18px) saturate(1.4);
  border-bottom-color: var(--line-1);
}
.inner {
  width: 100%;
  max-width: var(--page-max);
  margin: 0 auto;
  padding: 0 var(--gutter);
  display: flex;
  align-items: center;
  gap: var(--sp-3);
}
.brand { display: flex; align-items: center; gap: 10px; margin-right: var(--sp-4); }
.logo {
  display: grid; place-items: center;
  width: 30px; height: 30px;
  border-radius: 10px;
  background: linear-gradient(150deg, var(--brand-2), #d98a12 70%);
  color: var(--brand-ink);
  box-shadow: 0 4px 16px rgba(240, 169, 46, 0.32), inset 0 1px 0 rgba(255, 255, 255, 0.5);
}
.wordmark {
  font-family: var(--font-display);
  font-size: 17px;
  font-weight: 800;
  letter-spacing: -0.035em;
}
.wordmark .wm-suffix { color: var(--text-3); font-weight: 600; }

.copy-hot { outline: 1px solid var(--brand); outline-offset: 3px; border-radius: 3px; background: var(--brand-tint); }
.nav { display: flex; align-items: center; gap: 2px; }
.nav-link {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 8px 12px;
  border-radius: var(--r-s);
  font-size: var(--fs-14);
  font-weight: 550;
  color: var(--text-2);
  transition: color var(--t-fast), background var(--t-fast);
}
.nav-link:hover { color: #fff; background: rgba(255, 255, 255, 0.06); }
.nav-link.nav-active { color: #fff; }
.nav-link.nav-active::after {
  content: "";
  position: absolute;
  left: 12px; right: 12px; bottom: 2px;
  height: 2px;
  border-radius: 2px;
  background: var(--brand);
}
.flip { transform: rotate(180deg); transition: transform var(--t-fast); }

.icon-btn.round { color: var(--text-2); }
.topbar.overlay:not(.solid) .icon-btn.round:hover { background: rgba(255, 255, 255, 0.12); }

.avatar {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 8px 4px 4px;
  border-radius: var(--r-pill);
  transition: background var(--t-fast);
}
.avatar:hover { background: rgba(255, 255, 255, 0.08); }
.av {
  display: grid; place-items: center;
  width: 30px; height: 30px;
  border-radius: 50%;
  background: linear-gradient(150deg, #3d4450, #22262e);
  border: 1px solid var(--line-2);
  font-size: var(--fs-13);
  font-weight: 700;
  color: var(--text-1);
}
.av.lg { width: 38px; height: 38px; font-size: var(--fs-15); }

.menu-wrap { position: relative; }
.dropdown {
  position: absolute;
  top: calc(100% + 10px);
  left: 0;
  min-width: 244px;
  padding: 7px;
  border-radius: var(--r-m);
  background: rgba(16, 18, 23, 0.96);
  border: 1px solid var(--line-2);
  box-shadow: var(--sh-3);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  z-index: 950;
}
.dropdown.right { left: auto; right: 0; min-width: 232px; }
.dd-title {
  padding: 8px 10px 6px;
  font-size: var(--fs-11);
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--text-4);
}
.dd-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 10px;
  border-radius: var(--r-s);
  color: var(--text-2);
  font-size: var(--fs-13);
  text-align: left;
  transition: background var(--t-fast), color var(--t-fast);
}
.dd-item:hover { background: rgba(255, 255, 255, 0.07); color: #fff; }
.dd-item.accent { color: var(--brand); }
.dd-name { flex: 1; font-weight: 600; }
.dd-user { display: flex; align-items: center; gap: 11px; padding: 10px; border-bottom: 1px solid var(--line-1); margin-bottom: 6px; }
.dd-empty { padding: 10px; }

.tabbar { display: none; }

@media (max-width: 900px) {
  .desktop-only { display: none; }
  .tabbar {
    display: flex;
    position: fixed;
    inset: auto 0 0 0;
    z-index: 900;
    padding: 6px 4px calc(4px + env(safe-area-inset-bottom));
    background: rgba(9, 10, 14, 0.9);
    backdrop-filter: blur(20px) saturate(1.3);
    -webkit-backdrop-filter: blur(20px) saturate(1.3);
    border-top: 1px solid var(--line-1);
  }
  .tab {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 3px;
    padding: 6px 2px;
    border-radius: var(--r-s);
    color: var(--text-4);
    font-size: 10.5px;
    font-weight: 600;
    transition: color var(--t-fast);
  }
  .tab.router-link-active { color: var(--brand); }
  .tab.router-link-active svg { filter: drop-shadow(0 2px 8px rgba(240, 169, 46, 0.4)); }
  .brand { margin-right: 0; }
}
@media (max-width: 520px) {
  .wordmark .wm-suffix { display: none; }
}
</style>
