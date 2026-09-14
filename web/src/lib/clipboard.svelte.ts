// Cut/copy/paste is client-side state: paste is what sends the move or copy
// request. Nothing is held server-side between the two, so a reload simply
// clears the clipboard rather than leaving an orphaned pending operation.
import type { Entry } from './types'

class Clipboard {
  paths = $state<string[]>([])
  mode = $state<'copy' | 'cut' | null>(null)

  copy(entries: Entry[]) {
    this.paths = entries.map((e) => e.path)
    this.mode = 'copy'
  }

  cut(entries: Entry[]) {
    this.paths = entries.map((e) => e.path)
    this.mode = 'cut'
  }

  clear() {
    this.paths = []
    this.mode = null
  }

  get has() {
    return this.paths.length > 0 && this.mode !== null
  }

  /** True while an entry is staged for a move, so the tile can dim. */
  isCutting(path: string) {
    return this.mode === 'cut' && this.paths.includes(path)
  }
}

export const clipboard = new Clipboard()
