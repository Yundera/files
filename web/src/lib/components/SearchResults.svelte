<script lang="ts">
  import { search } from '../search.svelte'
  import { browse } from '../browse.svelte'
  import { iconFor } from '../icons'
  import { date, size } from '../format'
  import type { Entry } from '../types'

  interface Props {
    onopen: (entry: Entry) => void
  }
  let { onopen }: Props = $props()

  /** The folder a hit lives in, shown so a result is locatable — the point of
   *  searching a subtree is usually to find where something went. */
  function parent(p: string) {
    const i = p.lastIndexOf('/')
    return i <= 0 ? '/' : p.slice(0, i)
  }
</script>

<div class="results">
  <div class="summary">
    {#if search.loading}
      Searching {search.root}…
    {:else}
      {search.results.length} result{search.results.length === 1 ? '' : 's'} in {search.root}
      <span class="meta">· {search.elapsedMs} ms</span>
      {#if search.truncated}
        <span class="warn">· stopped early, showing the first {search.results.length}</span>
      {/if}
    {/if}
  </div>

  {#if search.error}
    <p class="error">{search.error}</p>
  {:else if !search.loading && search.results.length === 0}
    <p class="empty">Nothing matched “{search.query}”.</p>
  {:else}
    {#each search.results as entry (entry.path)}
      <!-- A div, not a button: the row contains its own "go to folder" button
           and nesting buttons is invalid HTML the browser silently restructures. -->
      <div
        class="row"
        role="button"
        tabindex="0"
        ondblclick={() => onopen(entry)}
        onclick={() => browse.selectOnly(entry.path)}
        onkeydown={(e) => e.key === 'Enter' && onopen(entry)}
      >
        <img class="cover" src={iconFor(entry)} alt="" />
        <span class="name one-line">{entry.name}</span>
        <button
          class="where one-line"
          title="Go to {parent(entry.path)}"
          onclick={(e) => {
            e.stopPropagation()
            search.clear()
            browse.load(parent(entry.path))
          }}>{parent(entry.path)}</button
        >
        <span class="when">{date(entry.modTime)}</span>
        <span class="size">{entry.kind === 'dir' ? '' : size(entry.size)}</span>
      </div>
    {/each}
  {/if}
</div>

<style>
  .results {
    padding: 0 1.5rem 1.5rem;
  }
  .summary {
    padding: 0.75rem 0;
    font-size: var(--font-base);
  }
  .meta {
    color: var(--text-muted);
  }
  .warn {
    color: var(--orange);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 1rem;
    width: 100%;
    height: var(--list-row-height);
    border-top: var(--sidebar-bg) 1px solid;
    background: transparent;
    text-align: left;
    font-size: var(--font-base);
    color: inherit;
    cursor: pointer;
  }
  .row:hover {
    background: var(--sidebar-bg);
  }
  .cover {
    width: 2rem;
    height: 2rem;
    flex-shrink: 0;
  }
  .name {
    flex: 1 1 auto;
    min-width: 6rem;
  }
  .where {
    flex: 1 1 auto;
    min-width: 6rem;
    border: 0;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--font-meta);
    text-align: left;
    padding: 0;
  }
  .where:hover {
    color: var(--primary);
    text-decoration: underline;
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
  .error {
    color: var(--text-muted);
    font-size: var(--font-base);
  }
  .error {
    color: var(--red);
  }
</style>
