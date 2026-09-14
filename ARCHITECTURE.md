# Architecture

## Shape

One container, one process, no database.

```
┌─ files-backend (this repo) ─────────────────────────────────┐
│                                                             │
│  cmd/files/main.go      config from env, wire, listen       │
│                                                             │
│  internal/                                                  │
│    config/    env parsing, the one place defaults live      │
│    vfs/       the traversal-safe filesystem boundary        │
│    ownership/ PUID:PGID + DIR_MODE/FILE_MODE application    │
│    listing/   read a directory, classify, sort              │
│    jobs/      background copy/move/delete + progress        │
│    trash/     move-to-trash, restore, purge                 │
│    upload/    tus server, staging, commit-on-complete       │
│    textfile/  guarded read/write, YAML validation           │
│    thumb/     on-the-fly downscale + bounded cache          │
│    search/    bounded subtree name search                   │
│    server/    chi router, SSE, SPA embed                    │
│    ui/dist/   the built Svelte app, go:embed'ed             │
│                                                             │
│  web/         Svelte 5 + vite -> internal/ui/dist           │
└─────────────────────────────────────────────────────────────┘
```

The layout mirrors `packages/maison` on purpose: same language, same router,
same embed-the-UI-at-build-time trick, same publish path. A change of shape in
one is portable to the other, and `web/src/styles/tokens.css` — Maison's design
tokens, ported verbatim from CasaOS-UI's SCSS variables — is copied in, which is
what buys CasaOS visual parity without redesigning anything.

---

## The filesystem boundary

Everything the app can touch is one directory: `DATA_ROOT`. This is the single
most security-relevant part of the app, so it gets one chokepoint and no
exceptions.

**`internal/vfs` is the only package in the repo that calls `os.*` on a
user-supplied path.** Every handler takes a *virtual path* — a slash-separated,
absolute-looking string like `/Documents/notes/todo.md` — and hands it to the
vfs. Nothing else ever concatenates a user string onto `DATA_ROOT`.

The vfs is built on `os.Root` (Go 1.24+), which resolves every path component
inside an opened directory and refuses to escape it. That makes `../../etc`, an
absolute path, a symlink pointing outside the root, and a symlink *raced into
place between the check and the open* all fail at the syscall layer instead of
at a string check we have to get right. The TOCTOU race is the reason to use it
rather than `filepath.Clean` plus a prefix test.

On top of that, the vfs enforces:

- **Normalisation.** Reject NUL bytes and any component that is `.` or `..`
  after cleaning; collapse separators; a path must be valid UTF-8.
- **The state-dir exclusion.** `STATE_DIR` (by default
  `${DATA_ROOT}/AppData/files`) is filtered out of every listing and rejected as
  a target by every mutating call. Without this the user can browse into their
  own trash, "delete" something into the trash twice, or drop a file into the
  tus staging directory and watch it get garbage-collected. The Trash view
  reaches that content through `internal/trash`, never through the vfs.
- **Symlink policy.** Symlinks are *listed* (with a distinct kind, so the UI can
  show them) but never followed out of the root — which `os.Root` enforces for
  us. A symlink whose target is inside the root works normally.

---

## Ownership and modes

The backend **runs as root inside the container** and chowns what it creates to
`PUID:PGID`.

This is not the obvious choice, so it is worth writing down why the obvious one
is wrong. Running the container as `user: $PUID:$PGID` — what the FileBrowser
app does — gives correct ownership for free and no root. But a file manager over
`/DATA` has to read `/DATA/AppData`, and app data directories are routinely owned
by other uids with mode `0700`: `AppData/hubs/pgdata` is uid 70, `drwx------`,
and `AppData/duplicati/config` is root, `drwx------`. As `PUID` those are
unreadable, so the app would show empty folders and permission errors over a
large part of the tree it exists to manage. The same constraint already bit
Maison's kopia backup engine.

Capabilities are not a middle ground. Docker has no `--ambient-cap`, so
`--user 1000 --cap-add DAC_READ_SEARCH` grants nothing to a non-root process —
verified on a real box. The choice is genuinely root or blind.

So: root, with the blast radius pulled in from the other side —

```yaml
cap_drop: [ALL]
cap_add:  [CHOWN, FOWNER, FSETID, DAC_OVERRIDE, DAC_READ_SEARCH]
security_opt: [no-new-privileges:true]
read_only: true          # with tmpfs for /tmp
```

