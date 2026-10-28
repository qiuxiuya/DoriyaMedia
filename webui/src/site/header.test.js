import test from 'node:test'
import assert from 'node:assert/strict'
import { DEFAULTS, clone, mergeDeep, fill } from './schema.js'
import { createHeaderRoles, resolveHeaderItems, safeHeaderTarget } from './header.js'

const libraries = [{ id: 11, type: 'movie' }, { id: 22, type: 'series' }]
const resolve = (header, section = 'desktop', admin = false, libs = libraries) => resolveHeaderItems(header, section, admin, libs)

test('default desktop navigation preserves original order for both roles', () => {
  for (const admin of [false, true]) {
    const entries = resolve(DEFAULTS.header, 'desktop', admin)
    assert.deepEqual(entries.map((e) => e.type), ['home', 'movies', 'shows', 'bangumi', 'browse', 'libraries'])
    assert.equal(entries[1].to, '/library/11?type=movie')
    assert.equal(entries[2].to, '/library/22?type=show')
    assert.equal(entries.at(-1).action, 'libraries')
  }
})

test('default mobile and account menus preserve role restrictions', () => {
  assert.deepEqual(resolve(DEFAULTS.header, 'mobile', false).map((e) => e.type), ['home', 'movies', 'shows', 'browse'])
  assert.deepEqual(resolve(DEFAULTS.header, 'mobile', true).map((e) => e.type), ['home', 'movies', 'shows', 'browse', 'settings'])
  assert.deepEqual(resolve(DEFAULTS.header, 'account', false).map((e) => e.type), ['account', 'bangumi', 'logout'])
  assert.deepEqual(resolve(DEFAULTS.header, 'account', true).map((e) => e.type), ['account', 'bangumi', 'settings', 'logout'])
})

test('older saved content keeps custom wording and receives role defaults', () => {
  const doc = mergeDeep(DEFAULTS, { header: { brand: { name: '影院' }, nav: { home: '发现', movies: '影片' } } })
  assert.equal(doc.header.brand.name, '影院')
  assert.equal(resolve(doc.header)[0].label, '发现')
  assert.equal(resolve(doc.header)[1].label, '影片')
  assert.equal(resolve(doc.header, 'account')[1].label, 'Bangumi 追番')
  assert.equal(doc.header.roles.admin.showManageLibraries, true)
  assert.equal(doc.header.roles.user.showManageLibraries, false)
})

test('each role and section has independent arrays and entry objects', () => {
  const roles = createHeaderRoles()
  roles.admin.desktop[0].label = '管理首页'
  roles.user.desktop.reverse()
  assert.equal(roles.user.desktop.at(-1).label, '')
  assert.equal(roles.admin.mobile[0].label, '')
  assert.equal(roles.admin.desktop[0].type, 'home')
  assert.equal(createHeaderRoles().admin.desktop[0].label, '')
})

test('role-specific copy, visibility, ordering and custom links resolve separately', () => {
  const header = clone(DEFAULTS.header)
  header.roles.user.desktop = [
    { type: 'custom', label: '我的追番', to: '/bangumi', icon: 'star' },
    { type: 'home', label: '用户首页' },
    { type: 'browse', enabled: false },
  ]
  assert.deepEqual(resolve(header).map((e) => e.label), ['我的追番', '用户首页'])
  assert.equal(resolve(header)[0].icon, 'star')
  assert.equal(resolve(header, 'desktop', true)[0].label, '首页')
  assert.equal(resolve(header, 'mobile')[0].label, '首页')
})

test('removed menus stay empty across save/load merge rather than resurrecting defaults', () => {
  const header = clone(DEFAULTS.header)
  header.roles.user.desktop = []
  header.roles.admin.account = []
  const doc = mergeDeep(DEFAULTS, JSON.parse(JSON.stringify({ header })))
  assert.deepEqual(resolve(doc.header), [])
  assert.deepEqual(resolve(doc.header, 'account', true), [])
  assert.notEqual(resolve(doc.header, 'desktop', true).length, 0)
})

test('movie/show shortcuts appear only when a matching library exists', () => {
  assert.deepEqual(resolve(DEFAULTS.header, 'desktop', false, []).map((e) => e.type), ['home', 'bangumi', 'browse', 'libraries'])
  assert.deepEqual(resolve(DEFAULTS.header, 'mobile', true, [libraries[1]]).map((e) => e.type), ['home', 'shows', 'browse', 'settings'])
})

test('built-in routes and actions cannot be overridden with a target field', () => {
  const header = clone(DEFAULTS.header)
  header.roles.admin.account = [{ type: 'logout', to: '/settings', label: '离开' }, { type: 'account' }]
  header.roles.admin.desktop = [{ type: 'home', to: '/settings' }]
  assert.equal(resolve(header, 'desktop', true)[0].to, '/')
  const entries = resolve(header, 'account', true)
  assert.equal(entries[0].action, 'logout')
  assert.equal(entries[0].to, '')
  assert.equal(entries[1].action, 'account')
})

test('regular users cannot expose administrator destinations through saved configuration', () => {
  const header = clone(DEFAULTS.header)
  header.roles.user.desktop = [
    { type: 'settings' },
    { type: 'custom', label: '管理', to: '/settings?tab=users' },
    { type: 'custom', label: '管理', to: '/%73ettings' },
    { type: 'home' },
  ]
  assert.deepEqual(resolve(header).map((e) => e.type), ['home'])
  header.roles.admin.desktop = [{ type: 'settings' }, { type: 'custom', to: '/settings' }]
  assert.equal(resolve(header, 'desktop', true).length, 2)
})

test('unsafe URLs are excluded from custom navigation', () => {
  for (const target of ['https://example.com', '//example.com', '/\\example.com', 'javascript:alert(1)', '/\n/example.com', '/%5cexample.com', '/%2fexample.com', '/%00bad', '/%zz', null]) {
    assert.equal(safeHeaderTarget(target, true), '', String(target))
  }
  assert.equal(safeHeaderTarget('/bangumi?sort=name#latest', false), '/bangumi?sort=name#latest')
  assert.equal(safeHeaderTarget('/settings', true), '/settings')
  assert.equal(safeHeaderTarget('/x/../settings', false), '')
})

test('copy keys track explicit role labels and inherited legacy copy', () => {
  const header = clone(DEFAULTS.header)
  header.roles.user.desktop[0].label = '发现'
  assert.equal(resolve(header)[0].copyKey, 'header.roles.user.desktop.0.label')
  assert.equal(resolve(header)[1].copyKey, 'header.nav.movies')
  assert.equal(resolve(header, 'account')[0].copyKey, 'header.account.account')
  header.nav.movies = '{site} 电影'
  assert.equal(fill(resolve(header)[1].label, { site: '测试影院' }), '测试影院 电影')
})

test('repeated library dropdowns have unique rendering keys', () => {
  const header = clone(DEFAULTS.header)
  header.roles.admin.desktop = [{ type: 'libraries' }, { type: 'libraries' }]
  const entries = resolve(header, 'desktop', true)
  assert.equal(entries.length, 2)
  assert.notEqual(entries[0].key, entries[1].key)
})

test('unsupported actions in a section and malformed entries are ignored', () => {
  const header = clone(DEFAULTS.header)
  header.roles.user.mobile = [null, false, {}, { type: { toString: null } }, { type: [] }, { type: '__proto__' }, { type: 'toString' }, { type: 'libraries' }, { type: 'logout' }, { type: 'unknown' }, { type: 'custom', to: '' }, { type: 'home' }]
  assert.deepEqual(resolve(header, 'mobile').map((e) => e.type), ['home'])
})
