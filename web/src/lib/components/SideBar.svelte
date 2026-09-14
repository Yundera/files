<script lang="ts">
  import { browse, roots, TRASH_PATH } from '../browse.svelte'
  import { rootIcon } from '../icons'
</script>

<!-- 15rem fixed, #f8f8f8. The Files tree in CasaOS is FLAT — no nesting, no
     chevrons — so this is a plain list, not a tree widget. -->
<nav class="nav-bar scrollbars-light">
  <section>
    <h3>Files</h3>
    <ul class="list-container">
      {#each roots as root (root.path)}
        <li>
          <button
            class="list-item"
            class:active={browse.path === root.path}
            onclick={() => browse.load(root.path)}
          >
            <img class="cover" src={rootIcon(root.name)} alt="" />
            <span class="one-line">{root.name}</span>
          </button>
        </li>
      {/each}
    </ul>
  </section>

  <!-- Pinned to the bottom, like CasaOS's .bottom-area. The trash is real disk,
       so it gets a permanent home rather than hiding behind a menu. -->
  <div class="bottom-area">
    <button
      class="list-item"
      class:active={browse.path === TRASH_PATH}
      onclick={() => browse.openTrash()}
    >
      <span class="cover trash-glyph">🗑</span>
      <span class="one-line">Trash</span>
    </button>
  </div>
</nav>

<style>
  .nav-bar {
    width: var(--sidebar-width);
    flex: 0 0 var(--sidebar-width);
    background: var(--sidebar-bg);
    height: 100%;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
  }
  h3 {
    font-size: var(--font-section);
    font-weight: 600;
    margin: 0;
    padding: 0.75rem 1.25rem;
    text-align: left;
  }
  .list-container {
    list-style: none;
    margin: 0;
    padding: 0.75rem;
    font-size: 14px;
  }
  .list-item {
    display: flex;
    align-items: center;
    width: 100%;
    padding: 0.625rem 0.75rem;
    margin: 0.125rem 0;
    border: 0;
    background: transparent;
    border-radius: var(--radius-item);
    font-size: 14px;
    font-weight: 600;
    color: inherit;
    text-align: left;
    transition: all 0.2s;
  }
  .list-item:hover {
    background: var(--sidebar-hover);
  }
  .list-item.active {
    background: var(--sidebar-active);
  }
  .bottom-area {
    margin-top: auto;
    border-top: rgba(0, 0, 0, 0.1) 1px solid;
    margin: auto 0.75rem 0.75rem;
    padding-top: 0.75rem;
  }
  .trash-glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 18px;
  }
  .cover {
    width: 24px;
    height: 24px;
    margin-right: 0.5rem;
    flex-shrink: 0;
  }
</style>
