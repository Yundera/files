// Browsing state. Svelte 5 runes in a .svelte.ts module, so any component can
// import the same live values without a store subscription.
import { api } from './api'
import type { Entry, Page, SortKey, ViewMode } from './types'

export const TRASH_PATH = '__trash__'

export const roots = [
  { name: 'DATA', path: '/' },
  { name: 'Documents', path: '/Documents' },
  { name: 'Downloads', path: '/Downloads' },
  { name: 'Media', path: '/Media' },
  { name: 'AppData', path: '/AppData' },
]

class BrowseState {
  path = $state('/')
  entries = $state<Entry[]>([])
  total = $state(0)
  truncated = $state(false)
  loading = $state(false)
  error = $state<string | null>(null)

  sort = $state<SortKey>('name')
  desc = $state(false)
  hidden = $state(false)
  view = $state<ViewMode>('grid')

  /** Selected virtual paths. A Set keyed by path rather than by index, so a
   *  refresh that reorders the listing does not move the selection. */
  selected = $state<Set<string>>(new Set())

  /** Guards against an out-of-order response overwriting a newer one when the
   *  user clicks through folders faster than the server answers. */
  #seq = 0

  async load(path = this.path) {
    if (path === TRASH_PATH) {
      this.openTrash()
      return
    }
    const mine = ++this.#seq
    this.loading = true
    this.error = null
    try {
      const q = new URLSearchParams({
        path,
        sort: this.sort,
        order: this.desc ? 'desc' : 'asc',
      })
      if (this.hidden) q.set('hidden', '1')
      const page = await api.get<Page>(`/api/fs/list?${q}`)
      if (mine !== this.#seq) return
      this.path = page.path
      this.entries = page.entries
      this.total = page.total
      this.truncated = page.truncated ?? false
      this.selected = new Set()
    } catch (e) {
      if (mine !== this.#seq) return
      this.error = e instanceof Error ? e.message : String(e)
      this.entries = []
      this.total = 0
    } finally {
      if (mine === this.#seq) this.loading = false
    }
  }

  /** The Trash view is not a directory, so it gets a sentinel path rather than
   *  a load. Using a sentinel keeps one "where am I" value instead of a second
   *  mode flag that every component would have to check. */
  openTrash() {
    this.path = TRASH_PATH
    this.entries = []
    this.selected = new Set()
    this.error = null
  }

  open(entry: Entry) {
    if (entry.kind === 'dir') this.load(entry.path)
  }

  goUp() {
    if (this.path === '/') return
    const parent = this.path.slice(0, this.path.lastIndexOf('/')) || '/'
    this.load(parent)
  }

  setSort(key: SortKey) {
    if (this.sort === key) this.desc = !this.desc
    else {
      this.sort = key
      this.desc = false
    }
    this.load()
  }

  setView(v: ViewMode) {
    this.view = v
  }

  toggleHidden() {
    this.hidden = !this.hidden
    this.load()
  }

  isSelected(path: string) {
    return this.selected.has(path)
  }

  toggle(path: string) {
    const next = new Set(this.selected)
    next.has(path) ? next.delete(path) : next.add(path)
    this.selected = next
  }

  selectOnly(path: string) {
    this.selected = new Set([path])
  }

  selectAll(on: boolean) {
    this.selected = on ? new Set(this.entries.map((e) => e.path)) : new Set()
  }
}

export const browse = new BrowseState()

/** Breadcrumb segments, Root first. */
export function crumbs(path: string): Array<{ name: string; path: string }> {
  const out = [{ name: 'Root', path: '/' }]
  let acc = ''
  for (const seg of path.split('/').filter(Boolean)) {
    acc += '/' + seg
    out.push({ name: seg, path: acc })
  }
  return out
}
