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
</script>

<!-- Responsive columns rather than fixed tiles: CasaOS computes
     cols = floor(containerWidth / 144) and gives each card that percentage.
     CSS grid with auto-fill and a 144px minimum is the same rule declaratively. -->
<div class="card-container">
  {#each browse.entries as entry (entry.path)}
    <div class="grid-card">
      <button
        class="node-card"
        class:active={browse.isSelected(entry.path)}
        class:cutting={clipboard.isCutting(entry.path)}
        oncontextmenu={(e) => onmenu(e, entry)}
        ondblclick={() => onopen(entry)}
        onclick={(e) => (e.ctrlKey || e.metaKey ? browse.toggle(entry.path) : browse.selectOnly(entry.path))}
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
  .node-card {
    width: var(--grid-tile-width);
    border: 0;
    background: transparent;
    border-radius: var(--radius-card);
    padding: 8px 0 10px;
    transition: all 0.25s;
  }
  .node-card:hover {
    background: var(--sidebar-bg);
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
  .desc {
    font-size: var(--font-meta);
    line-height: 1.5em;
    color: var(--meta-text);
    text-align: center;
  }
</style>
