<script lang="ts">
  import { illustration } from '../icons'

  interface Props {
    onaction: (kind: 'newFile' | 'newFolder' | 'uploadFiles' | 'uploadFolder') => void
  }
  let { onaction }: Props = $props()

  const tiles = [
    { label: 'New File', art: 'newFile' as const, kind: 'newFile' as const },
    { label: 'New Folder', art: 'newFolder' as const, kind: 'newFolder' as const },
    { label: 'Upload Files', art: 'upfile' as const, kind: 'uploadFiles' as const },
    { label: 'Upload Folder', art: 'upfolder' as const, kind: 'uploadFolder' as const },
  ]
</script>

<!-- CasaOS's empty holder: the label sits ABOVE the art, not below it. -->
<div class="empty">
  <h5>Drop your files here to upload</h5>
  <p>or</p>
  <ul>
    {#each tiles as tile (tile.label)}
      <li>
        <button onclick={() => onaction(tile.kind)}>
          <span>{tile.label}</span><img src={illustration(tile.art)} alt="" />
        </button>
      </li>
    {/each}
  </ul>
</div>

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    text-align: center;
  }
  h5 {
    font-weight: 600;
    font-size: 1.25rem;
    margin: 0 0 0.5rem;
  }
  p {
    font-size: var(--font-meta);
    color: var(--text-muted);
    margin: 0 0 0.5rem;
  }
  ul {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    list-style: none;
    padding: 0;
    margin: 0;
  }
  li {
    width: 124px;
    height: 124px;
    margin: 0.5rem;
    background: var(--sidebar-bg);
    border-radius: var(--radius-card);
    font-size: 14px;
    transition: all 0.3s ease;
  }
  li:hover {
    background: rgb(235, 235, 235);
  }
  /* A flex column, not a fixed padding-top plus an absolutely positioned image.
     The old version pinned the art at top:60px and let the label find its own
     position inside the button, so "Upload Folder" — the longest label, and the
     only one that does not fit 106px on one line — wrapped down onto the art.
     Laying both out in flow means the label can be any length without ever
     reaching the illustration. */
  li button {
    all: unset;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: flex-start;
    gap: 0.375rem;
    width: 100%;
    height: 100%;
    padding: 14px 6px 10px;
    cursor: pointer;
  }
  li span {
    /* Two lines' worth, reserved on every tile so the art sits at the same
       height across the row whether the label wrapped or not. */
    display: flex;
    align-items: center;
    justify-content: center;
    height: 2.6em;
    line-height: 1.3;
    text-align: center;
  }
  li img {
    width: 52px;
    height: 52px;
  }
</style>
