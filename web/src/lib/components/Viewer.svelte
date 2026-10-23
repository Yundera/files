<script lang="ts">
  import TextEditor from './TextEditor.svelte'
  import { api, ApiError, rawUrl } from '../api'
  import { size as fmtSize, date } from '../format'
  import { iconFor } from '../icons'
  import type { Entry, TextDoc } from '../types'
  import Icon from './Icon.svelte'

  interface Props {
    entry: Entry
    onclose: () => void
    onstatus: (msg: string) => void
    /** Step to the previous (-1) or next (+1) file of the listing. */
    onnav?: (delta: number) => void
  }
  let { entry, onclose, onstatus, onnav }: Props = $props()

  // Which viewer to use, from the extension. The lists mirror CasaOS's
  // filePanelMap so a file opens the same way it would there.
  const IMAGE = ['png', 'jpg', 'jpeg', 'bmp', 'gif', 'webp', 'svg', 'tiff', 'ico', 'avif']
  const VIDEO = ['mp4', 'webm', 'ogv', 'mov', 'm4v', 'mkv']
  const AUDIO = ['mp3', 'ogg', 'wav', 'flac', 'm4a', 'aac', 'opus']
  const PDF = ['pdf']

  const ext = $derived((entry.ext ?? '').toLowerCase())
  const media = $derived.by(() => {
    if (IMAGE.includes(ext)) return 'image'
    if (VIDEO.includes(ext)) return 'video'
    if (AUDIO.includes(ext)) return 'audio'
    if (PDF.includes(ext)) return 'pdf'
    return null
  })

  // Anything that is not media is offered to the editor, and the server decides.
  // It sniffs the content and refuses binary (415) or oversized (413) files, so
  // a .env, a .bashrc or an extensionless config opens without an allowlist of
  // names to keep up to date — and a name never makes a binary file editable.
  let doc = $state<TextDoc | null>(null)
  /** Why the editor was refused: '' for "not text", otherwise a sentence. */
  let refusal = $state<string | null>(null)

  $effect(() => {
    if (media) return
    const path = entry.path
    let cancelled = false
    api
      .get<TextDoc>(`/api/file/text?path=${encodeURIComponent(path)}`)
      .then((d) => {
        if (!cancelled) doc = d
      })
      .catch((e) => {
        if (cancelled) return
        refusal = e instanceof ApiError && e.status === 415 ? '' : e instanceof Error ? e.message : String(e)
      })
    return () => {
      cancelled = true
    }
  })

  const kind = $derived(media ?? (doc ? 'text' : refusal !== null ? 'none' : 'loading'))

  // inline=1 is only honoured by the server for media types it considers safe;
  // anything else downloads regardless of what is asked for here.
  const src = $derived(rawUrl(entry.path, true))

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose()
    // Not in the editor: there the arrows move the caret.
    else if (kind !== 'text' && e.key === 'ArrowRight') onnav?.(1)
    else if (kind !== 'text' && e.key === 'ArrowLeft') onnav?.(-1)
  }

  // ── Touch gestures on the stage ────────────────────────────────────────────
  // One finger: a horizontal swipe steps through the folder; a double tap
  // toggles zoom on an image; once zoomed, the finger pans instead. Two fingers
  // pinch. Built on pointer events, so it also works with a pen; a mouse is left
  // alone — it has the arrow keys and the browser's own controls.
  const MAX_ZOOM = 5
  let scale = $state(1)
  let tx = $state(0)
  let ty = $state(0)
  const pointers = new Map<number, { x: number; y: number }>()
  let pinch: { dist: number; scale: number } | null = null
  let drag: { x: number; y: number; t: number; tx: number; ty: number } | null = null
  let lastTap = 0

  function resetZoom() {
    scale = 1
    tx = 0
    ty = 0
  }

  function spread() {
    const [a, b] = [...pointers.values()]
    return Math.hypot(a.x - b.x, a.y - b.y)
  }

  function down(e: PointerEvent) {
    if (e.pointerType === 'mouse') return
    // Media controls own their drags: scrubbing a video must not change file.
    if ((e.target as HTMLElement).closest('video, audio')) return
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
    if (pointers.size === 2) {
      pinch = { dist: spread(), scale }
      drag = null
    } else if (pointers.size === 1) {
      drag = { x: e.clientX, y: e.clientY, t: Date.now(), tx, ty }
    }
  }

  function move(e: PointerEvent) {
    if (!pointers.has(e.pointerId)) return
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
    if (pinch && pointers.size === 2 && kind === 'image') {
      scale = Math.min(MAX_ZOOM, Math.max(1, (pinch.scale * spread()) / pinch.dist))
    } else if (drag && scale > 1) {
      tx = drag.tx + (e.clientX - drag.x)
      ty = drag.ty + (e.clientY - drag.y)
    }
  }

  function up(e: PointerEvent) {
    if (!pointers.delete(e.pointerId)) return
    if (pointers.size < 2) pinch = null
    if (scale <= 1.02 && scale !== 1) resetZoom()
    if (!drag || pointers.size > 0) return
    const dx = e.clientX - drag.x
    const dy = e.clientY - drag.y
    const quick = Date.now() - drag.t < 250
    drag = null
    if (scale === 1 && Math.abs(dx) > 60 && Math.abs(dx) > Math.abs(dy) * 1.5) {
      onnav?.(dx < 0 ? 1 : -1)
    } else if (quick && Math.hypot(dx, dy) < 10 && kind === 'image') {
      const now = Date.now()
      if (now - lastTap < 300) {
        if (scale > 1) resetZoom()
        else scale = 2.5
        lastTap = 0
      } else lastTap = now
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<div
  class="scrim"
  role="button"
  tabindex="-1"
  onclick={() => kind !== 'text' && onclose()}
  onkeydown={() => {}}
></div>

<div class="viewer" class:full={kind === 'text'} role="dialog" aria-label={entry.name}>
  {#if kind === 'text' && doc}
    <TextEditor {entry} {doc} {onclose} {onstatus} />
  {:else}
    <div class="bar">
      <span class="name one-line">{entry.name}</span>
      <div class="right">
        <a class="ghost" href={rawUrl(entry.path)} download={entry.name} aria-label="Download">
          <span class="label">Download</span><span class="glyph"><Icon name="download" /></span>
        </a>
        <button class="ghost" onclick={onclose} aria-label="Close">
          <span class="label">Close</span><span class="glyph"><Icon name="close" /></span>
        </button>
      </div>
    </div>

    <div
      class="body"
      class:stage={kind === 'image'}
      role="presentation"
      onpointerdown={down}
      onpointermove={move}
      onpointerup={up}
      onpointercancel={up}
    >
      {#if kind === 'image'}
        <img
          {src}
          alt={entry.name}
          draggable="false"
          style:transform={scale === 1 ? undefined : `translate(${tx}px, ${ty}px) scale(${scale})`}
        />
      {:else if kind === 'video'}
        <!-- svelte-ignore a11y_media_has_caption -->
        <video {src} controls autoplay></video>
      {:else if kind === 'audio'}
        <div class="audio">
          <img class="art" src={iconFor(entry)} alt="" />
          <audio {src} controls autoplay></audio>
        </div>
      {:else if kind === 'pdf'}
        <!-- The browser's own PDF viewer. Range requests from /api/fs/raw are
             what make page-jumps work without downloading the whole file. -->
        <iframe {src} title={entry.name}></iframe>
      {:else if kind === 'loading'}
        <p class="meta">Opening…</p>
      {:else}
        <div class="none">
          <img src={iconFor(entry)} alt="" />
          <p>{refusal || 'No preview for this file type.'}</p>
          <p class="meta">{fmtSize(entry.size)} · {date(entry.modTime)}</p>
          <a class="primary" href={rawUrl(entry.path)} download={entry.name}>Download</a>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    z-index: 200;
    border: 0;
  }
  .viewer {
    position: fixed;
    inset: 5vh 5vw;
    display: flex;
    flex-direction: column;
    background: var(--surface);
    border-radius: var(--radius-card);
    overflow: hidden;
    z-index: 201;
    box-shadow: 0 0.5em 1.5em -0.125em rgb(10 10 10 / 25%);
  }
  .viewer.full {
    inset: 2vh 2vw;
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.625rem 1rem;
    border-bottom: 1px solid var(--border);
  }
  .name {
    font-weight: 600;
  }
  .right {
    display: flex;
    gap: 0.5rem;
    flex-shrink: 0;
  }
  .ghost,
  .primary {
    border: 0;
    border-radius: 9999px;
    padding: 0.25rem 0.875rem;
    font-size: var(--font-base);
    text-decoration: none;
    display: inline-block;
  }
  .ghost {
    background: var(--surface-2);
    color: var(--text);
  }
  .primary {
    background: var(--primary);
    color: var(--text-on-accent);
    margin-top: 0.75rem;
  }
  .body {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--surface-sunken);
    overflow: auto;
    /* Vertical scroll stays the browser's; horizontal moves come to us. */
    touch-action: pan-y;
  }
  /* An image stage takes every gesture: pinch and pan are ours, not the page's. */
  .body.stage {
    touch-action: none;
    overflow: hidden;
  }
  .glyph {
    display: none;
  }
  img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
  }
  video {
    max-width: 100%;
    max-height: 100%;
  }
  iframe {
    width: 100%;
    height: 100%;
    border: 0;
  }
  .audio {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1rem;
  }
  .audio .art {
    width: 96px;
    height: 96px;
  }
  .none {
    text-align: center;
    color: var(--text-muted);
  }
  .none img {
    width: 80px;
    height: 80px;
  }
  .none p {
    margin: 0.5rem 0 0;
  }
  .meta {
    font-size: var(--font-meta);
  }
  @media (max-width: 768px) {
    .viewer,
    .viewer.full {
      inset: 0;
      border-radius: 0;
    }
    /* The editor follows the visual viewport (lib/media), so with the keyboard
       up the save bar is still on screen instead of scrolled above it. */
    .viewer.full {
      top: var(--vvt, 0px);
      bottom: auto;
      height: var(--vvh, 100dvh);
    }
    .bar {
      padding: calc(0.25rem + env(safe-area-inset-top)) 0.5rem 0.25rem 1rem;
    }
    .bar .ghost {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 40px;
      height: 40px;
      padding: 0;
      background: transparent;
    }
    .label {
      display: none;
    }
    .glyph {
      display: block;
    }
    .body {
      padding-bottom: env(safe-area-inset-bottom);
    }
  }
</style>
