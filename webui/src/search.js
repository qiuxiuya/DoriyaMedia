import { episodeLabel } from './api.js'

export function searchTarget(item) {
  if (item.type === 'season') return `/season/${item.id}`
  // An episode hit belongs to a season, and only the season page lists its
  // neighbours: land there, pointed at that one episode (?ep=), so the result
  // is "第 7 集 of that show" instead of an orphan episode page.
  if (item.type === 'episode' && item.parent_id) {
    return { path: `/season/${item.parent_id}`, query: { ep: String(item.id) } }
  }
  return `/item/${item.id}`
}

export function searchSubtitle(item) {
  const kind = { movie: '电影', show: '剧集', season: '季', episode: '单集' }[item.type]
  const number = item.type === 'episode'
    ? episodeLabel(item)
    : item.type === 'season' ? (item.season ? `第 ${item.season} 季` : '特别篇') : ''
  return [item.search_context, number || kind, item.year].filter(Boolean).join(' · ')
}

// Share ancestor requests within one result set: dozens of matching episodes
// usually belong to only a handful of seasons and shows.
export async function enrichSearchResults(items, getDetail) {
  const parents = new Map()
  function parent(id) {
    if (!parents.has(id)) {
      parents.set(id, Promise.resolve().then(() => getDetail(id)).then((r) => r.item).catch(() => null))
    }
    return parents.get(id)
  }
  return Promise.all(items.map(async (item) => {
    let context = ''
    let current = item
    const seen = new Set([item.id])
    while (current.parent_id && !seen.has(current.parent_id)) {
      seen.add(current.parent_id)
      current = await parent(current.parent_id)
      if (!current) break
      if (current.type === 'show' || !context) context = current.name
    }
    return { ...item, search_context: context }
  }))
}
