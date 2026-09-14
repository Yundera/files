# Implementation notes

Working notes for building the app: the order to build it in, the traps that are
worth knowing before hitting them, the decisions that were made and why, and what
is still open. `README.md` is the scope and `ARCHITECTURE.md` is the design; this
file is the one that goes stale, and that is fine.

---

## Toolchain

There is **no local `go` and no local `node`** in the dev container. Both stacks
run in Docker against the host engine, bind-mounting the **host** path:

```bash
P=/d/workspace/yundera/yundera-root/packages/files

# UI first — vite output is embedded in the binary, so go build needs it to exist
docker run --rm -v $P/web:/src -w /src node:22 npm ci
docker run --rm -v $P/web:/src -w /src node:22 npm run build

docker run --rm -v $P:/src -w /src golang:1.26 go build ./cmd/files
docker run --rm -v $P:/src -w /src golang:1.26 go test ./...
docker run --rm -v $P:/src -w /src golang:1.26 go vet ./...
```

Go 1.26. Two independent reasons, both hard requirements:
- The `os.Root` methods the vfs is built on (`Rename`, `Chown`, `Lchown`,
  `MkdirAll`, `RemoveAll`, `WriteFile`) are 1.24/1.25 additions.
- `golang.org/x/image` (webp and bmp decoding, CatmullRom scaling) requires
  1.26 from v0.46.0. Pinning an older x/image was the alternative; 1.26 is what
  `packages/maison`'s Dockerfile already builds with, so this keeps the two in
  step rather than freezing a dependency.

Add a `Makefile` or a `dev/build.sh` wrapping the above on day one; nobody will
retype those lines.

---

## Build order

Each milestone is meant to be demoable on its own and to leave the app in a
usable state. Resist building the vfs and the UI in parallel — almost every
frontend decision is downstream of the exact listing payload.

### M0 — Skeleton
Repo layout, `internal/config` with every env var from the README parsed and
validated at startup, `GET /api/health`, `go:embed` of an empty Svelte shell,
`Dockerfile` (node:22 build → golang:1.25 build → distroless/static), a
`docker-compose.yml` for local dev bind-mounting a throwaway `./DATA`, and the
GitHub Action publishing `ghcr.io/yundera/files` on push to `main` and on `v*`
tags. Matching Maison, the version is a **tag**, not a file to bump.

**Do M0 completely.** Getting the image published and pullable before there is
anything to see removes the release step from the critical path later.

### M1 — Browse, read-only
`internal/vfs`, `internal/listing`, `GET /api/fs/{list,stat,tree,raw}`, and the
UI chrome: sidebar, breadcrumb, grid and list views, sort, selection, details
panel. Serve `raw` through `http.ServeContent` from the start so Range works
before any media viewer needs it.

This milestone is the one to spend test effort on — see the vfs test list below.

### M2 — Mutate, jobs, trash
`mkdir`, `touch`, `rename`, then `copy`/`move`/`delete` on top of
`internal/jobs`, the WebSocket hub, the conflict dialog, the job tray, and
`internal/trash` with the Trash view.

Build **delete-to-trash before copy/move**: it is a single `Rename` and it gets
the job plumbing and the WebSocket wiring exercised with the least moving parts.

### M3 — Upload
tusd v2 as a library plus `tus-js-client`, staging in `STATE_DIR/uploads`,
commit-by-rename, the upload tray, drag-and-drop of files and of directories.

### M4 — View and edit
Image / media / PDF viewers, then CodeMirror 6, YAML validation on save, and the
Markdown preview pane.

### M5 — Thumbnails and search
The two independent extras. Either can slip past a first release without blocking
the rest. Multi-file download is not a milestone — it is a handful of lines in
the UI on top of the `raw` route M1 already ships.

### M6 — Ship on a PCS
The store app entry (AppShield gate + backend on a private network), tested on a
real box, and the FileBrowser migration note.

---

## Traps

