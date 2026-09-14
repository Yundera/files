<script lang="ts">
  import { live } from '../live.svelte'
  import * as act from '../actions'
  import { size } from '../format'

  let open = $state(false)
  const active = $derived(live.active)
  const jobs = $derived(live.visible)

  const verb: Record<string, string> = { copy: 'Copying', move: 'Moving', delete: 'Deleting' }

  function pct(j: { done: number; total: number; items: number; totalItems: number }) {
    // Bytes where we have them, items otherwise: a move is a rename, so it has
    // no byte progress to report and only the item count is meaningful.
    if (j.total > 0) return Math.floor((j.done / j.total) * 100)
    if (j.totalItems > 0) return Math.floor((j.items / j.totalItems) * 100)
    return 0
  }
</script>

{#if jobs.length > 0}
  <div class="tray">
    <button class="trigger" onclick={() => (open = !open)}>
      {#if active.length > 0}
        <span class="spinner"></span> {active.length} task{active.length > 1 ? 's' : ''}
      {:else}
        Tasks
      {/if}
    </button>

    {#if open}
      <div class="popper">
        <div class="head">Current Tasks</div>
        {#each jobs as job (job.id)}
          <div class="item">
            <div class="fill" style="width:{pct(job)}%"></div>
            <div class="info">
              <div class="row">
                <span class="one-line">{verb[job.kind] ?? job.kind}: {job.message}</span>
                {#if job.phase === 'running'}
                  <button class="cancel" onclick={() => act.cancelJob(job.id)}>Cancel</button>
                {/if}
              </div>
              <div class="meta">
                {#if job.phase === 'error'}
                  <span class="err">{job.error}</span>
                {:else if job.phase === 'cancelled'}
                  Cancelled
                {:else if job.total > 0}
                  {size(job.done)} / {size(job.total)}
                {:else}
                  {job.items} / {job.totalItems}
                {/if}
              </div>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .tray {
    position: relative;
    z-index: 100;
  }
  .trigger {
    border: 0;
    background: var(--primary-light);
    color: var(--primary);
    border-radius: 9999px;
    padding: 0.25rem 0.75rem;
    font-size: var(--font-base);
    display: flex;
    align-items: center;
    gap: 0.375rem;
  }
  .spinner {
    width: 10px;
    height: 10px;
    border: 2px solid var(--primary);
    border-top-color: transparent;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .popper {
    position: absolute;
    right: 0;
    top: calc(100% + 5px);
    width: 20rem;
    background: var(--surface);
    border-radius: var(--radius-thumb);
    padding: 0 0 0.5rem;
    text-align: initial;
    box-shadow:
      0 0.5em 1em -0.125em rgb(10 10 10 / 10%),
      0 0 0 1px rgb(10 10 10 / 2%);
  }
  .head {
    padding: 0.75rem 1rem;
    font-weight: 600;
  }
  .item {
    position: relative;
    width: 100%;
    padding: 0.5rem 1rem;
  }
  /* Progress is a full-height background fill, not a bar — CasaOS's upload row. */
  .fill {
    position: absolute;
    left: 0;
    top: 0;
    height: 100%;
    background: var(--primary-light);
    transition: width 0.3s ease;
    z-index: 0;
  }
  .info {
    position: relative;
    z-index: 2;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    font-size: var(--font-base);
  }
  .meta {
    font-size: var(--font-meta);
    color: var(--text-muted);
  }
  .err {
    color: var(--red);
  }
  .cancel {
    border: 0;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--font-meta);
    flex-shrink: 0;
  }
  .cancel:hover {
    color: var(--red);
  }
</style>
