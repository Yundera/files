// Live media-query flags, as runes so any component can branch on them.
//
// Two independent axes on purpose. `mobile` is about room: below the breakpoint
// the sidebar becomes a drawer and the trays go full-width. `touch` is about the
// input: a coarse pointer cannot double-click or right-click, so a tap opens and
// a long-press selects. A tablet in landscape is touch without being mobile.
const MOBILE = '(max-width: 768px)'
const TOUCH = '(pointer: coarse)'

class MediaState {
  mobile = $state(false)
  touch = $state(false)

  constructor() {
    if (typeof window === 'undefined') return
    this.#watch(MOBILE, (v) => (this.mobile = v))
    this.#watch(TOUCH, (v) => (this.touch = v))

    // The on-screen keyboard shrinks the visual viewport but not the layout one,
    // so `100dvh` still reaches behind it, and iOS scrolls the visual viewport
    // down to the caret. Publishing its height and offset lets the editor pin
    // itself to what is actually visible, save bar above the keyboard.
    const vv = window.visualViewport
    if (vv) {
      const root = document.documentElement.style
      const publish = () => {
        root.setProperty('--vvh', `${Math.round(vv.height)}px`)
        root.setProperty('--vvt', `${Math.round(vv.offsetTop)}px`)
      }
      vv.addEventListener('resize', publish)
      vv.addEventListener('scroll', publish)
      publish()
    }
  }

  #watch(query: string, set: (v: boolean) => void) {
    const mq = window.matchMedia(query)
    set(mq.matches)
    mq.addEventListener('change', (e) => set(e.matches))
  }
}

export const media = new MediaState()

/** Whether a click came from a finger. Decided per event where the browser says
 *  (a laptop with a touchscreen has a fine primary pointer, yet a tap on it is
 *  still a tap); the media flag only covers clicks that carry no pointer type,
 *  such as Enter on a focused row. */
export function isTouch(e: MouseEvent) {
  const type = (e as PointerEvent).pointerType
  return type ? type !== 'mouse' : media.touch
}

