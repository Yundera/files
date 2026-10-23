<script lang="ts">
  import { browse } from '../browse.svelte'
  import { clipboard } from '../clipboard.svelte'
  import * as act from '../actions'
  import { media } from '../media.svelte'
  import Icon from './Icon.svelte'

  interface Props {
    onstatus: (msg: string) => void
    /** The phone bar's ⋯: the item menu for the selection (Rename, Copy Path). */
    onmore: (e: MouseEvent) => void
  }
  let { onstatus, onmore }: Props = $props()

  // A tap is likelier to miss than a click, so on a phone Delete asks first, the
  // way the context menu's item does. Any change of selection re-arms it.
  let confirming = $state(false)
  $effect(() => {
    void browse.selected
    confirming = false
  })

  const selected = $derived(browse.entries.filter((e) => browse.isSelected(e.path)))

  async function guard(fn: () => Promise<unknown>) {
    try {
      await fn()
    } catch (e) {
      onstatus(e instanceof Error ? e.message : String(e))
    }
  }

  async function doDownload() {
    const files = selected.filter((s) => s.kind !== 'dir')
    if (files.length === 0) {
      onstatus('Folders cannot be downloaded. Use SMB for bulk transfers.')
      return
    }
    await act.download(files.map((f) => f.path))
    if (files.length > 1) onstatus(`Downloading ${files.length} files.`)
  }
</script>

<!-- The floating dark pill, CasaOS's selection action bar. On a phone it is a
     full-width bar on the bottom edge instead, icons over labels, where the five
     text buttons would not fit across the screen. -->
{#if selected.length > 0 && media.mobile}
  <div class="phone-bar">
    <button class="count" onclick={() => browse.selectAll(false)} aria-label="Clear selection">
      <Icon name="close" />
      <span>{selected.length}</span>
    </button>
    <button onclick={doDownload}><Icon name="download" /><span>Download</span></button>
    <button onclick={() => clipboard.copy(selected)}><Icon name="copy" /><span>Copy</span></button>
    <button onclick={() => clipboard.cut(selected)}><Icon name="cut" /><span>Cut</span></button>
    {#if !confirming}
      <button class="danger" onclick={() => (confirming = true)}>
        <Icon name="trash" /><span>Delete</span>
      </button>
    {:else}
      <button
        class="danger armed"
        onclick={() => guard(() => act.remove(selected.map((s) => s.path)))}
      >
        <Icon name="trash" /><span>Sure?</span>
      </button>
    {/if}
    <button onclick={onmore}><Icon name="more" /><span>More</span></button>
  </div>
{:else if selected.length > 0}
  <div class="operation-toolbar">
    <button onclick={doDownload}>Download</button>
    <button onclick={() => clipboard.copy(selected)}>Copy</button>
    <button onclick={() => clipboard.cut(selected)}>Cut</button>
    <button class="danger" onclick={() => guard(() => act.remove(selected.map((s) => s.path)))}>
      Delete
    </button>
    <button onclick={() => browse.selectAll(false)}>Cancel</button>
  </div>
{/if}

<style>
  .operation-toolbar {
    position: absolute;
    bottom: 50px;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    background: var(--toolbar-bg);
    border-radius: var(--radius-card);
    padding: 0.5rem 1rem;
    z-index: 100;
  }
  button {
    border: 0;
    background: transparent;
    color: #fff;
    font-size: var(--font-base);
    padding: 0.25rem 0.5rem;
    border-radius: var(--radius-small);
    margin-left: 0.5rem;
    transition: background 0.3s;
  }
  button:first-child {
    margin-left: 0;
  }
  @media (hover: hover) {
    .operation-toolbar button:hover {
      background: var(--toolbar-hover);
    }
    .operation-toolbar button.danger:hover {
      background: var(--red);
    }
  }
  .phone-bar {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 100;
    display: flex;
    background: var(--toolbar-bg);
    padding: 0.25rem 0.25rem calc(0.25rem + env(safe-area-inset-bottom));
  }
  .phone-bar button {
    flex: 1 1 0;
    min-width: 0;
    min-height: 52px;
    margin: 0;
    padding: 0.25rem 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    font-size: 11px;
  }
  .phone-bar button:active {
    background: var(--toolbar-hover);
  }
  .phone-bar .count {
    flex-direction: row;
    gap: 0.25rem;
    font-size: var(--font-base);
    font-weight: 600;
  }
  .phone-bar .armed {
    background: var(--red);
  }
</style>