Application rules, in `internal/ownership`:

- **Creating** a file or directory: `chown(PUID, PGID)` then
  `chmod(FILE_MODE | DIR_MODE)` — applied on the **open file descriptor**
  (`f.Chown`/`f.Chmod`), not by path. Go's own docs flag `Root.Chmod` and
  `Root.Chown` as racy on Unix: if the target flips from a regular file to a
  symlink mid-call, the operation can land on the link instead of its target. An
  fd cannot be redirected that way. Both explicitly, not via umask — a umask can
  only clear bits, so on a host with the usual `022` it could never produce the
  group-writable result that lets a container running as another uid in the same
  group write to what you uploaded.
- **Overwriting an existing** file: **preserve its current uid, gid and mode.**
  Editing an app's `config.yaml` must not silently re-home it to `PUID` and
  break the app that owns it. This asymmetry is deliberate and is the rule most
  likely to be "simplified" away by a later change.
- **Copying:** the copy is a new file, so it gets `PUID:PGID` and the configured
  mode. **Moving** (rename) does not touch ownership at all — the inode is the
  same one.
- Mode env vars are parsed once at startup; a malformed value is a fatal startup
  error, not a silent fallback.

---

## HTTP API

REST under `/api`, JSON in and out, virtual paths as described above. Errors are
`{"error": {"code": "...", "message": "..."}}` with a stable machine code, so the
UI can say "that name already exists" rather than echoing a raw `errno`.

### Browse

| Route | Notes |
|---|---|
| `GET /api/fs/list?path=&sort=&order=&hidden=` | One page of entries: name, kind (`file`/`dir`/`symlink`), size, mtime, mode, whether a thumbnail is available. Paginated by cursor — a Downloads folder with 200k files must not be one JSON blob. |
| `GET /api/fs/stat?path=` | Single entry, for the details panel. |
| `GET /api/fs/tree?path=&depth=1` | Directories only, for the sidebar tree. |
| `GET /api/fs/search?path=&q=` | Bounded subtree name search. |

### Read

| Route | Notes |
|---|---|
| `GET /api/fs/raw?path=` | Served with `http.ServeContent`, which gives Range requests and conditional GETs for free. Range is not optional: it is what makes video seeking and PDF page-jumps work. Always `Content-Disposition: attachment` unless `?inline=1`, and always `X-Content-Type-Options: nosniff`. |
| `GET /api/fs/thumb?path=&w=` | Cached downscale. |
| `GET /api/file/text?path=` | Guarded text read. |

### Mutate

| Route | Notes |
|---|---|
| `POST /api/fs/mkdir` | `{path, name}` |
| `POST /api/fs/touch` | `{path, name}` — new empty file |
| `POST /api/fs/rename` | `{path, newName}` — same directory only; cross-directory is a move |
| `POST /api/fs/move` | `{sources[], dest, conflict}` → job |
| `POST /api/fs/copy` | `{sources[], dest, conflict}` → job |
| `POST /api/fs/delete` | `{paths[]}` → job, moves to trash |
| `PUT  /api/file/text` | `{path, content, baseMtime}` |

`conflict` is `skip`, `overwrite` or `keepBoth`, matching the CasaOS
paste menu (`Paste - Overwrite` / `Paste - Skip`).

### Jobs

Copying 50 GB cannot be a synchronous request, and CasaOS's UI already assumes it
is not — it has an operation status bar with per-item progress and cancel. So
`copy`, `move` and `delete` return `202` with `{jobId}` immediately.

| Route | Notes |
|---|---|
| `GET /api/jobs` | Current and recently finished jobs |
| `POST /api/jobs/{id}/cancel` | Cooperative cancel at the next item boundary |
| `GET /api/events` | SSE stream: job progress, job completion, and directory-changed hints so an open listing refreshes itself |

Jobs live in memory only. A restart loses the job list; it does not lose the work
already done, and a half-finished copy leaves a `.part` file that the next run
can be told to overwrite. Persisting a job queue would mean a database, which the
app does not have and should not get.

### Trash

| Route | Notes |
|---|---|
| `GET /api/trash` | Items with original path, size, deleted-at |
| `POST /api/trash/restore` | `{ids[], conflict}` |
| `POST /api/trash/delete` | `{ids[]}` — permanent |
| `POST /api/trash/empty` | Permanent, all of it |

### Upload

