// Browsing state. Svelte 5 runes in a .svelte.ts module, so any component can
// import the same live values without a store subscription.
import { api } from './api'
import { isTouch } from './media.svelte'
import type { Entry, Page, SortKey, ViewMode } from './types'

export const TRASH_PATH = '__trash__'

// The sidebar shortcuts. Every entry below the root must exist on disk or the
// shortcut is a dead end, so the server creates them at startup — the matching
// list is `Roots` in internal/bootstrap. Add an entry here and add it there too.
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

  /** `push: false` is for a load that came FROM history (Back/Forward), which
   *  must not stack another entry on top of the one it just restored. */
  async load(path = this.path, push = true) {
    if (path === TRASH_PATH) {
      this.openTrash(push)
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
      remember(page.path, push)
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
  openTrash(push = true) {
    this.path = TRASH_PATH
    remember(TRASH_PATH, push)
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

  /** The tap rule every listing shares. On touch a tap opens, unless a
   *  long-press has started a selection — then taps add and remove. A mouse
   *  keeps the desktop rule: click selects, ctrl/cmd-click toggles, and opening
   *  is the double-click's job. */
  tap(e: MouseEvent, path: string, open: () => void) {
    if (isTouch(e)) {
      if (this.selected.size > 0) this.toggle(path)
      else open()
    } else if (e.ctrlKey || e.metaKey) this.toggle(path)
    else this.selectOnly(path)
  }

  selectAll(on: boolean) {
    this.selected = on ? new Set(this.entries.map((e) => e.path)) : new Set()
  }
}

/** Folder navigation as browser history, so Back — the phone's back gesture
 *  above all — goes up to where you were instead of leaving the app. A reload
 *  of an unchanged folder adds nothing. The first load replaces rather than
 *  pushes, or Back from the start folder would land on itself. */
function remember(path: string, push: boolean) {
  const state = history.state as { path?: string; view?: string } | null
  if (state?.path === path && !state.view) return
  const next = { path }
  if (push && state?.path !== undefined) history.pushState(next, '')
  else history.replaceState(next, '')
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
