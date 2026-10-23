// Long-press as a Svelte action: `use:longpress={handler}`.
//
// Touch screens have no right-click and no ctrl-click, so a held finger is the
// gesture that selects. iOS Safari fires no `contextmenu` on a long-press at all,
// which is why this is built on pointer events rather than on that event.
const HOLD_MS = 450
/** Past this the finger is scrolling, not holding. */
const SLOP_PX = 10

export function longpress(node: HTMLElement, handler: () => void) {
  let current = handler
  let timer: ReturnType<typeof setTimeout> | undefined
  let start: { x: number; y: number } | null = null
  let fired = false

  function cancel() {
    clearTimeout(timer)
    timer = undefined
    start = null
  }

  function down(e: PointerEvent) {
    // Only fingers and pens: a mouse already has right-click and ctrl-click.
    if (e.pointerType === 'mouse' || !e.isPrimary) return
    fired = false
    start = { x: e.clientX, y: e.clientY }
    timer = setTimeout(() => {
      fired = true
      start = null
      navigator.vibrate?.(10)
      current()
    }, HOLD_MS)
  }

  function move(e: PointerEvent) {
    if (!start) return
    if (Math.hypot(e.clientX - start.x, e.clientY - start.y) > SLOP_PX) cancel()
  }

  // The press is followed by a click when the finger lifts. Swallowed in the
  // capture phase, or the tap handler would undo the selection it just made.
  function click(e: MouseEvent) {
    if (!fired) return
    fired = false
    e.preventDefault()
    e.stopImmediatePropagation()
  }

  // Android does fire `contextmenu` on a long-press, which would open the
  // desktop menu at the finger on top of the selection.
  function contextmenu(e: Event) {
    if ((e as PointerEvent).pointerType !== 'mouse' && (timer !== undefined || fired)) {
      e.preventDefault()
      e.stopPropagation()
    }
  }

  node.addEventListener('pointerdown', down)
  node.addEventListener('pointermove', move)
  node.addEventListener('pointerup', cancel)
  node.addEventListener('pointercancel', cancel)
  node.addEventListener('click', click, true)
  node.addEventListener('contextmenu', contextmenu, true)

  return {
    update(next: () => void) {
      current = next
    },
    destroy() {
      cancel()
      node.removeEventListener('pointerdown', down)
      node.removeEventListener('pointermove', move)
      node.removeEventListener('pointerup', cancel)
      node.removeEventListener('pointercancel', cancel)
      node.removeEventListener('click', click, true)
      node.removeEventListener('contextmenu', contextmenu, true)
    },
  }
}
