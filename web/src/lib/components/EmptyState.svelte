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
    position: relative;
    width: 106px;
    height: 120px;
    margin: 0.5rem;
    padding-top: 20px;
    background: var(--sidebar-bg);
    border-radius: var(--radius-card);
    font-size: 14px;
    line-height: 1.5;
    transition: all 0.3s ease;
  }
  li:hover {
    background: rgb(235, 235, 235);
  }
  li button {
    all: unset;
    display: block;
    width: 100%;
    height: 100%;
    cursor: pointer;
  }
  li span {
    display: block;
    padding: 0 0.5rem;
  }
  li img {
    position: absolute;
    top: 60px;
    left: 25px;
    width: 56px;
    height: 56px;
  }
</style>
