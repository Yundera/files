// The browser half of uploads.
//
// tus-js-client owns the hard part — chunking, retries, offset negotiation,
// resuming after a dropped connection — so this module is only a queue and the
// state the tray renders.
import * as tus from 'tus-js-client'
import { browse } from './browse.svelte'

export type UploadStatus = 'uploading' | 'paused' | 'done' | 'error'

export interface UploadItem {
  id: string
  name: string
  size: number
  uploaded: number
  status: UploadStatus
  error?: string
}

let nextId = 1

class UploadQueue {
  items = $state<UploadItem[]>([])
  /** Set false by /api/config when the server could not open its staging area. */
  enabled = $state(true)

  #handles = new Map<string, tus.Upload>()

  get active() {
    return this.items.filter((i) => i.status === 'uploading' || i.status === 'paused')
  }

  /** Queue a set of files for the given directory.
   *
   *  `relativePath` comes from a folder drop (webkitRelativePath or the entry
   *  walk) and is what lets a dropped directory keep its structure instead of
   *  flattening into one folder. */
  add(files: Array<{ file: File; relativePath?: string }>, dest: string) {
    for (const { file, relativePath } of files) {
      const id = String(nextId++)
      const item: UploadItem = {
        id,
        name: relativePath || file.name,
        size: file.size,
        uploaded: 0,
        status: 'uploading',
      }
      this.items = [...this.items, item]

      const upload = new tus.Upload(file, {
        endpoint: '/api/tus/',
        // Resume across a page reload: the client stores the upload URL keyed
        // by file fingerprint, so a refresh mid-upload picks up where it left
        // off rather than starting again.
        storeFingerprintForResuming: true,
        removeFingerprintOnSuccess: true,
        retryDelays: [0, 1000, 3000, 5000, 10000],
        chunkSize: 8 * 1024 * 1024,
        metadata: {
          filename: file.name,
          dest,
          relativePath: relativePath ?? '',
          filetype: file.type,
        },
        onProgress: (sent, total) => this.#patch(id, { uploaded: sent, size: total }),
        onSuccess: () => {
          this.#patch(id, { status: 'done', uploaded: item.size })
          this.#handles.delete(id)
          // The server broadcasts a dir-changed event on commit, but only the
          // uploader is guaranteed to be looking at that folder — refresh
          // directly so the file appears without waiting for the round trip.
          if (dest === browse.path) browse.load()
        },
        onError: (err) => {
          this.#patch(id, { status: 'error', error: String(err) })
          this.#handles.delete(id)
        },
      })
      this.#handles.set(id, upload)
      upload.start()
    }
  }

  #patch(id: string, patch: Partial<UploadItem>) {
    this.items = this.items.map((i) => (i.id === id ? { ...i, ...patch } : i))
  }

  pause(id: string) {
    this.#handles.get(id)?.abort()
    this.#patch(id, { status: 'paused' })
  }

  resume(id: string) {
    const h = this.#handles.get(id)
    if (!h) return
    this.#patch(id, { status: 'uploading', error: undefined })
    h.start()
  }

  /** Cancel and ask the server to drop the partial upload, so the staging area
   *  does not keep the bytes for a day waiting on the collector. */
  cancel(id: string) {
    const h = this.#handles.get(id)
    if (h) void h.abort(true)
    this.#handles.delete(id)
    this.remove(id)
  }

  remove(id: string) {
    this.items = this.items.filter((i) => i.id !== id)
  }

  clearFinished() {
    this.items = this.items.filter((i) => i.status !== 'done')
  }
}

export const uploads = new UploadQueue()

/** Walk a DataTransfer into a flat list of files, preserving folder structure.
 *
 *  Dropping a folder gives directory entries rather than files, and the only way
 *  to read them is the non-standard webkitGetAsEntry API — which every current
 *  browser implements and none has replaced. Without this, dropping a folder
 *  silently uploads nothing. */
export async function filesFromDrop(dt: DataTransfer): Promise<Array<{ file: File; relativePath?: string }>> {
  const out: Array<{ file: File; relativePath?: string }> = []

  const readEntry = async (entry: any, prefix: string): Promise<void> => {
    if (!entry) return
    if (entry.isFile) {
      const file = await new Promise<File>((res, rej) => entry.file(res, rej))
      out.push({ file, relativePath: prefix ? `${prefix}/${file.name}` : undefined })
      return
    }
    if (entry.isDirectory) {
      const reader = entry.createReader()
      // readEntries returns at most 100 at a time and must be called until it
      // returns an empty batch — a single call silently truncates a large folder.
      for (;;) {
        const batch: any[] = await new Promise((res, rej) => reader.readEntries(res, rej))
        if (batch.length === 0) break
        for (const child of batch) {
          await readEntry(child, prefix ? `${prefix}/${entry.name}` : entry.name)
        }
      }
    }
  }

  const entries = [...dt.items]
    .filter((i) => i.kind === 'file')
    .map((i) => (i as any).webkitGetAsEntry?.())
    .filter(Boolean)

  if (entries.length > 0) {
    await Promise.all(entries.map((e) => readEntry(e, '')))
    return out
  }
  // Fallback for a browser without the entry API: plain files, no structure.
  return [...dt.files].map((file) => ({ file }))
}
