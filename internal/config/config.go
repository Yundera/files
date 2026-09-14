// Package config turns the process environment into a Config value.
//
// Policy, matching packages/maison: FromEnv never fails. A malformed value falls
// back to its default and logs a warning rather than aborting the boot — a file
// manager that will not start is a worse outcome for the user than one running
// with a default mode. The warning is what makes it non-silent; grep the logs
// when ownership looks wrong.
package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is passed by value. Nil/zero fields are tolerated everywhere, which is
// what makes config.Config{DataRoot: t.TempDir()} a complete test fixture.
type Config struct {
	// Addr is the listen address. The container always listens on 8080; the
	// compose maps the host port.
	Addr string

	// DataRoot is the tree this app serves, as seen INSIDE the container.
	// Everything below it is browsable and nothing above it is reachable —
	// internal/vfs opens it with os.OpenRoot and every user path is resolved
	// against that root.
	DataRoot string

	// StateDirPath overrides StateDir(). Empty means "use the default".
	//
	// The default lives INSIDE DataRoot, which is load-bearing: it makes
	// delete-to-trash a single os.Root.Rename within one root. A path outside
	// DataRoot would need a cross-root rename, which os.Root does not offer.
	// See IMPLEMENTATION-NOTES.md.
	StateDirPath string

	// PUID/PGID own every file and directory this app creates.
	//
	// Ints, not strings as in maison: maison's flow into compose interpolation,
	// ours flow into chown(2). A negative value means "leave ownership alone".
	PUID int
	PGID int

	// DirMode/FileMode are applied explicitly to created inodes rather than via a
	// umask. A umask can only CLEAR bits, so under the usual 022 it could never
	// produce the group-writable result that lets a container running as another
	// uid in the same group write to an uploaded file.
	DirMode  os.FileMode
	FileMode os.FileMode

	// TZ affects display only. The API speaks RFC 3339 UTC and the browser
	// formats; see IMPLEMENTATION-NOTES.md "Timezone".
	TZ string

	// TrashRetention purges trashed items older than this. Zero disables it.
	TrashRetention time.Duration
	// TrashMaxBytes purges oldest-first once the trash exceeds this. Zero disables it.
	TrashMaxBytes int64

	// ThumbCacheBytes caps the thumbnail cache; eviction is least-recently-used.
	ThumbCacheBytes int64

	// EditMaxBytes is the largest file the text editor will open.
	EditMaxBytes int64
	// UploadMaxBytes is the largest single upload. Zero is unlimited.
	UploadMaxBytes int64

	// SearchMaxDepth and SearchTimeout bound a subtree name search. Whichever is
	// hit first ends the walk and the response is marked truncated.
	SearchMaxDepth int
	SearchTimeout  time.Duration
}

// FromEnv reads the environment. Every default in README.md's configuration
// table is expressed here and nowhere else.
func FromEnv() Config {
	dataRoot := envOr("DATA_ROOT", "/DATA")
	return Config{
		Addr:         envOr("HTTP_ADDR", ":8080"),
		DataRoot:     dataRoot,
		StateDirPath: os.Getenv("STATE_DIR"),

		PUID:     envInt("PUID", 1000),
		PGID:     envInt("PGID", 1000),
		DirMode:  envMode("DIR_MODE", 0o775),
		FileMode: envMode("FILE_MODE", 0o664),
		TZ:       os.Getenv("TZ"),

		TrashRetention: time.Duration(envInt("TRASH_RETENTION_DAYS", 30)) * 24 * time.Hour,
		TrashMaxBytes:  int64(envInt("TRASH_MAX_GB", 0)) << 30,

		ThumbCacheBytes: int64(envInt("THUMB_CACHE_MB", 512)) << 20,

		EditMaxBytes:   int64(envInt("EDIT_MAX_BYTES", 2*1024*1024)),
		UploadMaxBytes: int64(envInt("UPLOAD_MAX_BYTES", 0)),

		SearchMaxDepth: envInt("SEARCH_MAX_DEPTH", 8),
		SearchTimeout:  time.Duration(envInt("SEARCH_TIMEOUT_MS", 2000)) * time.Millisecond,
	}
}

// StateDir is where everything this app owns lives: the trash, the thumbnail
// cache and the upload staging area.
//
// internal/vfs filters this path out of every listing and refuses it as a
// mutation target. That is not cosmetic — without it a user can browse into
// their own trash, delete a trashed item into the trash again, and drop files
// into the tus staging area that the GC then eats.
func (c Config) StateDir() string {
	if c.StateDirPath != "" {
		return c.StateDirPath
	}
	return filepath.Join(c.DataRoot, "AppData", "files")
}

// TrashDir holds one directory per trashed item: meta.json plus the payload.
func (c Config) TrashDir() string { return filepath.Join(c.StateDir(), "trash") }

// ThumbDir holds the generated thumbnail cache, keyed by path+mtime+size+width.
func (c Config) ThumbDir() string { return filepath.Join(c.StateDir(), "thumbs") }

// UploadDir holds in-flight tus uploads until they are committed by rename.
func (c Config) UploadDir() string { return filepath.Join(c.StateDir(), "uploads") }

// StateDirInsideRoot reports whether StateDir lies within DataRoot.
//
// When it does (the default), trash, upload-commit and atomic save are all a
// single rename within one os.Root. When it does not, those become cross-root
// operations that os.Root cannot express. The server refuses to start in that
// case rather than carry two code paths for a flexibility nobody has asked for.
func (c Config) StateDirInsideRoot() bool {
	rel, err := filepath.Rel(filepath.Clean(c.DataRoot), filepath.Clean(c.StateDir()))
	if err != nil {
		return false
	}
	// Rel yields ".." or a "../"-prefixed path exactly when the target escapes
	// the base. "." means StateDir IS DataRoot, which is also refused: the state
	// dir has to be a subdirectory we can hide from listings.
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt parses a base-10 int, warning and falling back on anything unparseable
// or negative. Negative is rejected rather than passed through because every
// caller here is a size, a count or a duration where it is meaningless.
func envInt(key string, def int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		log.Printf("config: %s=%q is not a non-negative integer, using %d", key, raw, def)
		return def
	}
	return n
}

// envMode parses an octal file mode, with or without a leading 0 or 0o.
//
// Accepting a bare "775" matters: YAML reads an unquoted 0644 as octal and
// strips the leading zero, so a compose file that means 0644 can hand us "644".
// All three spellings mean the same thing here. ParseUint with an explicit base
// of 8 tolerates a redundant leading zero but NOT a "0o" prefix, so that prefix
// is stripped first.
func envMode(key string, def os.FileMode) os.FileMode {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	digits := raw
	if len(digits) > 2 && digits[0] == '0' && (digits[1] == 'o' || digits[1] == 'O') {
		digits = digits[2:]
	}
	n, err := strconv.ParseUint(digits, 8, 32)
	if err != nil || n == 0 || n > 0o7777 {
		log.Printf("config: %s=%q is not an octal mode, using %#o", key, raw, def)
		return def
	}
	return os.FileMode(n)
}
