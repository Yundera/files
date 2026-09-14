<script lang="ts">
  import TextEditor from './TextEditor.svelte'
  import { rawUrl } from '../api'
  import { size as fmtSize, date } from '../format'
  import { iconFor } from '../icons'
  import type { Entry } from '../types'

  interface Props {
    entry: Entry
    onclose: () => void
    onstatus: (msg: string) => void
  }
  let { entry, onclose, onstatus }: Props = $props()

  // Which viewer to use, from the extension. The lists mirror CasaOS's
  // filePanelMap so a file opens the same way it would there.
  const IMAGE = ['png', 'jpg', 'jpeg', 'bmp', 'gif', 'webp', 'svg', 'tiff', 'ico', 'avif']
  const VIDEO = ['mp4', 'webm', 'ogv', 'mov', 'm4v', 'mkv']
  const AUDIO = ['mp3', 'ogg', 'wav', 'flac', 'm4a', 'aac', 'opus']
  const PDF = ['pdf']
  // Everything the editor will accept. Extensionless build files are included by
  // name because a Dockerfile has no extension to match on.
  const TEXT_NAMES = ['dockerfile', 'makefile', 'caddyfile', 'readme', 'license']
  const TEXT = [
    'txt', 'log', 'md', 'markdown', 'yml', 'yaml', 'json', 'jsonld', 'xml', 'html', 'htm',
    'css', 'scss', 'less', 'js', 'mjs', 'cjs', 'ts', 'jsx', 'tsx', 'sh', 'bash', 'zsh',
    'env', 'conf', 'config', 'ini', 'cfg', 'toml', 'service', 'py', 'go', 'rs', 'rb', 'pl',
    'c', 'h', 'cpp', 'cs', 'java', 'sql', 'csv', 'tsv', 'srt', 'vue', 'svelte', 'gitignore',
  ]

  const ext = $derived((entry.ext ?? '').toLowerCase())
  const kind = $derived.by(() => {
    if (IMAGE.includes(ext)) return 'image'
    if (VIDEO.includes(ext)) return 'video'
    if (AUDIO.includes(ext)) return 'audio'
    if (PDF.includes(ext)) return 'pdf'
    if (TEXT.includes(ext) || TEXT_NAMES.includes(entry.name.toLowerCase())) return 'text'
    return 'none'
  })

  // inline=1 is only honoured by the server for media types it considers safe;
  // anything else downloads regardless of what is asked for here.
  const src = $derived(rawUrl(entry.path, true))

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose()
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
  {#if kind === 'text'}
    <TextEditor {entry} {onclose} {onstatus} />
  {:else}
    <div class="bar">
      <span class="name one-line">{entry.name}</span>
      <div class="right">
        <a class="ghost" href={rawUrl(entry.path)} download={entry.name}>Download</a>
        <button class="ghost" onclick={onclose}>Close</button>
      </div>
    </div>

    <div class="body">
      {#if kind === 'image'}
        <img {src} alt={entry.name} />
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
      {:else}
        <div class="none">
          <img src={iconFor(entry)} alt="" />
          <p>No preview for this file type.</p>
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
</style>
