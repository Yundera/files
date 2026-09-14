<script lang="ts">
  import { api, ApiError } from '../api'
  import type { EditorHandle } from '../editor'
  import type { Entry } from '../types'

  // CodeMirror and its language modes are ~800 kB, and marked + DOMPurify add
  // more. Importing them statically would put all of it in the initial bundle,
  // so every session that only browses a folder pays for an editor it never
  // opens. These dynamic imports let vite split them into chunks fetched the
  // first time a file is actually opened for editing.
  const loadEditor = () => import('../editor')
  const loadMarkdown = () => import('../markdown')

  interface Doc {
    path: string
    content: string
    modTime: string
    size: number
    language: string
  }

  interface Props {
    entry: Entry
    onclose: () => void
    onstatus: (msg: string) => void
  }
  let { entry, onclose, onstatus }: Props = $props()

  let host: HTMLDivElement | undefined = $state()
  let handle: EditorHandle | undefined
  let doc = $state<Doc | null>(null)
  let loadError = $state<string | null>(null)
  let dirty = $state(false)
  let saving = $state(false)
  let syntaxError = $state<{ message: string; line: number } | null>(null)
  let preview = $state(false)
  let previewHtml = $state('')

  const isMarkdown = $derived(doc?.language === 'markdown')

  $effect(() => {
    let cancelled = false
    api
      .get<Doc>(`/api/file/text?path=${encodeURIComponent(entry.path)}`)
      .then((d) => {
        if (!cancelled) doc = d
      })
      .catch((e) => {
        if (!cancelled) loadError = e instanceof Error ? e.message : String(e)
      })
    return () => {
      cancelled = true
    }
  })

  // Mount CodeMirror once the document has arrived and the host element exists.
  $effect(() => {
    if (!doc || !host || handle) return
    const el = host
    const d = doc
    let disposed = false
    loadEditor()
      .then(({ createEditor }) => {
        // The component can unmount while the chunk is in flight.
        if (disposed) return
        handle = createEditor(el, d.content, d.language, () => {
          dirty = true
          syntaxError = null
        })
      })
      .catch((e) => (loadError = `Could not load the editor: ${e}`))
    return () => {
      disposed = true
      handle?.destroy()
      handle = undefined
    }
  })

  async function refreshPreview() {
    if (!handle) return
    const { renderMarkdown } = await loadMarkdown()
    if (handle) previewHtml = renderMarkdown(handle.getValue())
  }

  $effect(() => {
    if (preview) void refreshPreview()
  })

  async function save() {
    if (!doc || !handle || saving) return
    saving = true
    syntaxError = null
    try {
      const res = await api.put<{ modTime: string }>('/api/file/text', {
        path: doc.path,
        content: handle.getValue(),
        baseMtime: doc.modTime,
      })
      // Carry the new mtime forward, or the next save in this session looks
      // stale against the version we loaded.
      doc = { ...doc, modTime: res.modTime }
      dirty = false
      onstatus('Saved')
    } catch (e) {
      if (e instanceof ApiError && e.status === 422) {
        // The server reports the offending line; jump to it rather than making
        // the user hunt for it.
        const line = e.line ?? 0
        syntaxError = { message: e.message, line }
        if (line > 0) handle.goToLine(line)
      } else if (e instanceof ApiError && e.status === 409) {
        onstatus(`${e.message} — reload before saving, or your change will overwrite theirs.`)
      } else {
        onstatus(e instanceof Error ? e.message : String(e))
      }
    } finally {
      saving = false
    }
  }

  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
      e.preventDefault()
      save()
    }
  }

  function tryClose() {
    if (dirty && !confirm('Discard unsaved changes?')) return
    onclose()
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="editor">
  <div class="bar">
    <span class="name one-line">{entry.name}{dirty ? ' •' : ''}</span>
    <div class="right">
      {#if isMarkdown}
        <button class="ghost" onclick={() => (preview = !preview)}>
          {preview ? 'Hide preview' : 'Preview'}
        </button>
      {/if}
      <button class="primary" onclick={save} disabled={!dirty || saving}>
        {saving ? 'Saving…' : 'Save'}
      </button>
      <button class="ghost" onclick={tryClose}>Close</button>
    </div>
  </div>

  {#if syntaxError}
    <div class="syntax" role="alert">
      {syntaxError.line > 0 ? `Line ${syntaxError.line}: ` : ''}{syntaxError.message}
      <span class="hint">— not saved</span>
    </div>
  {/if}

  {#if loadError}
    <p class="error">{loadError}</p>
  {:else if !doc}
    <p class="muted">Loading…</p>
  {:else}
    <div class="panes" class:split={preview}>
      <div class="pane" bind:this={host}></div>
      {#if preview}
        <!-- eslint-disable-next-line svelte/no-at-html-tags -->
        <div class="pane markdown">{@html previewHtml}</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--surface);
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.625rem 1rem;
    border-bottom: 1px solid var(--border);
  }
  .name {
    font-weight: 600;
  }
  .right {
    display: flex;
    gap: 0.5rem;
    flex-shrink: 0;
  }
  .ghost,
  .primary {
    border: 0;
    border-radius: 9999px;
    padding: 0.25rem 0.875rem;
    font-size: var(--font-base);
  }
  .ghost {
    background: var(--surface-2);
    color: var(--text);
  }
  .primary {
    background: var(--primary);
    color: var(--text-on-accent);
  }
  .primary:disabled {
    opacity: 0.45;
  }
  .syntax {
    background: hsl(348, 100%, 96%);
    color: var(--red);
    padding: 0.5rem 1rem;
    font-size: var(--font-base);
  }
  .hint {
    color: var(--text-muted);
  }
  .panes {
    flex: 1 1 auto;
    display: flex;
    min-height: 0;
  }
  .pane {
    flex: 1 1 50%;
    min-width: 0;
    overflow: auto;
    background: var(--surface-sunken);
  }
  .panes.split .pane + .pane {
    border-left: 1px solid var(--border);
  }
  .markdown {
    background: var(--surface);
    padding: 1rem 1.5rem;
    line-height: 1.6;
  }
  .markdown :global(pre) {
    background: var(--surface-2);
    padding: 0.75rem;
    border-radius: var(--radius-small);
    overflow-x: auto;
  }
  .markdown :global(code) {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.9em;
  }
  .markdown :global(table) {
    border-collapse: collapse;
  }
  .markdown :global(td),
  .markdown :global(th) {
    border: 1px solid var(--border);
    padding: 0.25rem 0.5rem;
  }
  .markdown :global(img) {
    max-width: 100%;
  }
  .error {
    color: var(--red);
    padding: 1.5rem;
  }
  .muted {
    color: var(--text-muted);
    padding: 1.5rem;
  }
</style>
