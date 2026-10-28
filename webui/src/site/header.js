/** Built-in destinations stay code-owned; each role owns its ordering and copy. */
export const HEADER_ITEM_TYPES = {
  home: { label: '首页', icon: 'home', sections: ['desktop', 'mobile', 'account'] },
  movies: { label: '电影', icon: 'film', sections: ['desktop', 'mobile', 'account'] },
  shows: { label: '剧集', icon: 'tv', sections: ['desktop', 'mobile', 'account'] },
  bangumi: { label: '追番', icon: 'bookmark', sections: ['desktop', 'mobile', 'account'] },
  browse: { label: '文件', icon: 'folder', sections: ['desktop', 'mobile', 'account'] },
  libraries: { label: '媒体库下拉', icon: 'layers', sections: ['desktop'] },
  settings: { label: '设置', icon: 'settings', sections: ['desktop', 'mobile', 'account'], adminOnly: true },
  account: { label: '账号设置', icon: 'user', sections: ['account'] },
  logout: { label: '退出登录', icon: 'logout', sections: ['account'] },
  custom: { label: '自定义链接', icon: 'link', sections: ['desktop', 'mobile', 'account'] },
}

export const HEADER_SECTIONS = [
  { id: 'desktop', label: '桌面导航' },
  { id: 'mobile', label: '手机底部标签' },
  { id: 'account', label: '头像下拉菜单' },
]

function items(types) {
  return types.map((type) => ({ type, label: '', to: '', icon: HEADER_ITEM_TYPES[type].icon, enabled: true }))
}

export function createHeaderRoles() {
  const role = (admin) => ({
    desktop: items(['home', 'movies', 'shows', 'bangumi', 'browse', 'libraries']),
    mobile: items(['home', 'movies', 'shows', 'browse', ...(admin ? ['settings'] : [])]),
    account: items(['account', 'bangumi', ...(admin ? ['settings'] : []), 'logout']),
    showManageLibraries: admin,
  })
  return { admin: role(true), user: role(false) }
}

/** Only local application routes; exclude protocol-relative and executable URLs. */
export function safeHeaderTarget(target, isAdmin) {
  if (typeof target !== 'string') return ''
  const to = target.trim()
  if (!to.startsWith('/') || /[\\\s\u0000-\u001f\u007f]/.test(to) || to.startsWith('//')) return ''
  let path
  try { path = decodeURIComponent(new URL(to, 'https://site.invalid').pathname) } catch { return '' }
  // Decoding also prevents encoded slashes or letters from disguising admin routes.
  if (/[\\\u0000-\u001f\u007f]/.test(path) || path.startsWith('//')) return ''
  if (!isAdmin && /^\/settings(?:\/|$)/i.test(path)) return ''
  return to
}

/** Resolve against the actual logged-in role, never the role selected in the editor. */
export function resolveHeaderItems(header, section, isAdmin, libraries = []) {
  const entries = header?.roles?.[isAdmin ? 'admin' : 'user']?.[section]
  if (!Array.isArray(entries)) return []
  const movie = libraries.find((lib) => lib.type === 'movie')
  const series = libraries.find((lib) => lib.type === 'series')
  return entries.flatMap((entry, index) => {
    if (!entry || typeof entry !== 'object' || entry.enabled === false) return []
    if (typeof entry.type !== 'string' || !Object.hasOwn(HEADER_ITEM_TYPES, entry.type)) return []
    const spec = HEADER_ITEM_TYPES[entry.type]
    if (!spec.sections.includes(section) || (spec.adminOnly && !isAdmin)) return []
    let to = ''
    let action = ''
    switch (entry.type) {
      case 'home': to = '/'; break
      case 'movies': if (!movie) return []; to = `/library/${movie.id}?type=movie`; break
      case 'shows': if (!series) return []; to = `/library/${series.id}?type=show`; break
      case 'bangumi': to = '/bangumi'; break
      case 'browse': to = '/browse'; break
      case 'settings': to = '/settings'; break
      case 'libraries': action = 'libraries'; break
      case 'account': action = 'account'; break
      case 'logout': action = 'logout'; break
      case 'custom': to = safeHeaderTarget(entry.to, isAdmin); if (!to) return []; break
      default: return []
    }
    const copyGroup = section === 'account' && ['account', 'bangumi', 'settings', 'logout'].includes(entry.type) ? 'account' : 'nav'
    const hasLabel = typeof entry.label === 'string' && entry.label.trim()
    const label = hasLabel ? entry.label : header?.[copyGroup]?.[entry.type] ?? spec.label
    const copyKey = hasLabel || entry.type === 'custom'
      ? `header.roles.${isAdmin ? 'admin' : 'user'}.${section}.${index}.label`
      : `header.${copyGroup}.${entry.type}`
    return [{ key: `${section}-${index}`, copyKey, type: entry.type, label, to, action, icon: entry.icon || spec.icon, exact: to === '/' }]
  })
}
