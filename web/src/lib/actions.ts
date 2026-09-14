// Every mutating call the UI can make. Kept in one module so the context menu,
// the toolbar and the keyboard shortcuts cannot drift apart.
import { api } from './api'
import { browse } from './browse.svelte'
import { clipboard } from './clipboard.svelte'
import type { Policy } from './types'

export async function mkdir(name: string) {
  await api.post('/api/fs/mkdir', { path: browse.path, name })
  await browse.load()
}

export async function touch(name: string) {
  await api.post('/api/fs/touch', { path: browse.path, name })
  await browse.load()
}

export async function rename(path: string, newName: string) {
  await api.post('/api/fs/rename', { path, newName })
  await browse.load()
}

export async function remove(paths: string[]) {
  await api.post('/api/fs/delete', { paths })
}

/** Paste whatever is on the clipboard into the current directory. */
export async function paste(conflict: Policy) {
  if (!clipboard.has) return
  const route = clipboard.mode === 'cut' ? '/api/fs/move' : '/api/fs/copy'
  await api.post(route, { sources: clipboard.paths, dest: browse.path, conflict })
  // A cut is consumed by its paste; a copy stays on the clipboard so it can be
  // pasted into several places, which is what a desktop file manager does.
  if (clipboard.mode === 'cut') clipboard.clear()
}

export async function cancelJob(id: string) {
  await api.post(`/api/jobs/${encodeURIComponent(id)}/cancel`)
}

/** Download a selection.
 *
 *  One browser download per file, spaced out: browsers rate-limit or silently
 *  drop rapid programmatic downloads, and Safari is strictest. Chrome also shows
 *  an "allow multiple downloads?" prompt once per origin — if the user dismisses
 *  it nothing happens and there is no error to catch, which is why the caller
 *  tells them how many files to expect.
 *
 *  Folders are skipped: a folder has no URL, and with no archiver there is
 *  nothing to hand the browser. */
export async function download(paths: string[]): Promise<number> {
  let sent = 0
  for (const p of paths) {
    const a = document.createElement('a')
    a.href = `/api/fs/raw?path=${encodeURIComponent(p)}`
    a.download = p.slice(p.lastIndexOf('/') + 1)
    document.body.appendChild(a)
    a.click()
    a.remove()
    sent++
    if (sent < paths.length) await new Promise((r) => setTimeout(r, 300))
  }
  return sent
}

export function copyPath(path: string) {
  return navigator.clipboard?.writeText(path)
}
