<script lang="ts" module>
  // UI glyphs — deliberately separate from lib/icons.ts. That module maps a file
  // to one of CasaOS's coloured file-type illustrations; these are monochrome
  // line icons that take their colour from whatever button they sit in, so a
  // hover or a disabled state restyles them for free.
  //
  // One 24x24 grid, one stroke width, round caps and joins. Add a glyph by
  // adding an entry here — nothing else in the app needs to know.
  const glyphs = {
    uploadFile: [
      'M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z',
      'M14 3v5h5',
      'M12 18.5V12',
      'M9.5 14.5 12 12l2.5 2.5',
    ],
    uploadFolder: [
      'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
      'M12 18v-6',
      'M9.5 14.5 12 12l2.5 2.5',
    ],
    folderPlus: [
      'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
      'M12 11v5',
      'M9.5 13.5h5',
    ],
    eye: [
      'M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z',
      'M12 14.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Z',
    ],
    eyeOff: [
      'M9.9 5.8A8.5 8.5 0 0 1 12 5.5c6 0 9.5 6.5 9.5 6.5a16 16 0 0 1-2.8 3.6',
      'M6.3 7.8A16 16 0 0 0 2.5 12S6 18.5 12 18.5a8.7 8.7 0 0 0 3.6-.77',
      'M10.2 10.2a2.5 2.5 0 0 0 3.5 3.5',
      'M4 4l16 16',
    ],
    grid: ['M4.5 4.5h5v5h-5z', 'M14.5 4.5h5v5h-5z', 'M4.5 14.5h5v5h-5z', 'M14.5 14.5h5v5h-5z'],
    list: ['M9 6.5h11', 'M9 12h11', 'M9 17.5h11', 'M4.5 6.5h.8', 'M4.5 12h.8', 'M4.5 17.5h.8'],
    paste: [
      'M9 4.5h6v2.8H9z',
      'M15 5.9h2.5a2 2 0 0 1 2 2v10.6a2 2 0 0 1-2 2h-11a2 2 0 0 1-2-2V7.9a2 2 0 0 1 2-2H9',
    ],
    // The touch set: the phone layout trades labelled text buttons for these.
    menu: ['M4 6.5h16', 'M4 12h16', 'M4 17.5h16'],
    search: ['M10.5 17a6.5 6.5 0 1 0 0-13 6.5 6.5 0 0 0 0 13Z', 'M15.5 15.5 20 20'],
    close: ['M6 6l12 12', 'M18 6 6 18'],
    // Dots as small rings rather than zero-length strokes: at 1.6px a stroke dot
    // all but vanishes against white.
    more: [
      'M4.25 12a1.25 1.25 0 1 0 2.5 0 1.25 1.25 0 1 0-2.5 0',
      'M10.75 12a1.25 1.25 0 1 0 2.5 0 1.25 1.25 0 1 0-2.5 0',
      'M17.25 12a1.25 1.25 0 1 0 2.5 0 1.25 1.25 0 1 0-2.5 0',
    ],
    moreVertical: [
      'M10.75 5.5a1.25 1.25 0 1 0 2.5 0 1.25 1.25 0 1 0-2.5 0',
      'M10.75 12a1.25 1.25 0 1 0 2.5 0 1.25 1.25 0 1 0-2.5 0',
      'M10.75 18.5a1.25 1.25 0 1 0 2.5 0 1.25 1.25 0 1 0-2.5 0',
    ],
    filePlus: ['M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z', 'M14 3v5h5', 'M12 11.5v6', 'M9 14.5h6'],
    refresh: ['M19.5 12a7.5 7.5 0 1 1-2.2-5.3', 'M19.5 4.5v4h-4'],
    download: ['M12 4v11', 'M7.5 10.5 12 15l4.5-4.5', 'M5 19.5h14'],
    copy: [
      'M9 9h9.5a1.5 1.5 0 0 1 1.5 1.5V19a1.5 1.5 0 0 1-1.5 1.5H10.5A1.5 1.5 0 0 1 9 19z',
      'M15 9V5.5A1.5 1.5 0 0 0 13.5 4H5.5A1.5 1.5 0 0 0 4 5.5v8A1.5 1.5 0 0 0 5.5 15H9',
    ],
    cut: [
      'M6.5 20a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Z',
      'M17.5 20a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Z',
      'M8.4 15.9 17 4',
      'M15.6 15.9 7 4',
    ],
    trash: ['M4.5 7h15', 'M9.5 7V4.5h5V7', 'M6.5 7l1 12.5h9l1-12.5'],
    check: ['M5 12.5 10 17.5 19 7'],
    chevronLeft: ['M14.5 5.5 8 12l6.5 6.5'],
  }

  export type IconName = keyof typeof glyphs
  export { glyphs }
</script>

<script lang="ts">
  interface Props {
    name: IconName
    /** Rendered size in px. 18 sits right on a 14px label. */
    size?: number
  }
  let { name, size = 18 }: Props = $props()
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.6"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"
>
  {#each glyphs[name] as d (d)}
    <path {d} />
  {/each}
</svg>

<style>
  svg {
    display: block;
    flex-shrink: 0;
  }
</style>
