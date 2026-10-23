<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    /** Shown above the items: the entry name, "3 items", or nothing. */
    title?: string
    onclose: () => void
    children: Snippet
  }
  let { title, onclose, children }: Props = $props()
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />

<!-- The phone form of every menu: a panel slid up from the bottom edge, where the
     thumb already is, instead of a popover at a cursor the screen does not have.
     Items are plain <button>s or <hr>s passed in as children. -->
<div class="scrim" role="presentation" onclick={onclose}></div>
<div class="sheet" role="menu" tabindex="-1">
  {#if title}
    <div class="title one-line">{title}</div>
  {/if}
  {@render children()}
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    z-index: 300;
  }
  .sheet {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 301;
    max-height: 80dvh;
    overflow-y: auto;
    background: var(--surface);
    border-radius: var(--radius-card) var(--radius-card) 0 0;
    padding: 0.5rem 0.5rem calc(0.5rem + env(safe-area-inset-bottom));
    box-shadow: 0 -0.5em 1.5em -0.25em rgb(10 10 10 / 20%);
    animation: up 0.18s ease-out;
  }
  @keyframes up {
    from {
      transform: translateY(100%);
    }
  }
  .title {
    padding: 0.5rem 0.75rem 0.75rem;
    font-weight: 600;
    color: var(--text-muted);
    font-size: var(--font-meta);
    border-bottom: 1px solid var(--border);
    margin-bottom: 0.25rem;
  }
  .sheet :global(button) {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    width: 100%;
    min-height: 48px;
    border: 0;
    background: transparent;
    text-align: left;
    padding: 0 0.75rem;
    border-radius: var(--radius-small);
    font-size: 1rem;
    color: inherit;
  }
  .sheet :global(button:active) {
    background: var(--surface-2);
  }
  .sheet :global(button.danger) {
    color: var(--red);
  }
  .sheet :global(button.on) {
    color: var(--primary);
  }
  .sheet :global(hr) {
    border: 0;
    border-top: 1px solid var(--border);
    margin: 0.25rem 0.5rem;
  }
</style>
