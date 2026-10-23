<script lang="ts">
  import type { Entry } from '../types'
  import { browse } from '../browse.svelte'
  import { clipboard } from '../clipboard.svelte'
  import * as act from '../actions'
  import { media } from '../media.svelte'
  import Sheet from './Sheet.svelte'

  interface Props {
    x: number
    y: number
    /** The right-clicked entry, or null for the blank-area menu. */
    entry: Entry | null
    onclose: () => void
    onprompt: (kind: 'newFile' | 'newFolder' | 'rename', entry?: Entry) => void
    onstatus: (msg: string) => void
  }
  let { x, y, entry, onclose, onprompt, onstatus }: Props = $props()

  // CasaOS flips the menu when it would overhang. The offsets are its own:
  // 128px wide, 270px tall.
  const left = $derived(window.innerWidth - x - 128 > 0 ? x : Math.max(0, x - 128))
  const top = $derived(window.innerHeight - y - 270 > 0 ? y : Math.max(0, y - 270))

  // Delete is replaced IN PLACE by "Are you sure?" — no submenu, no dialog. The
  // flag resets every time the menu opens, which is why CasaOS keeps the menu
  // open on click.
  let confirming = $state(false)

  /** The items an action applies to: the whole selection when the right-clicked
   *  entry is part of it, otherwise just that entry. */
  const targets = $derived.by(() => {
    if (!entry) return []
    return browse.isSelected(entry.path)
      ? browse.entries.filter((e) => browse.isSelected(e.path))
      : [entry]
  })
  const single = $derived(targets.length === 1)

  // A phone gets the same items as a bottom sheet: there is no cursor to anchor
  // a popover to, and a 128px menu at a fingertip is under that fingertip.
  const sheet = $derived(media.mobile)
  const title = $derived(
    !entry ? undefined : single ? entry.name : `${targets.length} items`,
  )

  async function run(fn: () => unknown | Promise<unknown>) {
    try {
      await fn()
    } catch (e) {
      onstatus(e instanceof Error ? e.message : String(e))
    }
    onclose()
  }

  async function doDownload() {
    const files = targets.filter((t) => t.kind !== 'dir')
    const folders = targets.length - files.length
    if (files.length === 0) {
      onstatus('Folders cannot be downloaded. Use SMB for bulk transfers.')
      onclose()
      return
    }
    await act.download(files.map((f) => f.path))
    // Say how many to expect: Chrome's "allow multiple downloads?" prompt, if
    // dismissed, silently does nothing and raises no error we could catch.
    const note = folders > 0 ? ` ${folders} folder(s) skipped.` : ''
    onstatus(files.length > 1 ? `Downloading ${files.length} files.${note}` : '')
    onclose()
  }
</script>

<!-- The sheet closes on its own scrim; a window-level click would also catch
     the tap that lands inside it. -->
<svelte:window onclick={() => !sheet && onclose()} oncontextmenu={() => !sheet && onclose()} />

{#snippet items()}
  {#if !entry}
    <!-- Blank-area menu. CasaOS gives these items icons; the item menu has none. -->
    <button onclick={() => (onprompt('newFile'), onclose())}>New File</button>
    <button onclick={() => (onprompt('newFolder'), onclose())}>New Folder</button>
    <hr />
    {#if clipboard.has}
      <button onclick={() => run(() => act.paste('overwrite'))}>Paste - Overwrite</button>
      <button onclick={() => run(() => act.paste('skip'))}>Paste - Skip</button>
    {/if}
    <hr />
    <button onclick={() => run(() => browse.load())}>Refresh</button>
  {:else}
    {#if media.touch && !browse.isSelected(entry.path)}
      <!-- The long-press is invisible until someone tells you about it; this is
           the visible way into selection mode. -->
      <button onclick={() => (browse.toggle(entry.path), onclose())}>Select</button>
      <hr />
    {/if}
    <button onclick={doDownload}>Download</button>
    {#if single}
      <button onclick={() => run(() => act.copyPath(entry.path))}>Copy Path</button>
    {/if}
    <hr />
    {#if single}
      <button onclick={() => (onprompt('rename', entry), onclose())}>Rename</button>
    {/if}
    <button onclick={() => (clipboard.cut(targets), onclose())}>Cut</button>
    <button onclick={() => (clipboard.copy(targets), onclose())}>Copy</button>
    <hr />
    {#if !confirming}
      <button
        class="danger"
        onclick={(e) => {
          e.stopPropagation()
          confirming = true
        }}>Delete</button
      >
    {:else}
      <button class="danger" onclick={() => run(() => act.remove(targets.map((t) => t.path)))}>
        Are you sure?
      </button>
    {/if}
  {/if}
{/snippet}

{#if sheet}
  <Sheet {title} {onclose}>{@render items()}</Sheet>
{:else}
  <div class="context-menu" style="left:{left}px; top:{top}px" role="menu" tabindex="-1">
    {@render items()}
  </div>
{/if}

<style>
  .context-menu {
    position: fixed;
    z-index: 100;
    min-width: 8rem;
    background: var(--surface);
    border-radius: var(--radius-card);
    padding: 4px;
    box-shadow:
      0 0.5em 1em -0.125em rgb(10 10 10 / 10%),
      0 0 0 1px rgb(10 10 10 / 2%);
  }
  .context-menu button {
    display: block;
    width: 100%;
    border: 0;
    background: transparent;
    text-align: left;
    white-space: nowrap;
    padding: 0.375rem 0.5rem;
    border-radius: var(--radius-small);
    font-size: var(--font-base);
    color: inherit;
    transition: all 0.25s;
  }
  @media (hover: hover) {
    .context-menu button:hover {
      background: var(--surface-2);
    }
    .context-menu button.danger:hover {
      background: hsl(348, 100%, 95%);
    }
  }
  .context-menu button.danger {
    color: var(--red);
  }
  .context-menu hr {
    border: 0;
    border-top: 1px solid var(--border);
    margin: 4px;
  }
</style>
