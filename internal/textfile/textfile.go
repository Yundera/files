// Package textfile reads and writes the files the built-in editor can open.
//
// Everything here is a guard. A file manager's editor is the one place where a
// wrong answer silently destroys data: opening a JPEG as text and saving it
// rewrites it as mojibake, and a last-writer-wins save on a file another app
// also writes is a loss the user never traces back. So the rules are strict and
// each one refuses rather than guesses.
package textfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/ownership"
	"github.com/yundera/files/internal/vfs"
)

var (
	// ErrTooLarge is a file over EDIT_MAX_BYTES.
	ErrTooLarge = errors.New("file is too large to edit")
	// ErrBinary is a file that is not text.
	ErrBinary = errors.New("file is not text")
	// ErrStale means the file changed since the client loaded it.
	ErrStale = errors.New("the file changed on disk since you opened it")
	// ErrSyntax is a YAML document that does not parse.
	ErrSyntax = errors.New("syntax error")
)

// Doc is a file opened for editing.
type Doc struct {
	Path    string    `json:"path"`
	Content string    `json:"content"`
	ModTime time.Time `json:"modTime"`
	Size    int64     `json:"size"`
	// Language names a CodeMirror mode. Empty means plain text.
	Language string `json:"language"`
	// ReadOnly is set when the file can be shown but not saved.
	ReadOnly bool `json:"readOnly,omitempty"`
}

// sniffLen is how much of a file is examined for binary content. 8 KiB is what
// git uses, and it is enough to catch every real binary format's header.
const sniffLen = 8 << 10

// Read opens a file for editing, refusing anything the editor would damage.
func Read(f *vfs.FS, cfg config.Config, virtual string) (*Doc, error) {
	fh, err := f.Open(virtual)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	fi, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, errors.New("that is a folder")
	}
	// Check the size BEFORE reading: the point of the cap is not to allocate
	// hundreds of megabytes to then decide against it.
	if fi.Size() > cfg.EditMaxBytes {
		return nil, fmt.Errorf("%w: %s is %d bytes, the limit is %d",
			ErrTooLarge, path.Base(virtual), fi.Size(), cfg.EditMaxBytes)
	}

	b, err := io.ReadAll(fh)
	if err != nil {
		return nil, err
	}
	if isBinary(b) {
		return nil, fmt.Errorf("%w: %s looks like binary data", ErrBinary, path.Base(virtual))
	}

	return &Doc{
		Path:     virtual,
		Content:  string(b),
		ModTime:  fi.ModTime().UTC(),
		Size:     fi.Size(),
		Language: Language(path.Base(virtual)),
	}, nil
}

// isBinary reports whether b should be refused by the editor.
//
// Two signals, both cheap: a NUL byte in the first 8 KiB (no text encoding this
// editor supports produces one) and invalid UTF-8 anywhere. The second matters
// because round-tripping a latin-1 file through a UTF-8 editor would rewrite
// every high byte as U+FFFD and destroy the original on save.
func isBinary(b []byte) bool {
	head := b
	if len(head) > sniffLen {
		head = head[:sniffLen]
	}
	for _, c := range head {
		if c == 0 {
			return true
		}
	}
	return !utf8.Valid(b)
}

