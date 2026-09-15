<script lang="ts">
  import { browse, crumbs } from '../browse.svelte'
  const items = $derived(crumbs(browse.path))
</script>

<!-- Separator is a literal ">" in ::before, as in CasaOS — not an icon. The LEAF
     truncates, not the head, so the folder you are in is what shortens. -->
<nav class="breadcrumb-container">
  <ul>
    {#each items as item, i (item.path)}
      <li class:last={i === items.length - 1}>
        <button onclick={() => browse.load(item.path)}>{item.name}</button>
      </li>
    {/each}
  </ul>
</nav>

<style>
  .breadcrumb-container {
    position: relative;
    max-width: calc(100% - 1.5rem);
    overflow: hidden;
    /* Without this the nav is a flex item with the default `min-width: auto`, so
       it refuses to shrink below its content and the leaf is chopped off mid-word
       by the header instead of ellipsised by the rule below. */
    min-width: 0;
  }
  ul {
    display: flex;
    flex-wrap: nowrap;
    align-items: center;
    list-style: none;
    margin: 0;
    padding: 0;
    font-size: 1rem;
  }
  li {
    flex-shrink: 0;
    padding: 0 0.25rem 0 0;
    display: flex;
    align-items: center;
  }
  li + li::before {
    content: '>';
    color: var(--breadcrumb-sep);
    margin-right: 0.25rem;
  }
  li.last {
    flex-shrink: 1;
    overflow: hidden;
  }
  li.last button {
    font-weight: 700;
    display: block;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  button {
    border: 0;
    background: transparent;
    color: var(--breadcrumb-link);
    font-size: 1rem;
    padding: 0 0.5em;
    margin-left: 0.25em;
    border-radius: var(--radius-item);
    transition:
      color 0.2s,
      background-color 0.2s;
    max-width: 100%;
  }
  li:first-child button {
    margin-left: 0;
  }
  button:hover {
    color: var(--primary);
    background: var(--primary-light);
  }
</style>