### `os.Root` follows symlinks — within the root
The doc comment is worth reading in full before writing the vfs. It **does**
follow symlinks; what it refuses is a link that resolves outside the root, or an
absolute link. That is the behaviour we want. But it explicitly does **not**
prevent crossing a filesystem boundary, a bind mount, `/proc`, or a device file
that happens to live under the root — so "confined to `DATA_ROOT`" means confined
by path, not confined by device.

### `Root.Chmod` and `Root.Chown` have a documented TOCTOU race on Unix
If the target flips from a regular file to a symlink mid-operation, the operation
can land on the link instead of its target. Since `internal/ownership` calls
these on every created inode, do it the safe way instead: keep the `*os.File`
from `Create`/`OpenFile` and use `f.Chown` / `f.Chmod`, which act on the open
descriptor and cannot be redirected. Use `Root.Lchown` where a path is
unavoidable. This is a small detail with a real consequence on a tree that
contains other apps' data.

### `STATE_DIR` inside vs outside `DATA_ROOT`
By default `STATE_DIR` is `${DATA_ROOT}/AppData/files`, i.e. **inside** the root.
That is what makes delete-to-trash a single `Root.Rename` within one `os.Root`.
The README lets it be moved, and if it is moved outside `DATA_ROOT` then a
cross-root rename is needed, which `os.Root` does not offer — that path has to
drop to `os.Rename` on two validated absolute paths, and may hit `EXDEV`. Either
implement both paths or make an out-of-root `STATE_DIR` a startup error. **The
startup error is the better v1** — one code path, and no one has asked for the
flexibility yet.

### Hiding `STATE_DIR` is load-bearing, not cosmetic
If it leaks into listings, a user can browse into their own trash, delete a
trashed item into the trash again, and drop files into the tus staging directory
that the GC then eats. Filter it in `internal/vfs` at the chokepoint, not in the
listing handler and not in the UI — there are too many handlers to remember.

### `EXDEV` on every rename
Delete-to-trash, upload commit and atomic text save are all renames that assume
one filesystem. On today's PCS boxes that holds — `/DATA` is a plain directory on
the root ext4 volume, not a mountpoint, verified on holyhorse and wisera. It is
an observation, not a guarantee. Write one `vfs.MoveOrCopy` helper that falls
back to copy-then-delete on `EXDEV` and call it from all three, so a future PCS
layout with `/DATA` on its own volume degrades in speed rather than erroring out.

### Directory listings can be enormous
`/DATA/Downloads` on a real box can hold tens of thousands of entries. Use
`File.ReadDir(n)` in batches and paginate the API by cursor. `os.ReadDir` on a
200k-entry directory allocates the whole slice and stats every entry, and the
tab will hang. Decide pagination in M1 — retrofitting it through the UI's
selection and sorting model later is painful.

### `filepath.Walk` stats everything
For search, use `fs.WalkDir` (via `Root.FS()`), which carries `DirEntry` and only
stats on demand. On `/DATA/Media` the difference is the whole budget.

### The container runs as root — keep the compose honest
`cap_drop: [ALL]` plus the five caps in `ARCHITECTURE.md`,
`no-new-privileges`, `read_only: true` with a tmpfs for `/tmp`. And the backend
must carry **no Caddy labels and no published ports**, on the private network
only. If someone "simplifies" the compose by putting the backend on `pcs`, every
app on the box gets unauthenticated root-level access to `/DATA`. Put that
warning in the compose file as a comment, where the person making the change will
see it.

### Decompression bombs
A 200 MB PNG that decodes to 40 GB of pixels will OOM the container. Check
dimensions from the header via `image.DecodeConfig` and refuse above a pixel
ceiling **before** `image.Decode`. Applies to the thumbnailer, which is the only
place that decodes.

### tus re-validation at commit
An upload can be resumed hours after it started, by which time its destination
directory may have been renamed or deleted. Validate the destination through the
vfs at create time *and again* at commit time, and fail the upload cleanly rather
than panicking or recreating a deleted directory.

