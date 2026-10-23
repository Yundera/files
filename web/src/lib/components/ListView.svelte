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
  import { isTouch, media } from '../media.svelte'
  import { longpress } from '../longpress'
  import Icon from './Icon.svelte'

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
      {#if media.touch}<div class="td more"></div>{/if}
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
        ondblclick={(e) => !isTouch(e) && onopen(entry)}
        onclick={(e) => browse.tap(e, entry.path, () => onopen(entry))}
        onkeydown={(e) => e.key === 'Enter' && onopen(entry)}
        use:longpress={() => browse.toggle(entry.path)}
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
          <div class="label">
            <span class="one-line">{entry.name}</span>
            <!-- The phone form of the Type/Date/Size columns, which do not fit. -->
            <span class="meta one-line">
              {entry.kind === 'dir' ? date(entry.modTime) : `${size(entry.size)} · ${date(entry.modTime)}`}
            </span>
          </div>
        </div>
        <div class="td type one-line">{entry.kind === 'dir' ? '' : (entry.ext ?? '')}</div>
        <div class="td date one-line">{date(entry.modTime)}</div>
        <div class="td size one-line">{entry.kind === 'dir' ? '' : size(entry.size)}</div>
        {#if media.touch}
          <div class="td more">
            <button
              aria-label="Actions for {entry.name}"
              onclick={(e) => {
                e.stopPropagation()
                onmenu(e, entry)
              }}
            >
              <Icon name="moreVertical" />
            </button>
          </div>
        {/if}
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
  @media (hover: hover) {
    .tbody .tr:hover {
      background: var(--sidebar-bg);
    }
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
  .label {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .meta {
    display: none;
    font-size: var(--font-meta);
    color: var(--meta-text);
  }
  .more {
    width: 2.75rem;
    flex-shrink: 0;
    justify-content: flex-end;
  }
  .more button {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    border: 0;
    background: transparent;
    color: var(--text-muted);
  }
  /* Phone: name over "size · date", the three columns and the header gone.
     Sorting moves to the header's ⋯ sheet, since the header row held it. */
  @media (max-width: 768px) {
    /* .td.x, not .x: `.tbody .td { display: flex }` would outrank a lone class. */
    .thead,
    .td.type,
    .td.date,
    .td.size {
      display: none;
    }
    .tr-wrapper {
      padding: 0 0.25rem 0 1rem;
    }
    .meta {
      display: block;
    }
    .cover {
      margin-right: 0.75rem;
    }
  }
</style>
