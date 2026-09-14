// Thin fetch wrapper, the same shape as packages/maison's lib/api/client.ts.
//
// The server's error contract is {"error": "<sentence>"} plus a meaningful HTTP
// status. The sentence is written to be shown to the user, so apiError throws it
// as the Error message and callers can put `e.message` straight on screen. The
// status is carried on the thrown error for the few callers that branch on it
// (409 raises the conflict dialog).

export class ApiError extends Error {
  status: number
  /** Extra fields the server put alongside `error`. The editor uses `line` from
   *  a 422 to jump to the offending line of YAML. */
  line?: number
  constructor(message: string, status: number, extra?: Record<string, unknown>) {
    super(message)
    this.status = status
    if (typeof extra?.line === 'number') this.line = extra.line
  }
}

async function req<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!res.ok) throw await apiError(method, path, res)
  if (res.status === 204) return undefined as T
  const ctype = res.headers.get('Content-Type') ?? ''
  return (ctype.includes('application/json') ? await res.json() : await res.text()) as T
}

async function apiError(method: string, path: string, res: Response): Promise<ApiError> {
  const text = await res.text().catch(() => '')
  try {
    const parsed = JSON.parse(text)
    if (parsed?.error) return new ApiError(String(parsed.error), res.status, parsed)
  } catch {
    // Not JSON — fall through to the generic message below.
  }
  return new ApiError(`${method} ${path} -> ${res.status} ${text}`.trim(), res.status)
}

export const api = {
  get: <T>(path: string, signal?: AbortSignal) => req<T>('GET', path, undefined, signal),
  post: <T>(path: string, body?: unknown) => req<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => req<T>('PUT', path, body),
  del: <T>(path: string, body?: unknown) => req<T>('DELETE', path, body),
}

/** URL of a cached thumbnail. The server folds the source's mtime and size into
 *  the cache key, so this URL changes when the file does and the response can be
 *  cached immutably. */
export function thumbUrl(path: string, width = 256): string {
  return `/api/fs/thumb?w=${width}&path=${encodeURIComponent(path)}`
}

/** URL of a file's bytes. `inline` is only honoured for media the server allows
 *  to render in place; everything else downloads regardless. */
export function rawUrl(path: string, inline = false): string {
  return `/api/fs/raw?path=${encodeURIComponent(path)}${inline ? '&inline=1' : ''}`
}
