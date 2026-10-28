<script setup>
/** 顶栏 tab — shared copy, with independent menus for each visitor role. */
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { listOf, rawOf, setField, addItem, removeItem, moveItem, hoverKey } from '../../site/store.js'
import { DEFAULTS, FEATURE_ICONS, clone } from '../../site/schema.js'
import { HEADER_ITEM_TYPES, HEADER_SECTIONS, safeHeaderTarget } from '../../site/header.js'
import AppIcon from '../AppIcon.vue'
import PanelGroup from './PanelGroup.vue'
import PanelField from './PanelField.vue'
import PanelSegmented from './PanelSegmented.vue'

const route = useRoute()
const noHeaderHere = computed(() => route.meta.chrome === false)
// This is an editing target only: never touch the current session or permissions.
const role = ref('admin')
const roles = [{ id: 'admin', label: '管理员' }, { id: 'user', label: '普通用户' }]
const iconNames = [...new Set([...Object.values(HEADER_ITEM_TYPES).map((type) => type.icon), ...FEATURE_ICONS])]
const roleLabel = computed(() => roles.find((item) => item.id === role.value).label)

function menuPath(section) {
  return `header.roles.${role.value}.${section}`
}
function entryPath(section, index) {
  return `${menuPath(section)}.${index}`
}
function entries(section) {
  return listOf(menuPath(section))
}
function availableTypes(section) {
  return Object.entries(HEADER_ITEM_TYPES).filter(([type, spec]) => (
    spec.sections.includes(section)
    && (!spec.adminOnly || role.value === 'admin')
    // Library management belongs in the libraries dropdown, not the main menu.
    && !(type === 'manageLibraries' && section === 'desktop')
  ))
}
function changeType(section, index, type) {
  if (!availableTypes(section).some(([key]) => key === type)) return
  const item = entries(section)[index]
  setField(entryPath(section, index), {
    type,
    label: '',
    to: type === 'custom' ? '/' : '',
    icon: HEADER_ITEM_TYPES[type].icon,
    enabled: item.enabled !== false,
  })
}
function addEntry(section) {
  addItem(menuPath(section), { type: 'custom', label: '新链接', to: '/', icon: 'link', enabled: true })
}
function copyHint(section, type) {
  if (type === 'custom') return '自定义链接显示的文字'
  const group = section === 'account' && ['account', 'bangumi', 'settings', 'logout'].includes(type) ? 'account' : 'nav'
  return `留空继承共享默认文案 header.${group}.${type}；内置入口的路由固定`
}
function setTarget(section, index, event) {
  const input = event.target
  const target = safeHeaderTarget(input.value, role.value === 'admin')
  if (!target) {
    input.setCustomValidity(role.value === 'admin'
      ? '请输入以 / 开头的有效站内链接，不支持外部网址。'
      : '请输入有效站内链接；普通用户菜单不能指向 /settings 管理页面。')
    input.reportValidity()
    input.value = entries(section)[index].to || '/'
    return
  }
  input.setCustomValidity('')
  setField(`${entryPath(section, index)}.to`, target)
  input.value = target
}
function resetRole() {
  if (!confirm(`恢复${roleLabel.value}的默认菜单？\n\n只重置该角色的菜单及媒体库管理开关，品牌与共享文案保持不变。`)) return
  setField(`header.roles.${role.value}`, clone(DEFAULTS.header.roles[role.value]))
}
function hot(key) {
  hoverKey.value = key
}
function cold() {
  hoverKey.value = ''
}
</script>