### The dev container's own paths are invisible to Docker
The Docker socket is the **host** engine, so every `-v` source is resolved on the
host, not inside this container. Bind-mounting a container-only path (`/tmp/...`,
a scratch dir) silently creates an *empty* directory on the host and mounts that
— the container comes up, the app runs, and every listing is empty with no error
anywhere. Test data has to live under a real host path: `/d/workspace/...`
(`D:\workspace\...` on the host), e.g. `/d/workspace/tmp-claude/`.

The same rule bit `dev/build.sh`: it originally mounted only `web/`, but vite's
`outDir` is `../internal/ui/dist`, which then resolved *outside* the mount. vite
reported writing the files, they went into the container's own filesystem, and
`go build` embedded the stale placeholder `index.html`. Mount the repo root and
set the workdir to `web/`.

### Browsers throttle programmatic downloads
Multi-select download fires one `<a download>` click per file. Chrome shows an
"allow multiple downloads?" prompt once per origin — a user who dismisses it sees
nothing happen, with no error to catch — and Safari drops downloads issued too
fast. Space them out, cap the batch, and surface a count so the user knows how
many to expect. Also remember the `download` attribute cannot carry a path: the
files land flat, and the UI has to say so rather than let the user assume
otherwise.

### Timezone
`TZ` affects only display. Send RFC 3339 UTC timestamps over the API and format
in the browser — a Go binary formatting in `TZ` and a browser in another timezone
will disagree, and the bug reports are miserable to read.

---

## Testing

**`internal/vfs` is where the test effort goes.** Table-driven, against a temp
tree, and it should include at minimum: `..` in every position; an absolute path;
a path with a NUL byte; a symlink to `/etc`; a symlink to a path inside the root
(must work); a symlink chain that leaves and re-enters; a path that resolves into
`STATE_DIR`; a name that is exactly `.`; invalid UTF-8; a very long path; and a
Windows-reserved name (harmless on Linux, but cheap).

`internal/ownership` deserves tests that assert the asymmetry explicitly — a
created file gets `PUID:PGID`, an **overwritten** file keeps its original uid,
gid and mode. That rule is the one a future refactor will quietly flatten. The
test needs root to chown to an arbitrary uid, which the `golang:1.25` container
gives.

`internal/jobs` should have a cancel test and a conflict-policy test per policy.

The frontend gets `svelte-check` in CI and no unit tests for v1; the value is in
the Go half.

End-to-end, test on a real box — holyhorse (`ssh admin@185.216.75.105`) and
wisera (`ssh admin@80.241.218.30`) are test PCS boxes with no customer data and
deploys there are pre-authorised. A dev container with a synthetic `./DATA` will
not reproduce the `AppData` ownership mess, which is exactly the thing the root
decision exists for.

---

## Decisions

