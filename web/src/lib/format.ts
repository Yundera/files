/** Human file size. CasaOS shows at most one decimal and no decimal for bytes. */
export function size(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  let v = bytes / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}

/** CasaOS's dateFmt: DD/MM hh:mm within the current year, DD/MM/YYYY hh:mm otherwise.
 *
 *  The server sends RFC 3339 UTC and the browser renders in local time — a Go
 *  binary formatting in TZ and a browser in another zone would disagree. */
export function date(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const p = (n: number) => String(n).padStart(2, '0')
  const day = `${p(d.getDate())}/${p(d.getMonth() + 1)}`
  const year = d.getFullYear() === new Date().getFullYear() ? '' : `/${d.getFullYear()}`
  return `${day}${year} ${p(d.getHours())}:${p(d.getMinutes())}`
}