tus 1.0.0 at `/api/tus/`, plus the standard tus headers on `OPTIONS`. See below.

### Misc

`GET /api/health` — the one route the gate lets through unauthenticated.
`GET /api/config` — roots, limits and feature flags, so the UI does not hardcode
what the server was configured with.

---

## Trash

Trash lives **inside the app's own folder**, at `${STATE_DIR}/trash`:

```
/DATA/AppData/files/trash/
  01JC8F.../            one trashed item
    meta.json           {originalPath, name, isDir, size, mode, uid, gid, deletedAt}
    payload/<name>      the file or directory itself, original basename kept
```

Per-item metadata rather than one index file: two concurrent deletes then never
contend on the same file, and a corrupted `meta.json` loses exactly one item's
restore path instead of the whole trash.

**Delete is `rename(2)`** into that tree — instant regardless of size, and
atomic. This works because a PCS has a single filesystem: `/DATA` is a plain
directory on the root ext4 volume, not a mountpoint (verified on holyhorse and
wisera). The code must still handle `EXDEV` by falling back to copy-then-delete,
because that layout is an observation about today's boxes, not a guarantee — and
the failure mode if we assume it is a delete that errors out on a future PCS
whose `/DATA` is a separate volume.

**Restore** renames back to `originalPath`, recreating missing parent
directories with the configured ownership and mode, and applying the usual
conflict policy if something is there now.

**Purge** runs at startup and on a daily ticker: anything older than
`TRASH_RETENTION_DAYS`, then — if `TRASH_MAX_GB` is set — oldest-first until the
trash is under the cap.

The trash is real disk. The Trash view shows its total size, and the app surfaces
it in the UI rather than letting a user wonder where their free space went.

---

## Upload

The tus protocol, as FileBrowser uses. Resumable, chunked, and with a
well-specified client (`tus-js-client`) so the browser half is not ours to
invent.

```
browser ──POST /api/tus/──▶  create upload, get id
        ──PATCH  ...     ──▶  send a chunk, repeat, resume after a drop
                              chunks land in ${STATE_DIR}/uploads/<id>
        ── on complete   ──▶  chown/chmod, then rename into the destination
```

Staging inside `STATE_DIR` and committing with a rename means the destination
folder never contains a partial file that the user can see, click, or that
another app can pick up half-written. Same-filesystem again, same `EXDEV`
fallback.

Metadata on the create request carries the destination virtual path and the
filename; both are validated through the vfs **at create time and again at
commit time**, because a resumed upload can complete long after its directory
was renamed out from under it.

Abandoned uploads are garbage-collected after 24 hours.

---

## Text editing

`internal/textfile` refuses before it reads:

1. Size over `EDIT_MAX_BYTES` → refuse with the actual size, so the UI can say
   why.
2. Binary sniff — NUL byte in the first 8 KB, or invalid UTF-8 → refuse. Opening
   a JPEG in a text editor and saving it destroys it.

Writing is atomic: a temp file in the *same directory* (so the rename stays
within one filesystem), ownership and mode applied per the rules above, then
`rename` over the target.

Lost-update protection: the client sends the `baseMtime` it loaded, and the
server rejects the write with `409` if the file changed underneath. Cheap, and
the alternative — last-writer-wins on a file that another app also writes — is
the kind of data loss users never trace back.

For `.yml` / `.yaml`, the content is parsed with `gopkg.in/yaml.v3` before the
write and a syntax error **blocks the save**, returning line and column. A file
manager on a PCS is going to be used to edit compose files; saving a broken one
and finding out when a stack fails to come up is a bad trade against one round
trip.

---

## Thumbnails

`GET /api/fs/thumb` decodes, downscales to the requested width (one of a small
fixed set), encodes JPEG, and caches at
`${STATE_DIR}/thumbs/<sha256(path|mtime|size|width)>.jpg`. Keying on mtime and
size means an edited file gets a new key with no invalidation logic.

Pure Go, no cgo: `image/jpeg`, `image/png`, `image/gif`, and
`golang.org/x/image/webp` for decode; `golang.org/x/image/draw` with
`draw.CatmullRom` for the resize. JPEG out rather than WebP because encoding WebP
in pure Go is not well served. Formats with no pure-Go decoder — HEIC, AVIF, RAW
— get no thumbnail and the UI falls back to a kind icon, which is the correct
degradation rather than a broken image.

