import { ref, computed } from 'vue'
import { api } from './api.js'

// One shared answer to "who am I", because both the router guard and the header
// need it and two independent fetches could disagree mid-navigation.
export const me = ref(null)
export const ready = ref(false)

export const isAdmin = computed(() => me.value?.role === 'admin')

/**
 * Whether this session may see and open the raw 文件 browser. The server answers
 * it on /api/auth/me (can_list_file_browser) because the owner controls it with
 * a three-way setting: 都可见 / 仅管理员 / 关闭. Defaults to false until the
 * session is known, so an entry point never flashes for someone who cannot use
 * it.
 */
export const canBrowseFiles = computed(() => !!me.value?.can_list_file_browser)
/** The raw setting value ("all" / "admin" / "off"), for diagnosing the choice. */
export const fileBrowserMode = computed(() => me.value?.file_browser || 'admin')

/** Load the signed-in account once; force re-reads it from the server. */
export async function loadMe(force = false) {
  if (ready.value && !force) return me.value
  try {
    const res = await api.get('/api/auth/me')
    // The server reports the file-browser visibility beside the user; fold it
    // into the one object every caller reads, so canBrowseFiles has an answer.
    me.value = res?.user
      ? { ...res.user, file_browser: res.file_browser, can_list_file_browser: res.can_list_file_browser }
      : null
  } catch {
    me.value = null
  }
  ready.value = true
  return me.value
}

/** Forget the cached answer, so the next guard run hits the server. */
export function resetAuth() {
  ready.value = false
  me.value = null
}

export async function logout() {
  try { await api.post('/api/auth/logout') } catch {}
  resetAuth()
  location.href = '/login'
}
