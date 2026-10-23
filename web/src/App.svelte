<script lang="ts">
  import SideBar from './lib/components/SideBar.svelte'
  import Breadcrumb from './lib/components/Breadcrumb.svelte'
  import GridView from './lib/components/GridView.svelte'
  import ListView from './lib/components/ListView.svelte'
  import EmptyState from './lib/components/EmptyState.svelte'
  import ContextMenu from './lib/components/ContextMenu.svelte'
  import OperationToolbar from './lib/components/OperationToolbar.svelte'
  import JobTray from './lib/components/JobTray.svelte'
  import PromptModal from './lib/components/PromptModal.svelte'
  import TrashView from './lib/components/TrashView.svelte'
  import UploadTray from './lib/components/UploadTray.svelte'
  import Viewer from './lib/components/Viewer.svelte'
  import SearchResults from './lib/components/SearchResults.svelte'
  import Icon from './lib/components/Icon.svelte'
  import Sheet from './lib/components/Sheet.svelte'
  import { isTouch, media } from './lib/media.svelte'
  import { search } from './lib/search.svelte'
  import DropZone from './lib/components/DropZone.svelte'
  import { uploads } from './lib/uploads.svelte'
  import { browse } from './lib/browse.svelte'
  import { clipboard } from './lib/clipboard.svelte'
  import { live } from './lib/live.svelte'
  import * as act from './lib/actions'
  import type { Entry, SortKey } from './lib/types'

  browse.load('/')
  live.connect()

  let menu = $state<{ x: number; y: number; entry: Entry | null } | null>(null)
  let prompt = $state<{ kind: 'newFile' | 'newFolder' | 'rename'; entry?: Entry } | null>(null)
  let viewing = $state<Entry | null>(null)
  let status = $state<string | null>(null)
  // Phone-only chrome: the sidebar as a drawer, the search field folded behind
  // an icon, and the header actions that no longer fit behind a ⋯ sheet.
  let drawerOpen = $state(false)
  let searchOpen = $state(false)
  let moreOpen = $state(false)
  let statusTimer: ReturnType<typeof setTimeout> | undefined

  // Two hidden inputs rather than one: `webkitdirectory` is what turns a file
  // picker into a folder picker, and it cannot be toggled reliably after the
  // element exists.
  let filePicker: HTMLInputElement | undefined = $state()
  let folderPicker: HTMLInputElement | undefined = $state()

  function pick(input?: HTMLInputElement) {
    input?.click()
  }

  function onPicked(e: Event) {
    const input = e.currentTarget as HTMLInputElement
    const chosen = [...(input.files ?? [])].map((file) => ({
      file,
      // webkitRelativePath is set only by the folder picker; it is what keeps a
      // chosen directory's structure instead of flattening it.
      relativePath: file.webkitRelativePath || undefined,
    }))
    if (chosen.length > 0) uploads.add(chosen, browse.path)
    input.value = '' // so picking the same file twice still fires a change
  }

  function setStatus(msg: string) {
    if (!msg) return
    status = msg
    clearTimeout(statusTimer)
    statusTimer = setTimeout(() => (status = null), 6000)
  }

  const allSelected = $derived(
    browse.entries.length > 0 && browse.selected.size === browse.entries.length,
  )
  const selectedEntries = $derived(browse.entries.filter((e) => browse.isSelected(e.path)))

  /** Double-click (or Enter): a folder navigates, a file opens in the viewer.
   *  Opening a folder from search results leaves the search — otherwise the
   *  results stay on screen while the breadcrumb says somewhere else. */
  function openEntry(entry: Entry) {
    if (entry.kind === 'dir') {
      search.clear()
      browse.open(entry)
    } else {
      showEntry(entry)
    }
  }

  function openMenu(e: MouseEvent, entry: Entry | null) {
    e.preventDefault()
    e.stopPropagation()
    // A mouse right-click selects what it lands on, as on desktop. A finger's ⋮
    // must not: on touch any selection is selection mode, and the menu closing
    // would leave the user in it without having asked.
    if (entry && !browse.isSelected(entry.path) && !isTouch(e)) browse.selectOnly(entry.path)
    menu = { x: e.clientX, y: e.clientY, entry }
  }

  // ── Viewer as a history entry ────────────────────────────────────────────
  // On a phone the back gesture is how anything full-screen is closed. Opening a
  // file therefore pushes an entry that Back pops, and closing it any other way
  // pops that entry too, so Back afterwards goes up a folder as expected.
  type NavState = { path?: string; view?: string } | null

  function showEntry(entry: Entry) {
    viewing = entry
    history.pushState({ path: browse.path, view: entry.path }, '')
  }

  function closeViewer() {
    if ((history.state as NavState)?.view) history.back()
    else viewing = null
  }

  function onPop(e: PopStateEvent) {
    const state = e.state as NavState
    if (!state?.view) viewing = null
    else viewing = viewable().find((v) => v.path === state.view) ?? null
    if (state?.path !== undefined && state.path !== browse.path) browse.load(state.path, false)
  }

  /** What the viewer swipes through: the files of whatever is on screen. */
  function viewable() {
    return (search.active ? search.results : browse.entries).filter((e) => e.kind !== 'dir')
  }

  function stepViewer(delta: number) {
    if (!viewing) return
    const files = viewable()
    const next = files[files.findIndex((f) => f.path === viewing!.path) + delta]
    if (!next) return
    viewing = next
    history.replaceState({ path: browse.path, view: next.path }, '')
  }

  const sortKeys: Array<{ key: SortKey; label: string }> = [
    { key: 'name', label: 'Name' },
    { key: 'modified', label: 'Date modified' },
    { key: 'size', label: 'Size' },
    { key: 'kind', label: 'Type' },
  ]

  async function submitPrompt(value: string) {
    const p = prompt
    prompt = null
    if (!p) return
    try {
      if (p.kind === 'newFolder') await act.mkdir(value)
      else if (p.kind === 'newFile') await act.touch(value)
      else if (p.entry) await act.rename(p.entry.path, value)
    } catch (e) {
      setStatus(e instanceof Error ? e.message : String(e))
    }
  }

  async function onKey(e: KeyboardEvent) {
    const typing = ['INPUT', 'TEXTAREA'].includes((e.target as HTMLElement)?.tagName)
    if (typing) return
    const mod = e.ctrlKey || e.metaKey
    try {
      if (e.key === 'Backspace') {
        e.preventDefault()
        browse.goUp()
      } else if (e.key === 'Delete' && selectedEntries.length > 0) {
        await act.remove(selectedEntries.map((s) => s.path))
      } else if (mod && e.key === 'c' && selectedEntries.length > 0) {
        clipboard.copy(selectedEntries)
      } else if (mod && e.key === 'x' && selectedEntries.length > 0) {
        clipboard.cut(selectedEntries)
      } else if (mod && e.key === 'v' && clipboard.has) {
        await act.paste('keepBoth')
      } else if (mod && e.key === 'a') {
        e.preventDefault()
        browse.selectAll(true)
      } else if (e.key === 'Escape') {
        // The viewer has its own Escape handler; don't also wipe the selection
        // underneath it, or closing a preview loses the user's place.
        if (viewing) return
        if (search.active) {
          search.clear()
          return
        }
        browse.selectAll(false)
        clipboard.clear()
      }
    } catch (err) {
      setStatus(err instanceof Error ? err.message : String(err))
    }
  }

  const inTrash = $derived(browse.path === '__trash__')
