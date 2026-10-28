/** Driver paths are source-relative absolute paths, never host mount paths. */
export function normalizeScanPath(value) {
  const path = typeof value === 'string' ? value.trim() : ''
  if (!path) throw new Error('请输入要扫描的目录，空路径不会提交扫描')
  if (!path.startsWith('/') || path.includes('\\') || /[\u0000-\u001f\u007f]/.test(path)) {
    throw new Error('请输入以 / 开头的源内完整目录路径，不要填写本机挂载路径或 URL')
  }
  const parts = path.split('/').filter(Boolean)
  if (parts.some((part) => part === '.' || part === '..')) {
    throw new Error('目录路径不能包含 . 或 .. 路径段')
  }
  return '/' + parts.join('/')
}

export function libraryScanSources(library) {
  return library?.sources?.length
    ? library.sources
    : library ? [{ source_id: library.source_id, path: library.path || '/' }] : []
}

export function isScanPathWithin(path, root) {
  return root === '/' || path === root || path.startsWith(root + '/')
}

/** Validate again at submission, so an invalid target can never become a full scan. */
export function buildPathScanRequest(library, place, value) {
  const sourceId = Number(place?.source_id)
  if (!Number.isSafeInteger(sourceId) || sourceId <= 0) throw new Error('请选择媒体库内的存储源')
  const root = normalizeScanPath(place.path || '/')
  const configured = libraryScanSources(library).some((entry) =>
    Number(entry.source_id) === sourceId && normalizeScanPath(entry.path || '/') === root)
  if (!configured) throw new Error('所选目录不属于该媒体库，请重新选择存储源')
  const path = normalizeScanPath(value)
  if (!isScanPathWithin(path, root)) throw new Error(`扫描目录必须是 ${root} 或其子目录`)
  return { path, source_id: sourceId }
}