Guards that matter on a folder of 4000 photos: a bounded worker pool (`GOMAXPROCS`,
capped), a decoded-pixel ceiling so a decompression-bomb image cannot exhaust
memory, and LRU eviction once the cache passes `THUMB_CACHE_MB`. Thumbnails are
requested lazily by an `IntersectionObserver`, so scrolling past a folder does
not queue every file in it.

---

## Search

A bounded walk from the current directory: case-insensitive substring on the
basename, `SEARCH_MAX_DEPTH` deep, `SEARCH_TIMEOUT_MS` of wall clock, capped at
200 results, skipping `STATE_DIR`. Whichever bound is hit first ends the walk and
the response carries `truncated: true` so the UI can say "showing the first 200"
instead of implying it found everything.

No index, no state, no background crawler. That is the whole point of scoping it
this way — the moment it needs an index it becomes a service with a lifecycle,
and `/DATA/Media` is exactly the tree that makes an index expensive.

---

## Multi-file download

There is no archiver. Downloading a selection means the UI issues one ordinary
`GET /api/fs/raw` download per selected file, which the browser handles with its
own download manager — progress, resume and all — for free.

Two limitations follow from the browser, not from the backend, and the UI states
both rather than hiding them:

- **Flat.** The `download` attribute cannot contain a path separator, so every
  file lands directly in the user's Downloads folder. Structure is lost.
- **Files only.** A folder has no URL to download. With no archive format there
  is nothing to hand the browser, so folders are not downloadable and the action
  is disabled with a tooltip pointing at SMB.

Sequencing matters: browsers rate-limit or silently drop rapid programmatic
downloads, and Safari is the strictest. Issue them from `internal/../web` one at
a time with a short delay, cap a batch at a sane number of files, and confirm
above that cap — the browser's own "allow multiple downloads?" prompt appears
once per origin and a user who dismisses it sees nothing happen at all.

The File System Access API (`showDirectoryPicker`) *would* restore both folder
download and real structure by writing a tree straight to disk. It is
Chromium-desktop only — no Firefox, no Safari — so it is not the v1 answer. It
is the thing to reach for if folder download turns out to be missed.

## Frontend

Svelte 5 (runes), vite, no router library — a hash/path router of about 40 lines
covers `/browse/*`, `/trash` and `/edit/*`.

| Area | Components |
|---|---|
| Chrome | `TopBar` (breadcrumb, search, view toggle, sort), `SideBar` (roots + folder tree + Trash) |
| Listing | `GridView`, `ListView`, `EntryIcon`, `EmptyState` |
| Interaction | `ContextMenu`, `SelectionLayer` (marquee + keyboard), `DropZone` |
| Long ops | `JobTray` — the CasaOS operation status bar |
| Upload | `UploadTray`, `uploadQueue.svelte.ts` wrapping `tus-js-client` |
| Modals | `NewFolder`, `NewFile`, `Rename`, `Details`, `ConfirmDelete`, `ConflictDialog` |
| Viewers | `ImageViewer`, `MediaPlayer`, `PdfViewer`, `TextEditor` (CodeMirror 6), `MarkdownPreview` |

The clipboard (cut/copy/paste) is client-side state: paste is what sends the
`move` or `copy` request. Nothing is held server-side between the two, so a
reload simply clears the clipboard rather than leaving an orphaned pending
operation.

Assets are hashed by vite and embedded with `go:embed`, served with a long
`Cache-Control` for hashed names and `no-cache` for `index.html`.

---

## Security boundary

The app authenticates nobody. It is reachable only through the AppShield gate,
and the gate is only the boundary if nothing else can reach the backend — which
is why the backend sits on a private compose network with **no** Caddy labels and
**no** published ports, exactly as the FileBrowser app does today. Putting the
backend on `pcs` would let any other app on the box read and write the whole of
`/DATA` with no credential at all.

AppShield forwards identity (`Remote-User`, `X-Forwarded-*`, and a signed
`X-AppShield-Assertion`). **v1 ignores all of it** — a PCS has one owner, and
there is no per-user state to attach it to. The hook for later is that the signed
assertion, not the plain headers, is the thing to verify if this ever becomes
multi-user; plain headers are forgeable by anything on the same network.

Other defaults: `nosniff` on every response, `Content-Disposition: attachment` by
default so an uploaded `.html` cannot execute on the app's origin, a
`Content-Security-Policy` with no `unsafe-inline`, and no CORS headers at all.