// Write saves content atomically.
//
// baseMtime is the modification time the client saw when it loaded the file. If
// the file has changed since, the write is refused rather than applied — the
// alternative is silent data loss on a file that another app also writes, which
// is exactly the case on a PCS where the editor is used on app config.
func Write(f *vfs.FS, owner ownership.Owner, virtual, content string, baseMtime time.Time) (time.Time, error) {
	rel, err := f.Clean(virtual)
	if err != nil {
		return time.Time{}, err
	}
	root := f.Root()

	// Validate before touching the disk. A compose file that does not parse
	// should never reach the filesystem: on a PCS this editor is used on app
	// config, and finding out at `docker compose up` is far worse than one
	// round trip.
	if err := Validate(path.Base(rel), content); err != nil {
		return time.Time{}, err
	}

	var saved ownership.Preserved
	if fi, err := root.Lstat(rel); err == nil {
		if !baseMtime.IsZero() && !fi.ModTime().UTC().Truncate(time.Second).Equal(baseMtime.UTC().Truncate(time.Second)) {
			return time.Time{}, fmt.Errorf("%w (it was modified %s)", ErrStale, fi.ModTime().UTC().Format(time.RFC3339))
		}
		// Capture before the overwrite: editing an app's config must not
		// re-home it to PUID and break the app that owns it.
		saved = ownership.Capture(fi)
	}

	// The temp file goes in the SAME directory as the target, so the rename that
	// follows stays within one filesystem and is therefore atomic.
	tmpRel := path.Join(path.Dir(rel), "."+path.Base(rel)+".tmp")
	tmp, err := root.OpenFile(tmpRel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, owner.FileMode)
	if err != nil {
		return time.Time{}, err
	}
	cleanup := func() { _ = root.Remove(tmpRel) }

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return time.Time{}, err
	}
	if !saved.Restore(tmp) {
		owner.ApplyFile(tmp)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return time.Time{}, err
	}
	if err := root.Rename(tmpRel, rel); err != nil {
		cleanup()
		return time.Time{}, err
	}

	fi, err := root.Lstat(rel)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime().UTC(), nil
}

// yamlLine pulls the line number out of a go-yaml error message, which is
// formatted "yaml: line N: ...". Returned so the editor can put the cursor on
// the offending line instead of making the user hunt for it.
var yamlLine = regexp.MustCompile(`^line (\d+): `)

// SyntaxError carries a parse failure with its position.
type SyntaxError struct {
	Message string
	Line    int
}

func (e *SyntaxError) Error() string { return e.Message }
func (e *SyntaxError) Unwrap() error { return ErrSyntax }

// Validate checks content that has a format we can check. Today that is YAML;
// everything else is accepted as-is.
//
// Only YAML, deliberately. It is the format this app is most likely to be used
// on (compose files, app config) and the one where a syntax error surfaces
// late and confusingly. JSON would be easy to add but a broken .json usually
// fails loudly and immediately where it is read.
func Validate(name, content string) error {
	ext := strings.ToLower(path.Ext(name))
	if ext != ".yml" && ext != ".yaml" {
		return nil
	}
	var out any
	if err := yaml.Unmarshal([]byte(content), &out); err != nil {
		msg := strings.TrimPrefix(err.Error(), "yaml: ")
		se := &SyntaxError{Message: msg}
		// go-yaml formats the position into the message ("line 2: ..."). The
		// line is returned as its own field so the editor can jump to it, so
		// strip it from the text too — otherwise the UI renders "Line 2: line
		// 2: ...".
		if m := yamlLine.FindStringSubmatch(msg); len(m) == 2 {
			se.Line, _ = strconv.Atoi(m[1])
			se.Message = strings.TrimPrefix(msg, m[0])
		}
		return se
	}
	return nil
}

// Language maps a filename to a CodeMirror mode name.
//
// The set is narrow on purpose: each mode is a bundle the browser has to
// download, and the formats people actually edit on a PCS are configuration and
// scripts, not application source.
func Language(name string) string {
	lower := strings.ToLower(name)
	switch lower {
	case "dockerfile", "makefile", "caddyfile":
		return "text"
	}
	switch strings.TrimPrefix(path.Ext(lower), ".") {
	case "yml", "yaml":
		return "yaml"
	case "json", "jsonld":
		return "json"
	case "md", "markdown":
		return "markdown"
	case "html", "htm", "xml", "svg":
		return "html"
	case "css", "scss", "less":
		return "css"
	case "js", "mjs", "cjs", "ts", "jsx", "tsx":
		return "javascript"
	case "sh", "bash", "zsh", "env", "conf", "ini", "cfg", "toml", "service":
		return "text"
	default:
		return "text"
	}
}
