<script lang="ts">
  interface Props {
    title: string
    value?: string
    confirmLabel?: string
    onsubmit: (value: string) => void
    oncancel: () => void
  }
  let { title, value = '', confirmLabel = 'OK', onsubmit, oncancel }: Props = $props()

  // Capturing the initial value only is intended: App.svelte mounts this inside
  // an {#if prompt} block, so a new prompt is a new component instance. Making it
  // reactive would fight the user's typing if the parent ever re-rendered.
  // svelte-ignore state_referenced_locally
  let current = $state(value)
  let input: HTMLInputElement | undefined = $state()

  $effect(() => {
    input?.focus()
    // Select the stem, not the extension — renaming "report.pdf" should not make
    // it trivially easy to delete the ".pdf".
    const dot = current.lastIndexOf('.')
    if (dot > 0) input?.setSelectionRange(0, dot)
    else input?.select()
  })
</script>

<div
  class="scrim"
  role="button"
  tabindex="-1"
  onclick={oncancel}
  onkeydown={(e) => e.key === 'Escape' && oncancel()}
></div>
<div class="modal" role="dialog" aria-label={title}>
  <h4>{title}</h4>
  <form
    onsubmit={(e) => {
      e.preventDefault()
      if (current.trim()) onsubmit(current.trim())
    }}
  >
    <!-- svelte-ignore a11y_autofocus -->
    <input bind:this={input} bind:value={current} onkeydown={(e) => e.key === 'Escape' && oncancel()} />
    <div class="buttons">
      <button type="button" class="ghost" onclick={oncancel}>Cancel</button>
      <button type="submit" class="primary" disabled={!current.trim()}>{confirmLabel}</button>
    </div>
  </form>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    z-index: 200;
    border: 0;
  }
  .modal {
    position: fixed;
    top: 30%;
    left: 50%;
    transform: translateX(-50%);
    width: min(24rem, calc(100vw - 2rem));
    background: var(--surface);
    border-radius: var(--radius-card);
    padding: 1.25rem;
    z-index: 201;
    box-shadow: 0 0.5em 1em -0.125em rgb(10 10 10 / 20%);
  }
  h4 {
    margin: 0 0 0.75rem;
    font-size: 1rem;
    font-weight: 600;
  }
  input {
    width: 100%;
    font: inherit;
    padding: 0.5rem 0.625rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-small);
  }
  input:focus {
    outline: 2px solid var(--primary);
    outline-offset: -1px;
  }
  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }
  .ghost,
  .primary {
    border: 0;
    border-radius: 9999px;
    padding: 0.375rem 1rem;
    font-size: var(--font-base);
  }
  .ghost {
    background: var(--surface-2);
    color: var(--text);
  }
  .primary {
    background: var(--primary);
    color: var(--text-on-accent);
  }
  .primary:disabled {
    opacity: 0.5;
  }
</style>
