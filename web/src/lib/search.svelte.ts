// Subtree search state.
import { api } from './api'
import type { Entry } from './types'

interface SearchResult {
  query: string
  root: string
  entries: Entry[]
  truncated: boolean
  elapsedMs: number
}

class SearchState {
  query = $state('')
  results = $state<Entry[]>([])
  truncated = $state(false)
  elapsedMs = $state(0)
  root = $state('/')
  active = $state(false)
  loading = $state(false)
  error = $state<string | null>(null)

  #seq = 0
  #debounce: ReturnType<typeof setTimeout> | undefined

  /** Debounced so typing does not launch a filesystem walk per keystroke. */
  schedule(path: string) {
    clearTimeout(this.#debounce)
    const q = this.query.trim()
    if (q === '') {
      this.clear()
      return
    }
    this.active = true
    this.#debounce = setTimeout(() => this.run(path), 250)
  }

  async run(path: string) {
    const q = this.query.trim()
    if (q === '') return
    const mine = ++this.#seq
    this.loading = true
    this.error = null
    try {
      const res = await api.get<SearchResult>(
        `/api/fs/search?path=${encodeURIComponent(path)}&q=${encodeURIComponent(q)}`,
      )
      if (mine !== this.#seq) return
      this.results = res.entries
      this.truncated = res.truncated
      this.elapsedMs = res.elapsedMs
      this.root = res.root
      this.active = true
    } catch (e) {
      if (mine !== this.#seq) return
      this.error = e instanceof Error ? e.message : String(e)
      this.results = []
    } finally {
      if (mine === this.#seq) this.loading = false
    }
  }

  clear() {
    clearTimeout(this.#debounce)
    this.#seq++
    this.query = ''
    this.results = []
    this.truncated = false
    this.active = false
    this.loading = false
    this.error = null
  }
}

export const search = new SearchState()