| Decision | Why | What was rejected |
|---|---|---|
| Go + Svelte 5, Maison's shape | Same language, router, embed trick and publish path as `packages/maison`, and `tokens.css` is already a verbatim port of CasaOS-UI's SCSS variables — CasaOS visual parity for free. | Node/TS (a runtime in the image for no gain here); forking FileBrowser (inherits a bolt DB and a user model baked into every handler, which the scope exists to delete). |
| `ghcr.io/yundera/files`, tagged releases | Matches Maison. | dockflow → Scaleway, which is the TypeScript services' path and would mean a version file to bump. |
| Trash, visible, in the app folder | User's call. Delete is the one irreversible action in the app and users will use it on things they care about. | Permanent delete with a confirm (CasaOS and FileBrowser behaviour). |
| Per-item `meta.json` | No contention between concurrent deletes; a corrupt file costs one item's restore path, not the trash. | A single `index.json`, which needs a lock and fails whole. |
| Run as root, chown on create | `PUID` cannot read `AppData/hubs/pgdata` (uid 70, `0700`) or `AppData/duplicati/config` (root, `0700`), so a large part of the tree the app exists to manage would be invisible. Docker has no ambient caps, so `--user 1000 --cap-add DAC_READ_SEARCH` grants nothing — verified on a real box. | `user: $PUID:$PGID`, which is what the FileBrowser app does and is why its coverage of `AppData` is poor. |
| Explicit `DIR_MODE`/`FILE_MODE` | A umask can only clear bits, so under the usual `022` it cannot produce the group-writable result that lets a container running as another uid in the same group write to an uploaded file. | `UMASK`, which reads simpler and cannot express the requirement. |
| Overwrites preserve uid/gid/mode | Editing an app's `config.yaml` must not re-home it to `PUID` and break the app that owns it. | Uniform chown, which is simpler and wrong. |
| tus for upload | What FileBrowser uses, a real spec, and `tus-js-client` means the browser half is not ours to invent or debug. | A hand-rolled `Content-Range` scheme. |
| Background jobs over a WebSocket | A 50 GB copy is not a request, and CasaOS's UI already assumes an operation status bar with per-item progress and cancel. WS rather than SSE because maison's `internal/live/hub.go` already does exactly this and can be copied whole. | SSE (an earlier draft of ARCHITECTURE.md said SSE); synchronous requests with a spinner. |
| `{"error": "sentence"}` + HTTP status | The shape maison uses at every call site. The status is the machine-readable half — 409 collision, 403 permission, 413 too large, 422 bad YAML — so a nested code object would only duplicate it. | A `{code, message}` object, which the first draft of ARCHITECTURE.md specified. |
| Malformed config warns and falls back | Matches `config.FromEnv` in maison, which never returns an error: *"a dashboard that will not start is a worse outcome"*. One typo in a compose file should not stop the file manager booting. | A fatal startup error, which an earlier draft of this document specified. |
| Jobs in memory | Persisting a queue means a database, which the app deliberately does not have. | A bolt/SQLite job store. |
| YAML errors block the save | This app will be used to edit compose files on a PCS. Finding out a file is broken when a stack fails to come up is worse than one round trip. | A non-blocking warning. |
| `baseMtime` check on save | Last-writer-wins on a file another app also writes is data loss users never trace back. | No check. |
| Ignore forwarded identity in v1 | One owner per PCS, no per-user state to attach it to. | Parsing `Remote-User` now, which would be the *forgeable* half of AppShield's identity story and would need replacing with the signed assertion anyway. |
| Multi-file download, no archiver | Dropping zip removes a streaming archiver, a compression policy, Zip64, and a download with no `Content-Length`. The cost is that folders cannot be downloaded and selections arrive flat — accepted, with SMB as the answer for bulk or structured transfer. | Streamed zip; the File System Access API, which restores both but is Chromium-desktop only. |
| Warn on delete, don't hide `AppData` | Hiding it contradicts the reason the container runs as root. Trash and the backup system already soften a mistake; past that, deleting your own database is your call. | A hidden-files gate or a blocking confirmation on every `AppData` action. |
| Smallest Markdown renderer that works | No need to match Maison's flavour — it was written for app descriptions, not documents. `marked` plus `DOMPurify`, because a `.md` file in the tree can contain raw HTML and the preview renders on the app's own origin. | Porting `maison/web/src/lib/markdown.ts`. |
| JPEG thumbnails | Pure-Go WebP **encoding** is not well served; decode-only is. No cgo in the image. | WebP out (smaller, and what a fresh design would pick). |

---

## Resolved

All five questions from the first draft are settled:

1. **App id and routes** — `files-${APP_DOMAIN}`, containers `files` (gate) and
   `files-backend`.
2. **FileBrowser migration** — the two coexist and Files keeps no state
   FileBrowser would collide with. No install tip, no uninstall prompt, no
   grace period for its share links. An already-installed FileBrowser is left
   alone and whatever happens inside it is out of scope.
3. **`AppData` visibility** — shown normally. A clear warning on delete, and
   nothing more.
4. **Markdown preview** — the easiest thing that renders, sanitised.
5. **Zip download** — dropped entirely in favour of multi-file download.

## Still open

Nothing blocking. The two worth revisiting after the app is on a real box:

- Whether folder download is missed enough to justify the File System Access
  API as a Chromium-only progressive enhancement.
- Whether video thumbnails are missed enough to justify ffmpeg in the image.
