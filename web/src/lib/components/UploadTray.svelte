<script lang="ts">
  import { uploads } from '../uploads.svelte'
  import { size } from '../format'

  let collapsed = $state(false)
  let dismissed = $state(false)

  const items = $derived(uploads.items)
  const active = $derived(uploads.active)

  $effect(() => {
    // A new upload re-opens a tray the user dismissed, otherwise the second
    // batch of the session would upload invisibly.
    if (items.length > 0) dismissed = false
  })

  function pct(i: { uploaded: number; size: number }) {
    return i.size > 0 ? Math.floor((i.uploaded / i.size) * 100) : 0
  }
</script>

{#if items.length > 0 && !dismissed}
  <div class="upload-list">
    <div class="card-header">
      <button class="collapse-trigger" onclick={() => (collapsed = !collapsed)}>
        {collapsed ? '▸' : '▾'}
        {active.length > 0 ? `Uploading (${active.length})` : 'Uploads'}
      </button>
      <button
        class="dismiss"
        title="Close"
        onclick={() => {
          uploads.clearFinished()
          dismissed = true
        }}>✕</button
      >
    </div>

    {#if !collapsed}
      <div class="card-content scrollbars-light">
        {#each items as item (item.id)}
          <div class="task">
            <!-- Progress is a full-height background fill, not a bar — CasaOS's
                 uploader row. Hidden once the upload succeeds. -->
            {#if item.status !== 'done'}
              <div class="task-progress" style="width:{pct(item)}%"></div>
            {/if}
            <div class="task-info">
              <div class="task-file-name one-line" title={item.name}>{item.name}</div>
              <div class="task-desc-wrapper">
                <span class="task-desc">
                  {item.status === 'done' ? size(item.size) : `${size(item.uploaded)}/${size(item.size)}`}
                </span>
                <span class="task-dot"></span>
                <span class="task-desc" class:err={item.status === 'error'}>
                  {item.status === 'error' ? (item.error ?? 'Failed') : item.status}
                </span>
              </div>
            </div>
            <div class="action">
              {#if item.status === 'uploading'}
                <button title="Pause" onclick={() => uploads.pause(item.id)}>❚❚</button>
              {:else if item.status === 'paused'}
                <button title="Resume" onclick={() => uploads.resume(item.id)}>▶</button>
              {:else if item.status === 'error'}
                <button title="Retry" onclick={() => uploads.resume(item.id)}>↻</button>
              {/if}
              {#if item.status !== 'done'}
                <button title="Cancel" onclick={() => uploads.cancel(item.id)}>✕</button>
              {:else}
                <button title="Remove" onclick={() => uploads.remove(item.id)}>✕</button>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .upload-list {
    position: absolute;
    z-index: 30;
    width: 375px;
    right: 32px;
    bottom: 28px;
    background: var(--surface);
    border-radius: var(--radius-card);
    box-shadow:
      0 0.5em 1em -0.125em rgb(10 10 10 / 15%),
      0 0 0 1px rgb(10 10 10 / 2%);
    overflow: hidden;
  }
  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    transition: background 0.3s;
  }
  .card-header:hover {
    background: var(--sidebar-bg);
  }
  .collapse-trigger,
  .dismiss {
    border: 0;
    background: transparent;
    font-size: var(--font-base);
    color: inherit;
  }
  .collapse-trigger {
    flex: 1 1 auto;
    text-align: left;
    padding: 0.75rem 1rem;
    font-weight: 600;
  }
  .dismiss {
    padding: 0.75rem 1rem;
  }
  .card-content {
    max-height: 464px;
    overflow-y: auto;
    font-size: var(--font-base);
  }
  .task {
    position: relative;
    display: flex;
    align-items: center;
    width: 100%;
    height: 64px;
    padding: 14px 18px 14px 16px;
  }
  .task-progress {
    position: absolute;
    left: 0;
    top: 0;
    height: 64px;
    min-width: 4px;
    background: var(--primary-light);
    transition: width 0.3s ease;
    z-index: 0;
  }
  .task-progress::after {
    content: '';
    position: absolute;
    bottom: 4px;
    left: 0;
    width: 100%;
    height: 2px;
    background: var(--primary);
  }
  .task-info {
    position: relative;
    z-index: 2;
    flex: 1 1 auto;
    min-width: 0;
  }
  .task-file-name {
    font-size: 14px;
    line-height: 1.5;
    max-width: 240px;
  }
  .task-desc-wrapper {
    display: flex;
    align-items: center;
  }
  .task-desc {
    font-size: 12px;
    line-height: 1.6;
    color: var(--grey-600);
  }
  .task-desc.err {
    color: var(--red);
  }
  .task-dot::before {
    content: '';
    display: block;
    width: 2px;
    height: 2px;
    background: var(--grey-600);
    margin: 0 7px;
  }
  .action {
    position: relative;
    z-index: 2;
    display: flex;
    gap: 0.25rem;
    flex-shrink: 0;
  }
  .action button {
    width: 24px;
    height: 24px;
    border: 0;
    border-radius: 50%;
    background: transparent;
    color: var(--grey-600);
    font-size: 12px;
  }
  .action button:hover {
    background: var(--grey-400);
  }
</style>
