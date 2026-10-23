<script lang="ts">
  import { browse } from '../browse.svelte'
  import { clipboard } from '../clipboard.svelte'
  import type { Entry } from '../types'

  interface Props {
    onmenu: (e: MouseEvent, entry: Entry | null) => void
    onopen: (entry: Entry) => void
  }
  let { onmenu, onopen }: Props = $props()

  // Paths whose preview failed to decode, so the tile falls back to a kind icon.
  let broken = $state<Set<string>>(new Set())
  function markBroken(path: string) {
    broken = new Set(broken).add(path)
  }
  import { iconFor } from '../icons'
  import { thumbUrl } from '../api'
  import { date } from '../format'
  import { isTouch, media } from '../media.svelte'
  import { longpress } from '../longpress'
  import Icon from './Icon.svelte'
</script>

<!-- Responsive columns rather than fixed tiles: CasaOS computes
     cols = floor(containerWidth / 144) and gives each card that percentage.
     CSS grid with auto-fill and a 144px minimum is the same rule declaratively. -->
<div class="card-container">
  {#each browse.entries as entry (entry.path)}
    <div class="grid-card">
      <!-- .tile holds the card AND its ⋮ button side by side: the menu button
           cannot live inside the card, which is itself a <button>. -->
      <div class="tile">
        <button
          class="node-card"
          class:active={browse.isSelected(entry.path)}
          class:cutting={clipboard.isCutting(entry.path)}
          oncontextmenu={(e) => onmenu(e, entry)}
          ondblclick={(e) => !isTouch(e) && onopen(entry)}
          onclick={(e) => browse.tap(e, entry.path, () => onopen(entry))}
          use:longpress={() => browse.toggle(entry.path)}
        >
          <div class="cover">
            {#if entry.thumb && !broken.has(entry.path)}
              <!-- A file can carry an image extension and not be a decodable image
                   (truncated, corrupt, or simply misnamed). Without this fallback
                   the tile renders empty, which reads as a bug rather than as a
                   file the browser could not decode. -->
              <!-- The thumbnail endpoint, not the raw file: a 12 MP photo would
                   otherwise be downloaded in full to be drawn at 120px, and a
                   folder of 400 of them would pull gigabytes. loading="lazy" is
                   what keeps scrolling past a big folder from requesting every
                   thumbnail in it. -->
              <img
                class="thumb"
                src={thumbUrl(entry.path, 256)}
                alt=""
                loading="lazy"
                decoding="async"
                onerror={() => markBroken(entry.path)}
              />
            {:else}
              <img class:folder={entry.kind === 'dir'} src={iconFor(entry)} alt="" />
            {/if}
          </div>
          <div class="info">
            <div class="title">{entry.name}</div>
            <div class="desc one-line">{date(entry.modTime)}</div>
          </div>
        </button>
        {#if media.touch}
          {#if browse.selected.size > 0}
            <!-- Selection mode: a tick says which tiles a tap has collected. -->
            <span class="tick" class:on={browse.isSelected(entry.path)} aria-hidden="true">
              {#if browse.isSelected(entry.path)}<Icon name="check" size={14} />{/if}
            </span>
          {:else}
            <button class="more" aria-label="Actions for {entry.name}" onclick={(e) => onmenu(e, entry)}>
              <Icon name="moreVertical" />
            </button>
          {/if}
        {/if}
      </div>
    </div>
  {/each}
</div>

<style>
  .card-container {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--grid-pitch), 1fr));
    margin: 0 0.75rem;
  }
  .grid-card {
    height: var(--grid-row-height);
    padding: 0 0.75rem;
    display: flex;
    justify-content: center;
  }
  .tile {
    position: relative;
    display: flex;
  }
  .node-card {
    width: var(--grid-tile-width);
    border: 0;
    background: transparent;
    border-radius: var(--radius-card);
    padding: 8px 0 10px;
    transition: all 0.25s;
  }
  @media (hover: hover) {
    .node-card:hover {
      background: var(--sidebar-bg);
    }
  }
  .node-card.active {
    background: var(--primary-light);
  }
  /* Staged for a move. CasaOS dims the tile rather than removing it, so the
     source stays visible until the paste actually happens. */
  .node-card.cutting {
    opacity: 0.5;
  }
  .cover {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 96px;
    margin-bottom: 0.75rem;
    pointer-events: none;
  }
  .cover img {
    width: 80px;
    height: 80px;
  }
  .cover img.folder {
    width: 96px;
    height: 96px;
  }
  .cover img.thumb {
    width: auto;
    height: auto;
    max-width: 100px;
    max-height: 96px;
    border-radius: var(--radius-small);
    object-fit: contain;
  }
  .title {
    font-size: var(--font-base);
    line-height: 1.5em;
    text-align: center;
    padding: 0 0.5rem;
    margin-bottom: 2px;
    /* Two-line clamp, as CasaOS. */
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    overflow-wrap: break-word;
  }
  /* Touch-only overlays, top-right of the tile. */
  .more,
  .tick {
    position: absolute;
    top: 2px;
    right: -6px;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .more {
    width: 36px;
    height: 36px;
    border: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: 50%;
  }
  .tick {
    top: 8px;
    right: 4px;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    border: 2px solid var(--border-strong);
    background: var(--surface);
    pointer-events: none;
  }
  .tick.on {
    border-color: var(--primary);
    background: var(--primary);
    color: var(--text-on-accent);
  }
  @media (max-width: 768px) {
    .card-container {
      margin: 0 0.25rem;
    }
    .grid-card {
      padding: 0 0.25rem;
    }
    .cover {
      height: 72px;
      margin-bottom: 0.5rem;
    }
    .cover img {
      width: 60px;
      height: 60px;
    }
    .cover img.folder {
      width: 72px;
      height: 72px;
    }
    .cover img.thumb {
      max-width: 84px;
      max-height: 72px;
    }
    .title {
      font-size: var(--font-meta);
      padding: 0 0.25rem;
    }
  }
  .desc {
    font-size: var(--font-meta);
    line-height: 1.5em;
    color: var(--meta-text);
    text-align: center;
  }
</style>
