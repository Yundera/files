<script lang="ts">
  import { browse } from '../browse.svelte'
  import { clipboard } from '../clipboard.svelte'
  import type { Entry } from '../types'

  interface Props {
    onmenu: (e: MouseEvent, entry: Entry | null) => void
    onopen: (entry: Entry) => void
  }
  let { onmenu, onopen }: Props = $props()
  import { iconFor } from '../icons'
  import { date, size } from '../format'
  import type { SortKey } from '../types'

  const columns: Array<{ label: string; key: SortKey; cls: string }> = [
    { label: 'File name', key: 'name', cls: 'name' },
    { label: 'Type', key: 'kind', cls: 'type' },
    { label: 'Date Modified', key: 'modified', cls: 'date' },
    { label: 'Size', key: 'size', cls: 'size' },
  ]
</script>

<div class="table">
  <div class="thead">
    <div class="tr-wrapper">
      <div class="td check"></div>
      {#each columns as col (col.key)}
        <div class="td {col.cls}">
          <button onclick={() => browse.setSort(col.key)}>
            {col.label}
            {#if browse.sort === col.key}<span class="caret" class:asc={!browse.desc}>▾</span>{/if}
          </button>
        </div>
      {/each}
    </div>
  </div>
  <div class="tbody">
    {#each browse.entries as entry (entry.path)}
      <div
        class="tr-wrapper tr"
        class:active={browse.isSelected(entry.path)}
        class:cutting={clipboard.isCutting(entry.path)}
        oncontextmenu={(e) => onmenu(e, entry)}
        role="row"
        tabindex="0"
        ondblclick={() => onopen(entry)}
        onclick={(e) => (e.ctrlKey || e.metaKey ? browse.toggle(entry.path) : browse.selectOnly(entry.path))}
        onkeydown={(e) => e.key === 'Enter' && onopen(entry)}
      >
        <div class="td check">
          <input
            type="checkbox"
            checked={browse.isSelected(entry.path)}
            onclick={(e) => {
              e.stopPropagation()
              browse.toggle(entry.path)
            }}
          />
        </div>
        <div class="td name">
          <img class="cover" src={iconFor(entry)} alt="" />
          <span class="one-line">{entry.name}</span>
        </div>
        <div class="td type one-line">{entry.kind === 'dir' ? '' : (entry.ext ?? '')}</div>
        <div class="td date one-line">{date(entry.modTime)}</div>
        <div class="td size one-line">{entry.kind === 'dir' ? '' : size(entry.size)}</div>
      </div>
    {/each}
  </div>
</div>

<style>
  .table {
    width: 100%;
  }
  .tr-wrapper {
    display: flex;
    align-items: center;
    padding: 0 1.5rem;
  }
  .thead {
    position: sticky;
    top: 0;
    z-index: 20;
    background: var(--surface);
  }
  .thead .td {
    height: var(--list-header-height);
    font-size: var(--font-base);
    display: flex;
    align-items: center;
  }
  .thead button {
    border: 0;
    background: transparent;
    font-size: var(--font-base);
    color: inherit;
    padding: 0;
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }
  .caret {
    display: inline-block;
    transition: transform 0.2s;
  }
  .caret.asc {
    transform: rotate(180deg);
  }
  .tbody .tr {
    border-top: var(--sidebar-bg) 1px solid;
    border-bottom: var(--sidebar-bg) 1px solid;
    transition: all 0.25s;
    cursor: pointer;
  }
  .tbody .tr:hover {
    background: var(--sidebar-bg);
  }
  .tbody .tr.active {
    background: var(--primary-light);
  }
  .tbody .tr.cutting {
    opacity: 0.5;
  }
  .tbody .td {
    height: var(--list-row-height);
    display: flex;
    align-items: center;
  }
  /* Column widths, measured from CasaOS's _filebrowser.scss. */
  .check {
    width: 2rem;
    flex-shrink: 0;
  }
  .name {
    flex-grow: 1;
    min-width: 80px;
    overflow: hidden;
  }
  .type {
    width: 3rem;
    flex-shrink: 0;
  }
  .date {
    width: 9rem;
    flex-shrink: 0;
  }
  .size {
    width: 4rem;
    flex-shrink: 0;
  }
  .cover {
    width: 2rem;
    height: 2rem;
    margin-right: 1rem;
    flex-shrink: 0;
  }
</style>
