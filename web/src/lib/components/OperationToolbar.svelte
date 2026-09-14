<script lang="ts">
  import { browse } from '../browse.svelte'
  import { clipboard } from '../clipboard.svelte'
  import * as act from '../actions'

  interface Props {
    onstatus: (msg: string) => void
  }
  let { onstatus }: Props = $props()

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

<!-- The floating dark pill, CasaOS's selection action bar. -->
{#if selected.length > 0}
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
  button:hover {
    background: var(--toolbar-hover);
  }
  button.danger:hover {
    background: var(--red);
  }
</style>
