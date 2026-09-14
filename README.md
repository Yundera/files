# Files

**Files** is a lightweight web file manager for a single host. It is the Yundera
PCS replacement for [FileBrowser](https://github.com/filebrowser/filebrowser),
whose upstream was archived on 2026-09-01.

It mirrors the **CasaOS Files UX** — the same sidebar, breadcrumb, grid/list
views, context menu and upload tray — but is a fresh, much smaller implementation
with everything the PCS does not need taken out: **no authentication, no
accounts, no sharing, no network-storage mounts.**

It ships as **one container**: a Go binary with the Svelte UI embedded in it,
serving a tree that is bind-mounted into it. No database, no login screen.

> **No auth, by design.** Files has no login and assumes it sits behind an
> authenticating proxy. On a Yundera PCS that proxy is **AppShield**, and the
> app compose puts the backend on a private network so the gate is the only way
> in. Never publish the backend port directly.

---

## Why replace FileBrowser rather than keep it

| | FileBrowser | Files |
|---|---|---|
| Upstream | Archived 2026-09-01 | Ours |
| Auth | Its own user DB, has to be disabled at install time by a `set-noauth` init step | None — the gate is the boundary |
| Sharing | Anonymous share links, which force three path prefixes to be exempted from the SSO gate (`share/`, `api/public/`, `static/`) | None — **the whole app is gated, no exemptions** |
| State | bolt database | None, beyond a trash folder and a thumbnail cache |
| UX | Its own | CasaOS |

The share-link exemption is the load-bearing reason. Every prefix carved out of
the gate is a hole that has to stay correct forever. Dropping sharing closes all
three and makes the security story "the gate, and nothing else".

---

## Quick start

```bash
cp .env.example .env          # set DATA_HOST_PATH, PUID, PGID for your host
docker compose up -d --build  # http://localhost:8080
```

---

## Scope

### In scope

**Browse**
- Grid and list views, sort by name / size / modified / kind, ascending or descending
- Breadcrumb navigation, keyboard navigation, multi-select (click, ctrl-click, shift-click, marquee)
- Sidebar shortcuts for the tree roots (Documents, Downloads, Media, AppData) plus a folder tree
- Hidden-file toggle
- Name search inside the current folder's subtree (bounded walk, no index)

**Operate**
- New folder, new file
- Rename, delete, copy, cut, paste (with skip / overwrite / keep-both conflict handling)
- Download a file, or a multi-selection of files (see the limitation below)
- Copy path
- Long copy / move / delete run as **background jobs** with a progress tray and cancel, as in CasaOS

Downloading a **multi-selection** issues one browser download per file. They
land flat in your Downloads folder, because the browser will not let a page
choose a subdirectory. **Folders cannot be downloaded at all** — a folder has no
single URL, and without building an archive there is nothing to hand the browser.
Use the Samba app and mount the share for anything bulk or structured.

**Trash**
- Delete moves to a trash folder inside the app's own data directory, it is not immediate destruction
- A dedicated Trash view lists what is in it, with restore-to-original-location, delete-permanently and empty-trash
- Auto-purge after a configurable retention period

**Upload**
- Chunked, resumable uploads over the [tus](https://tus.io) protocol, as FileBrowser does
- Drag-and-drop of files and of whole folders, plus file and folder pickers
- An upload tray with per-file progress, cancel, and resume across a page reload

**View and edit**
- Images (with thumbnails in the grid), video and audio with seeking, PDF
- A text editor for plain text, source files, YAML and Markdown, with syntax
  highlighting, a size cap, and YAML syntax validation on save
- A side-by-side Markdown preview

**Ownership**
- Every file and folder the app creates is owned by `PUID:PGID` and created with
  the configured directory and file modes, so what you upload through the web UI
  and what your apps write over SMB or from a container look the same on disk

### Out of scope — deliberately, and not "not yet"

| Dropped | Because |
|---|---|
| Login, accounts, users, roles, permissions | AppShield is the auth system. One auth surface for the whole platform. |
| Sharing, public links, share passwords | The reason FileBrowser needs gate exemptions. Removing it removes them. |
| Network storage: SMB/NFS mounts, rclone remotes, ntfs tools | Host-level concerns. CasaOS put them behind this UI; they belong to the host, not to a file manager. |
| Disk / RAID / mount management | Same. |
| Archive create and extract (zip, tar, 7z) | Including download-as-zip. It buys folder download and structured multi-download, at the cost of a streaming archiver, a compression policy and a download with no progress bar. SMB covers the case better than a browser ever will. |
| Downloading a folder | Follows from the above. The honest limitation, stated in the UI rather than hidden. |
| Office viewers (`.docx`, `.xlsx`, `.pptx`) | A heavy dependency for a preview. |
| Full-text search, a search index | An index over `/DATA/Media` is a service, not a feature. Name search is bounded and needs no state. |
| Video thumbnails | Needs ffmpeg in the image. Revisit if the grid feels empty for media folders. |
| chmod / chown UI, symlink creation | The ownership model is `PUID:PGID` and two modes. Exposing the full POSIX surface invites users to break their own apps. |
| WebDAV | Use SMB (the Samba app) for mounting. |
| Terminal / command execution | The admin app has one, behind its own gate. |
| Set-as-wallpaper | A CasaOS-to-CasaOS integration with no counterpart here. |
| Multi-user anything | A PCS has one owner. |

---

## Configuration

| Env | Meaning | Default |
|---|---|---|
| `DATA_ROOT` | The tree the app serves, as seen **inside** the container. Everything below it is browsable; nothing above it is reachable. | `/DATA` |
| `STATE_DIR` | Where the app's own state lives — the trash, the thumbnail cache and the upload staging area. Hidden from the browse tree. | `${DATA_ROOT}/AppData/files` |
| `PUID` | Numeric uid that owns everything the app creates. | `1000` |
| `PGID` | Numeric gid that owns everything the app creates. | `1000` |
| `DIR_MODE` | Mode for created directories, octal. | `0775` |
| `FILE_MODE` | Mode for created files, octal. | `0664` |
| `TZ` | Timezone for the timestamps shown in the UI. | host default |
| `HTTP_ADDR` | Listen address. | `:8080` |
| `TRASH_RETENTION_DAYS` | Auto-purge trashed items older than this. `0` disables auto-purge. | `30` |
| `TRASH_MAX_GB` | Purge oldest trashed items once the trash exceeds this. `0` disables the cap. | `0` |
| `THUMB_CACHE_MB` | Size cap for the thumbnail cache, evicted least-recently-used. | `512` |
| `EDIT_MAX_BYTES` | Largest file the text editor will open. | `2097152` |
| `UPLOAD_MAX_BYTES` | Largest single upload. `0` is unlimited. | `0` |
| `SEARCH_MAX_DEPTH` | How deep a subtree search descends. | `8` |
| `SEARCH_TIMEOUT_MS` | Wall-clock budget for one search. | `2000` |

`DIR_MODE` and `FILE_MODE` are applied explicitly rather than via a umask,
because a umask can only clear bits and cannot make a group-writable file on a
host whose default umask is `022`. Group-writable is the point: it is what lets a
container running as a different uid in the same group still write what you
uploaded.

---

## Deployment on a PCS

Two containers. The **AppShield gate** is the only one on the `pcs` network and
the only one carrying Caddy labels; the backend is reachable only over a private
network, so the gate cannot be bypassed by another app on the box.

```
 internet ──▶ mesh-router-caddy ──▶ [files]  AppShield gate, container_name: files
                                        │    on: pcs + files-internal
                                        ▼
                                   [files-backend]  this app
                                        │           on: files-internal only
                                        ▼
                                   /DATA bind mount
```

Unlike the FileBrowser app, the gate needs no `ALLOWED_PATHS` beyond
`api/health` — there is no anonymous surface to exempt.

See [`ARCHITECTURE.md`](./ARCHITECTURE.md) for the internals and
[`IMPLEMENTATION-NOTES.md`](./IMPLEMENTATION-NOTES.md) for the build order and
the decisions behind them.
