<script lang="ts">
  import { uploads, filesFromDrop } from '../uploads.svelte'
  import { browse } from '../browse.svelte'
  import type { Snippet } from 'svelte'

  interface Props {
    children: Snippet
    onstatus: (msg: string) => void
  }
  let { children, onstatus }: Props = $props()

  // Nested elements fire dragleave as the pointer crosses them, so a boolean
  // would flicker. Counting enter/leave pairs is what keeps the mask stable.
  let depth = $state(0)
  const dragging = $derived(depth > 0)

  function hasFiles(e: DragEvent) {
    return [...(e.dataTransfer?.types ?? [])].includes('Files')
  }

  async function onDrop(e: DragEvent) {
    e.preventDefault()
    depth = 0
    if (!e.dataTransfer) return
    try {
      const files = await filesFromDrop(e.dataTransfer)
      if (files.length === 0) return
      uploads.add(files, browse.path)
    } catch (err) {
      onstatus(err instanceof Error ? err.message : String(err))
    }
  }

  const here = $derived(browse.path === '/' ? 'Root' : browse.path.slice(browse.path.lastIndexOf('/') + 1))
</script>

<div
  class="drop-target"
  role="presentation"
  ondragenter={(e) => hasFiles(e) && depth++}
  ondragleave={() => depth > 0 && depth--}
  ondragover={(e) => hasFiles(e) && e.preventDefault()}
  ondrop={onDrop}
>
  {@render children()}

  {#if dragging}
    <div class="drag-mask">
      <div class="badge">↑</div>
      <p>Upload to <strong>{here}</strong></p>
    </div>
  {/if}
</div>

<style>
  .drop-target {
    position: relative;
    width: 100%;
    height: 100%;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .drag-mask {
    position: absolute;
    inset: 0;
    z-index: 3000;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    font-size: 1.25rem;
    color: var(--primary);
    background: linear-gradient(rgba(255, 255, 255, 0.8), rgba(255, 255, 255, 1));
  }
  .badge {
    width: 72px;
    height: 72px;
    border-radius: 72px;
    background: var(--primary);
    color: #fff;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 2rem;
    margin: 0 auto 2rem;
  }
  p {
    margin: 0;
  }
</style>
