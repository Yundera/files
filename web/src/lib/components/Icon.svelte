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