<template>
  <div v-if="noHeaderHere" class="st-notice">
    这个页面不显示顶栏。改动会马上生效，<router-link to="/">回到首页</router-link> 就能预览顶栏。
  </div>

  <PanelGroup title="编辑角色" icon="user" open>
    <PanelSegmented v-model="role" :options="roles" />
    <p class="st-note">只编辑选中角色的菜单，不会切换当前登录身份。隐藏菜单不改变路由或 API 权限。</p>
    <label v-if="role === 'admin'" class="st-switch">
      <input
        type="checkbox" :checked="rawOf(`header.roles.${role}.showManageLibraries`) !== false"
        @change="setField(`header.roles.${role}.showManageLibraries`, $event.target.checked)"
      />
      在媒体库下拉中显示「管理媒体库」
    </label>
    <button class="st-add sm" type="button" @click="resetRole">
      <AppIcon name="refresh" :size="13" /> 恢复{{ roleLabel }}默认菜单
    </button>
  </PanelGroup>

  <PanelGroup title="共享品牌" icon="globe" open>
    <p class="st-note">品牌和默认文案由两个角色共享；单项文字留空时继承这里的文案。</p>
    <div class="st-pair">
      <PanelField k="header.brand.name" label="品牌 · 名称" />
      <PanelField k="header.brand.suffix" label="品牌 · 后缀(灰色部分)" />
    </div>
  </PanelGroup>

  <PanelGroup
    v-for="section in HEADER_SECTIONS" :key="`${role}-${section.id}`"
    :title="`${roleLabel} · ${section.label}`" :icon="section.id === 'account' ? 'user' : 'list'" open
  >
    <p v-if="section.id === 'desktop'" class="st-note">媒体库下拉仅支持桌面导航；管理入口由管理员的独立开关控制。</p>
    <p v-else-if="section.id === 'mobile'" class="st-note">手机底部标签独立排序；电影 / 剧集入口仅在对应媒体库存在时显示。</p>
    <p v-if="!entries(section.id).length" class="st-note">还没有菜单项，可以添加一个新链接。</p>
    <div
      v-for="(item, index) in entries(section.id)" :key="index" class="st-item"
      @mouseenter="hot(`${entryPath(section.id, index)}.label`)" @mouseleave="cold"
    >
      <div class="st-item-head">
        <span class="st-item-no">{{ index + 1 }}</span>
        <AppIcon :name="item.icon || HEADER_ITEM_TYPES[item.type]?.icon || 'link'" :size="14" />
        <span class="spacer" />
        <button class="st-mini" type="button" title="上移" :disabled="index === 0" @click="moveItem(menuPath(section.id), index, -1)"><AppIcon name="chevronUp" :size="14" /></button>
        <button class="st-mini" type="button" title="下移" :disabled="index === entries(section.id).length - 1" @click="moveItem(menuPath(section.id), index, 1)"><AppIcon name="chevronDown" :size="14" /></button>
        <button class="st-mini danger" type="button" title="删除菜单项" @click="removeItem(menuPath(section.id), index)"><AppIcon name="trash" :size="14" /></button>
      </div>
      <label class="st-row">
        <span class="st-label">类型</span>
        <select class="st-input" :value="item.type" @change="changeType(section.id, index, $event.target.value)">
          <option v-if="!availableTypes(section.id).some(([type]) => type === item.type)" :value="item.type" disabled>不适用于当前角色或分组</option>
          <option v-for="[type, spec] in availableTypes(section.id)" :key="type" :value="type">{{ spec.label }}</option>
        </select>
      </label>
      <PanelField :k="`${entryPath(section.id, index)}.label`" label="文字" :hint="copyHint(section.id, item.type)" />
      <label v-if="item.type === 'custom'" class="st-row">
        <span class="st-label">站内链接</span>
        <input
          class="st-input st-mono" :value="item.to" placeholder="/browse" :list="`st-header-routes-${role}`"
          @input="$event.target.setCustomValidity('')" @change="setTarget(section.id, index, $event)"
        />
        <span class="st-hint">以 / 开头；不支持外部网址{{ role === 'user' ? '，普通用户不能添加管理页面入口' : '' }}。</span>
      </label>
      <label v-if="section.id !== 'desktop'" class="st-row">
        <span class="st-label">图标</span>
        <select class="st-input" :value="item.icon || HEADER_ITEM_TYPES[item.type]?.icon || 'link'" @change="setField(`${entryPath(section.id, index)}.icon`, $event.target.value)">
          <option v-if="item.icon && !iconNames.includes(item.icon)" :value="item.icon">{{ item.icon }}</option>
          <option v-for="icon in iconNames" :key="icon" :value="icon">{{ icon }}</option>
        </select>
      </label>
      <label class="st-switch">
        <input type="checkbox" :checked="item.enabled !== false" @change="setField(`${entryPath(section.id, index)}.enabled`, $event.target.checked)" />
        显示此菜单项
      </label>
    </div>
    <button class="st-add" type="button" @click="addEntry(section.id)"><AppIcon name="plus" :size="14" /> 添加菜单项</button>
  </PanelGroup>

  <datalist :id="`st-header-routes-${role}`">
    <option value="/" />
    <option value="/browse" />
    <option value="/bangumi" />
    <option v-if="role === 'admin'" value="/settings" />
  </datalist>

  <PanelGroup title="共享默认文案 · 导航" icon="type">
    <PanelField k="header.nav.home" label="导航 · 首页" />
    <PanelField k="header.nav.movies" label="导航 · 电影" hint="有电影库时才出现" />
    <PanelField k="header.nav.shows" label="导航 · 剧集" hint="有剧集库时才出现" />
    <PanelField k="header.nav.bangumi" label="导航 · 追番" />
    <PanelField k="header.nav.browse" label="导航 · 文件" />
    <PanelField k="header.nav.libraries" label="导航 · 媒体库" hint="展开全部媒体库的那个按钮" />
    <PanelField k="header.nav.allLibraries" label="媒体库下拉 · 标题" />
    <PanelField k="header.nav.manageLibraries" label="媒体库下拉 · 管理入口" />
    <PanelField k="header.nav.settings" label="导航 · 设置" hint="仅管理员可见" />
  </PanelGroup>

  <PanelGroup title="共享默认文案 · 账号菜单" icon="user">
    <PanelField k="header.account.account" label="账号 · 账号设置" />
    <PanelField k="header.account.bangumi" label="账号 · 追番" />
    <PanelField k="header.account.settings" label="账号 · 设置" />
    <PanelField k="header.account.logout" label="账号 · 退出登录" />
  </PanelGroup>
</template>
