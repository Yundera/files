// The live channel. One WebSocket multiplexes jobs, trash and directory hints,
// matching internal/live/hub.go.
import { browse } from './browse.svelte'

export interface JobState {
  id: string
  kind: 'copy' | 'move' | 'delete'
  phase: 'running' | 'done' | 'error' | 'cancelled'
  message: string
  dest?: string
  done: number
  total: number
  items: number
  totalItems: number
  error?: string
}

class LiveState {
  jobs = $state<JobState[]>([])
  connected = $state(false)

  #socket: WebSocket | null = null
  #retry = 0

  connect() {
    if (this.#socket) return
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const sock = new WebSocket(`${proto}//${location.host}/ws`)
    this.#socket = sock

    sock.onopen = () => {
      this.connected = true
      this.#retry = 0
      for (const ch of ['jobs', 'trash', 'dir']) {
        sock.send(JSON.stringify({ type: 'subscribe', channel: ch }))
      }
    }
    sock.onmessage = (ev) => {
      let env: { channel?: string; data?: unknown }
      try {
        env = JSON.parse(ev.data)
      } catch {
        return
      }
      switch (env.channel) {
        case 'jobs':
          this.jobs = (env.data as JobState[]) ?? []
          break
        case 'dir': {
          // The server sends the path, not the listing. Re-read only if the
          // user is actually looking at that directory — otherwise a background
          // job would push a page of entries to every open browser.
          const { path } = (env.data as { path: string }) ?? {}
          if (path === browse.path) browse.load()
          break
        }
        case 'trash':
          trashChanged.notify()
          break
      }
    }
    sock.onclose = () => {
      this.connected = false
      this.#socket = null
      // Back off to a ceiling rather than hammering a server that is restarting.
      this.#retry = Math.min(this.#retry + 1, 6)
      setTimeout(() => this.connect(), 250 * 2 ** this.#retry)
    }
    sock.onerror = () => sock.close()
  }

  /** Jobs worth showing in the tray: everything running, plus recent outcomes. */
  get visible() {
    return this.jobs
  }

  get active() {
    return this.jobs.filter((j) => j.phase === 'running')
  }
}

export const live = new LiveState()

/** Minimal pub-sub so the Trash view can refresh without importing the socket. */
class Notifier {
  #subs = new Set<() => void>()
  subscribe(fn: () => void) {
    this.#subs.add(fn)
    return () => this.#subs.delete(fn)
  }
  notify() {
    for (const fn of this.#subs) fn()
  }
}
export const trashChanged = new Notifier()