</script>

<svelte:window onkeydown={onKey} onpopstate={onPop} />

<input bind:this={filePicker} type="file" multiple hidden onchange={onPicked} />
<input bind:this={folderPicker} type="file" webkitdirectory hidden onchange={onPicked} />

<div
  class="file-panel"
  style:--bottom-bar={media.mobile && selectedEntries.length > 0 && !inTrash && !search.active
    ? 'calc(60px + env(safe-area-inset-bottom))'
    : '0px'}
>
  <!-- On desktop .drawer is display: contents, so the sidebar sits in the row
       exactly as before; on a phone it is an off-canvas panel over the files. -->
  <div class="drawer" class:open={drawerOpen}>
    <SideBar onnavigate={() => (drawerOpen = false)} />
  </div>
  {#if media.mobile && drawerOpen}
    <div class="drawer-scrim" role="presentation" onclick={() => (drawerOpen = false)}></div>
  {/if}

  <div class="content">
    <header class:searching={media.mobile && searchOpen}>
      {#if media.mobile}
        <button class="ghost burger" aria-label="Locations" onclick={() => (drawerOpen = true)}>
          <Icon name="menu" />
        </button>
      {/if}
      {#if inTrash}
        <strong>Trash</strong>
      {:else}
        <Breadcrumb />
      {/if}
      <div class="actions">
        <div class="search">
          <input
            type="search"
            placeholder="Search {inTrash ? '' : browse.path}"
            bind:value={search.query}
            oninput={() => search.schedule(browse.path)}
            onkeydown={(e) => {
              if (e.key === 'Escape') search.clear()
              if (e.key === 'Enter') search.run(browse.path)
            }}
            disabled={inTrash}
          />
          {#if search.active}
            <button class="clear" title="Clear search" onclick={() => search.clear()}>✕</button>
          {/if}
        </div>
        {#if media.mobile}
          {#if searchOpen}
            <button
              class="ghost close-search"
              aria-label="Close search"
              onclick={() => ((searchOpen = false), search.clear())}
            >
              <Icon name="close" />
            </button>
          {:else}
            <button
              class="ghost"
              aria-label="Search"
              disabled={inTrash}
              onclick={() => {
                searchOpen = true
                // After the field is shown, or there is nothing to focus yet.
                requestAnimationFrame(() =>
                  document.querySelector<HTMLInputElement>('.search input')?.focus(),
                )
              }}
            >
              <Icon name="search" />
            </button>
          {/if}
        {/if}
        <JobTray />
        {#if clipboard.has}
          <button
            class="ghost"
            title="Paste {clipboard.paths.length} item{clipboard.paths.length === 1 ? '' : 's'}"
            aria-label="Paste {clipboard.paths.length} item{clipboard.paths.length === 1 ? '' : 's'}"
            onclick={() => act.paste('keepBoth').catch((e) => setStatus(e.message))}
          >
            <Icon name="paste" />
            <span class="badge">{clipboard.paths.length}</span>
          </button>
        {/if}
        {#if !(media.mobile && inTrash)}
          <button
            class="ghost"
            title="Upload files"
            aria-label="Upload files"
            onclick={() => pick(filePicker)}
          >
            <Icon name="uploadFile" />
          </button>
        {/if}
        {#if media.mobile}
          <button class="ghost" aria-label="More actions" onclick={() => (moreOpen = true)}>
            <Icon name="more" />
          </button>
        {:else}
          <button
            class="ghost"
            title="Upload a folder"
            aria-label="Upload a folder"
            onclick={() => pick(folderPicker)}
          >
            <Icon name="uploadFolder" />
          </button>
          <button
            class="ghost"
            title="New folder"
            aria-label="New folder"
            onclick={() => (prompt = { kind: 'newFolder' })}
          >
            <Icon name="folderPlus" />
          </button>
          <span class="sep" aria-hidden="true"></span>
          <button
            class="ghost"
            class:on={browse.hidden}
            title={browse.hidden ? 'Hide hidden files' : 'Show hidden files'}
            aria-label={browse.hidden ? 'Hide hidden files' : 'Show hidden files'}
            aria-pressed={browse.hidden}
            onclick={() => browse.toggleHidden()}
          >
            <Icon name={browse.hidden ? 'eyeOff' : 'eye'} />
          </button>
          <button
            class="ghost"
            title={browse.view === 'grid' ? 'Switch to list view' : 'Switch to grid view'}
            aria-label={browse.view === 'grid' ? 'Switch to list view' : 'Switch to grid view'}
            onclick={() => browse.setView(browse.view === 'grid' ? 'list' : 'grid')}
          >
            <Icon name={browse.view === 'grid' ? 'list' : 'grid'} />
          </button>
        {/if}
      </div>
    </header>

    {#if !inTrash && !search.active && browse.entries.length > 0}
      <div class="tool-bar">
        <label>
          <input
            type="checkbox"
            checked={allSelected}
            onchange={(e) => browse.selectAll(e.currentTarget.checked)}
          />
          {browse.selected.size > 0 ? `${browse.selected.size} selected` : `${browse.total} items`}
        </label>
        {#if browse.truncated}
          <span class="warn">Showing the first {browse.entries.length} of a very large folder</span>
        {/if}
      </div>
    {/if}

    <DropZone onstatus={setStatus}>
      <div
        class="listing scrollbars-light"
        role="presentation"
        oncontextmenu={(e) => !inTrash && openMenu(e, null)}
      >
        {#if search.active}
          <SearchResults onopen={openEntry} />
        {:else if inTrash}
          <TrashView onstatus={setStatus} />
        {:else if browse.error}
          <p class="error">{browse.error}</p>
        {:else if browse.loading && browse.entries.length === 0}
          <p class="muted">Loading…</p>
        {:else if browse.entries.length === 0}
          <EmptyState
            onaction={(kind) => {
              if (kind === 'newFile') prompt = { kind: 'newFile' }
              else if (kind === 'newFolder') prompt = { kind: 'newFolder' }
              else if (kind === 'uploadFiles') pick(filePicker)
              else pick(folderPicker)
            }}
          />
        {:else if browse.view === 'grid'}
          <GridView onmenu={openMenu} onopen={openEntry} />
        {:else}
          <ListView onmenu={openMenu} onopen={openEntry} />
        {/if}

        {#if !inTrash && !search.active}
          <OperationToolbar
            onstatus={setStatus}
            onmore={(e) => selectedEntries[0] && openMenu(e, selectedEntries[0])}
          />
        {/if}
      </div>
      <UploadTray />
    </DropZone>

    {#if status}
      <div class="status" role="status">{status}</div>
    {/if}
  </div>
</div>

{#if viewing}
  <!-- Keyed: stepping to the next file is a fresh viewer, not the previous one's
       loaded text or refusal carried over to a different file. -->
  {#key viewing.path}
    <Viewer entry={viewing} onclose={closeViewer} onstatus={setStatus} onnav={stepViewer} />
  {/key}
{/if}

{#if moreOpen}
  <Sheet onclose={() => (moreOpen = false)}>
    {#if !inTrash}
      <button onclick={() => ((moreOpen = false), (prompt = { kind: 'newFolder' }))}>
        <Icon name="folderPlus" /> New folder
      </button>
      <button onclick={() => ((moreOpen = false), (prompt = { kind: 'newFile' }))}>
        <Icon name="filePlus" /> New file
      </button>
      {#if !media.touch}
        <button onclick={() => ((moreOpen = false), pick(folderPicker))}>
          <Icon name="uploadFolder" /> Upload a folder
        </button>
      {/if}
      {#if clipboard.has}
        <button onclick={() => ((moreOpen = false), act.paste('overwrite').catch((e) => setStatus(e.message)))}>
          <Icon name="paste" /> Paste - Overwrite
        </button>
        <button onclick={() => ((moreOpen = false), act.paste('skip').catch((e) => setStatus(e.message)))}>
          <Icon name="paste" /> Paste - Skip
        </button>
      {/if}
      <hr />
    {/if}
    <button onclick={() => ((moreOpen = false), browse.setView(browse.view === 'grid' ? 'list' : 'grid'))}>
      <Icon name={browse.view === 'grid' ? 'list' : 'grid'} />
      {browse.view === 'grid' ? 'List view' : 'Grid view'}
    </button>
    <button class:on={browse.hidden} onclick={() => ((moreOpen = false), browse.toggleHidden())}>
      <Icon name={browse.hidden ? 'eyeOff' : 'eye'} />
      {browse.hidden ? 'Hide hidden files' : 'Show hidden files'}
    </button>
    <hr />
    <!-- The list view's sortable header is hidden on a phone, so sorting lives
         here. Tapping the current key flips the order, as the header does. -->
    {#each sortKeys as s (s.key)}
      <button class:on={browse.sort === s.key} onclick={() => ((moreOpen = false), browse.setSort(s.key))}>
        <span class="sort-mark">{browse.sort === s.key ? (browse.desc ? '↓' : '↑') : ''}</span>
        Sort by {s.label}
      </button>
    {/each}
    <hr />
    <button onclick={() => ((moreOpen = false), browse.load())}><Icon name="refresh" /> Refresh</button>
  </Sheet>
{/if}

{#if menu}
  <ContextMenu
    x={menu.x}
    y={menu.y}
    entry={menu.entry}
    onclose={() => (menu = null)}
    onprompt={(kind, entry) => (prompt = { kind, entry })}
    onstatus={setStatus}
  />
{/if}

{#if prompt}
  <PromptModal
    title={prompt.kind === 'rename'
      ? 'Rename'
      : prompt.kind === 'newFolder'
        ? 'New Folder'
        : 'New File'}
    value={prompt.entry?.name ?? ''}
    confirmLabel={prompt.kind === 'rename' ? 'Rename' : 'Create'}
    onsubmit={submitPrompt}
    oncancel={() => (prompt = null)}
  />
{/if}

<style>
  .file-panel {
    display: flex;
    height: 100vh;
    /* dvh: on iOS 100vh reaches behind the address bar, hiding the bottom edge. */
    height: 100dvh;
    background: var(--surface);
  }
  .drawer {
    display: contents;
  }
  .content {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    position: relative;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 1.25rem 1.25rem 0.5rem 1.5rem;
    border-bottom: 1px solid rgba(133, 149, 163, 0.1215686275);
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    /* Shrinks, but never below the buttons: the search field is the elastic part
       of the row. Pinned at flex-shrink: 0 the whole row held its width and the
       breadcrumb absorbed every pixel of the loss, down to nothing. */
    flex-shrink: 1;
    min-width: 0;
  }
  /* Icon buttons, not text ones. Five labelled actions plus the search field ran
     the breadcrumb off the header at anything under ~1400px — at 900px the
     breadcrumb disappeared entirely and the last button was clipped. Each button
     carries a `title` and an `aria-label`, so the meaning is still there for a
     hover and for a screen reader. */
  .ghost {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2rem;
    height: 2rem;
    border: 0;
    background: transparent;
    color: var(--breadcrumb-link);
    border-radius: var(--radius-small);
    transition: all 0.25s;
  }
  @media (hover: hover) {
    .ghost:hover {
      background: var(--sidebar-bg);
      color: var(--primary);
    }
  }
  .ghost:disabled {
    opacity: 0.4;
  }
  .ghost:focus-visible {
    outline: 2px solid var(--primary);
    outline-offset: -1px;
  }
  /* Hidden-files is a toggle, so it has to read as on or off, not merely as a
     glyph that swapped to one the user has never seen before. */
  .ghost.on {
    color: var(--primary);
    background: var(--primary-light);
  }
  /* How many items are on the clipboard — the one thing an icon cannot say. */
  .badge {
    position: absolute;
    top: 0;
    right: 0;
    min-width: 14px;
    padding: 0 3px;
    border-radius: 7px;
    background: var(--primary);
    color: var(--text-on-accent);
    font-size: 10px;
    line-height: 14px;
    text-align: center;
  }
  .sep {
    width: 1px;
    height: 1.25rem;
    background: var(--border);
    margin: 0 0.25rem;
  }
  .search {
    position: relative;
    display: flex;
    align-items: center;
  }
  .search {
    min-width: 0;
  }
  .search input {
    font: inherit;
    font-size: var(--font-base);
    width: 14rem;
    min-width: 6rem;
    max-width: 100%;
    padding: 0.3rem 1.75rem 0.3rem 0.625rem;
    border: 1px solid var(--border);
    border-radius: 9999px;
    background: var(--surface-2);
  }
  .search input:focus {
    outline: 2px solid var(--primary);
    outline-offset: -1px;
    background: var(--surface);
  }
  /* Chrome and Safari draw their own ✕ in a type=search field, next to ours. */
  .search input::-webkit-search-cancel-button {
    appearance: none;
  }
  .search input:disabled {
    opacity: 0.5;
  }
  .search .clear {
    position: absolute;
    right: 0.375rem;
    border: 0;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--font-meta);
    padding: 0.125rem 0.25rem;
  }
  .tool-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 1.75rem;
    padding: 0 1.5rem;
    margin: 0.5rem 0;
    font-size: var(--font-base);
  }
  .tool-bar label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .warn {
    color: var(--orange);
    font-size: var(--font-meta);
  }
  .listing {
    flex: 1 1 auto;
    overflow-y: auto;
    position: relative;
  }
  .error {
    color: var(--red);
    padding: 1.5rem;
  }
  .muted {
    color: var(--text-muted);
    padding: 1.5rem;
  }
  .status {
    position: absolute;
    bottom: 1rem;
    left: 1.5rem;
    background: var(--toolbar-bg);
    color: #fff;
    padding: 0.5rem 0.875rem;
    border-radius: var(--radius-small);
    font-size: var(--font-base);
    z-index: 150;
    max-width: 40rem;
  }
  .sort-mark {
    display: inline-block;
    width: 18px;
    text-align: center;
  }

  /* ── Phone ─────────────────────────────────────────────────────────────── */
  @media (max-width: 768px) {
    .drawer {
      display: block;
      position: fixed;
      top: 0;
      bottom: 0;
      left: 0;
      z-index: 250;
      transform: translateX(-100%);
      /* Hidden once off-screen, not merely moved: its shadow would bleed in at
         the left edge, and its buttons would still take keyboard focus. */
      visibility: hidden;
      transition:
        transform 0.2s ease-out,
        visibility 0s 0.2s;
    }
    .drawer.open {
      transform: none;
      visibility: visible;
      box-shadow: 0 0 1.5em rgb(10 10 10 / 20%);
      transition: transform 0.2s ease-out;
    }
    .drawer-scrim {
      position: fixed;
      inset: 0;
      z-index: 249;
      background: var(--scrim);
    }
    header {
      gap: 0.25rem;
      padding: calc(0.5rem + env(safe-area-inset-top)) 0.5rem 0.5rem;
      min-height: 3.5rem;
    }
    header > :global(.breadcrumb-container),
    header > strong {
      flex: 1 1 auto;
      max-width: none;
    }
    .actions {
      gap: 0.125rem;
      flex-shrink: 0;
    }
    .ghost {
      width: 40px;
      height: 40px;
    }
    .sep {
      display: none;
    }
    /* The field only exists while search is open, and then it is the row. */
    .search {
      display: none;
    }
    header.searching > :global(.breadcrumb-container),
    header.searching > strong,
    header.searching .burger {
      display: none;
    }
    header.searching .actions {
      flex: 1 1 auto;
    }
    header.searching .actions > :global(:not(.search):not(.close-search)) {
      display: none;
    }
    header.searching .search {
      display: flex;
      flex: 1 1 auto;
    }
    header.searching .search input {
      width: 100%;
      font-size: 1rem; /* under 16px, iOS zooms the page on focus */
      padding: 0.5rem 2rem 0.5rem 0.875rem;
    }
    .tool-bar {
      padding: 0 1rem;
    }
    .tool-bar input {
      width: 18px;
      height: 18px;
    }
    .listing {
      padding-bottom: var(--bottom-bar);
    }
    .status {
      left: 0.5rem;
      right: 0.5rem;
      max-width: none;
      bottom: calc(0.75rem + var(--bottom-bar) + env(safe-area-inset-bottom));
    }
  }
</style>
