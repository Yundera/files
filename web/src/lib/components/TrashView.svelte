<script lang="ts">
  import { api } from '../api'
  import { trashChanged } from '../live.svelte'
  import { browse } from '../browse.svelte'
  import { date, size } from '../format'

  interface Item {
    id: string
    originalPath: string
    name: string
    isDir: boolean
    size: number
    deletedAt: string
  }

  interface Props {
    onstatus: (msg: string) => void
  }
  let { onstatus }: Props = $props()

  let items = $state<Item[]>([])
  let total = $state(0)
  let selected = $state<Set<string>>(new Set())
  let confirmingEmpty = $state(false)

  async function load() {
    try {
      const res = await api.get<{ items: Item[]; size: number }>('/api/trash')
      items = res.items
      total = res.size
    } catch (e) {
      onstatus(e instanceof Error ? e.message : String(e))
    }
  }

  $effect(() => {
    load()
    return trashChanged.subscribe(load)
  })

  async function guard(fn: () => Promise<unknown>) {
    try {
      await fn()
      selected = new Set()
      await load()
    } catch (e) {
      onstatus(e instanceof Error ? e.message : String(e))
    }
  }

  const ids = $derived([...selected])

  function toggle(id: string) {
    const next = new Set(selected)
    next.has(id) ? next.delete(id) : next.add(id)
    selected = next
  }

  async function restore() {
    await guard(async () => {
      // keepBoth rather than overwrite: something else may be at the original
      // path now, and a restore must never destroy it.
      await api.post('/api/trash/restore', { ids, conflict: 'keepBoth' })
      await browse.load()
    })
  }
</script>

<div class="trash">
  <div class="bar">
    <div>
      <strong>Trash</strong>
      <span class="meta">
        {items.length} item{items.length === 1 ? '' : 's'} · {size(total)}
      </span>
    </div>
    <div class="actions">
      {#if ids.length > 0}
        <button class="ghost" onclick={restore}>Restore</button>
        <button class="ghost danger" onclick={() => guard(() => api.post('/api/trash/delete', { ids }))}>
          Delete permanently
        </button>
      {:else if items.length > 0}
        {#if !confirmingEmpty}
          <button class="ghost danger" onclick={() => (confirmingEmpty = true)}>Empty trash</button>
        {:else}
          <button
            class="ghost danger"
            onclick={() => {
              confirmingEmpty = false
              guard(() => api.post('/api/trash/empty'))
            }}>Are you sure?</button
          >
        {/if}
      {/if}
    </div>
  </div>

  {#if items.length === 0}
    <p class="empty">The trash is empty.</p>
  {:else}
    <div class="rows">
      {#each items as item (item.id)}
        <div class="row" class:active={selected.has(item.id)}>
          <input type="checkbox" checked={selected.has(item.id)} onchange={() => toggle(item.id)} />
          <span class="name one-line">{item.name}</span>
          <span class="from one-line" title={item.originalPath}>{item.originalPath}</span>
          <span class="when">{date(item.deletedAt)}</span>
          <span class="size">{item.isDir ? '' : size(item.size)}</span>
        </div>
      {/each}
    </div>
    <p class="note">
      Trashed items still use disk. They are purged automatically after the retention period.
    </p>
  {/if}
</div>

<style>
  .trash {
    padding: 0 1.5rem 1.5rem;
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 0;
  }
  .meta {
    color: var(--text-muted);
    margin-left: 0.5rem;
    font-size: var(--font-meta);
  }
  .actions {
    display: flex;
    gap: 0.5rem;
  }
  .ghost {
    border: 0;
    background: var(--surface-2);
    border-radius: 9999px;
    padding: 0.25rem 0.75rem;
    font-size: var(--font-base);
    color: inherit;
  }
  .ghost.danger {
    color: var(--red);
  }
  .ghost:hover {
    background: var(--surface-3);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 1rem;
    height: var(--list-row-height);
    border-top: var(--sidebar-bg) 1px solid;
  }
  .row.active {
    background: var(--primary-light);
  }
  .name {
    flex: 1 1 auto;
    min-width: 6rem;
  }
  .from {
    flex: 2 1 auto;
    color: var(--text-muted);
    font-size: var(--font-meta);
    min-width: 6rem;
  }
  .when {
    width: 9rem;
    flex-shrink: 0;
    font-size: var(--font-meta);
    color: var(--text-muted);
  }
  .size {
    width: 4rem;
    flex-shrink: 0;
    font-size: var(--font-meta);
  }
  .empty,
  .note {
    color: var(--text-muted);
    font-size: var(--font-meta);
  }
</style>
