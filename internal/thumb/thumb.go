// Package thumb generates and caches image thumbnails.
//
// Pure Go, no cgo: image/jpeg, image/png, image/gif from the standard library
// plus golang.org/x/image for webp and bmp. Formats with no pure-Go decoder —
// HEIC, AVIF, camera RAW — get no thumbnail and the UI falls back to a kind
// icon, which is the correct degradation rather than a broken image.
//
// Output is JPEG rather than WebP because pure-Go WebP ENCODING is not well
// served (x/image/webp is decode-only), and adding cgo to get it would cost the
// static binary.
package thumb

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"sync"
	"time"

	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/ownership"
	"github.com/yundera/files/internal/vfs"
)

// ErrUnsupported means the format has no pure-Go decoder.
var ErrUnsupported = errors.New("no thumbnail for this format")

// Widths the cache will generate. A fixed set bounds how many variants of one
// image can accumulate; an arbitrary ?w= would let a client fill the disk with
// near-identical thumbnails.
var Widths = []int{128, 256, 512}

const (
	// maxPixels caps the DECODED size of a source image.
	//
	// This is the decompression-bomb guard and it is checked with DecodeConfig
	// BEFORE Decode, because a 200 KB PNG can legally declare 40000x40000 and
	// decoding it allocates ~6 GB. 24 megapixels covers every consumer camera
	// while bounding one decode to roughly maxPixels*4 bytes (~96 MB for RGBA).
	//
	// Peak memory is that times maxConcurrent, so the app's container memory
	// limit has to exceed ~200 MB for the thumbnailer alone. See docker-compose.
	maxPixels = 24_000_000

	// maxConcurrent bounds simultaneous decodes. Deliberately small: the limit
	// that matters is memory, not CPU, and a folder of 4000 photos would
	// otherwise try to decode as many as there are cores.
	maxConcurrent = 2

	quality = 80
)

// Cache generates thumbnails on demand and keeps them on disk.
type Cache struct {
	fs    *vfs.FS
	cfg   config.Config
	owner ownership.Owner

	sem chan struct{}

	// evictMu serialises eviction so two concurrent writes cannot both decide to
	// delete the same files.
	evictMu sync.Mutex
}

func New(f *vfs.FS, cfg config.Config) *Cache {
	return &Cache{
		fs:    f,
		cfg:   cfg,
		owner: ownership.FromConfig(cfg),
		sem:   make(chan struct{}, maxConcurrent),
	}
}

// SnapWidth maps a requested width onto the nearest supported one.
func SnapWidth(requested int) int {
	best := Widths[0]
	for _, w := range Widths {
		if abs(w-requested) < abs(best-requested) {
			best = w
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (c *Cache) dirRel() string { return path.Join(c.fs.StateRel(), "thumbs") }

// key identifies one thumbnail.
//
// Keyed on mtime and size as well as path, so an edited file simply gets a new
// key. That is why there is no invalidation logic anywhere in this package: a
// stale entry is unreachable rather than wrong, and the LRU eventually reclaims
// it.
func key(rel string, modTime time.Time, size int64, width int) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d|%d", rel, modTime.UnixNano(), size, width)
	return hex.EncodeToString(h.Sum(nil))
}

// Get returns a JPEG thumbnail, generating it if it is not cached.
func (c *Cache) Get(virtual string, width int) ([]byte, error) {
	rel, err := c.fs.Clean(virtual)
	if err != nil {
		return nil, err
	}
	root := c.fs.Root()
	fi, err := root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, ErrUnsupported
	}

	width = SnapWidth(width)
	cacheRel := path.Join(c.dirRel(), key(rel, fi.ModTime(), fi.Size(), width)+".jpg")

	if b, err := root.ReadFile(cacheRel); err == nil {
		// Touch on read so mtime doubles as the last-access time. That is what
		// makes the eviction below a real LRU without a second index to keep in
		// sync with the files.
		now := time.Now()
		_ = root.Chtimes(cacheRel, now, now)
		return b, nil
	}

	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	// Another request may have generated it while we waited for the semaphore.
	if b, err := root.ReadFile(cacheRel); err == nil {
		return b, nil
	}

	b, err := c.generate(rel, width)
	if err != nil {
		return nil, err
	}
	c.store(cacheRel, b)
	return b, nil
}

func (c *Cache) generate(rel string, width int) ([]byte, error) {
	f, err := c.fs.Root().Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// DecodeConfig reads only the header. This is the bomb guard: it must happen
	// before Decode, or the allocation it is meant to prevent has already
	// happened.
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, ErrUnsupported
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, fmt.Errorf("%w: image is %dx%d, over the %d megapixel limit",
			ErrUnsupported, cfg.Width, cfg.Height, maxPixels/1_000_000)
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}

	// Never upscale: a 64px icon rendered at 512 is blurry and bigger than the
	// original.
	b := src.Bounds()
	outW, outH := width, int(float64(width)*float64(b.Dy())/float64(b.Dx()))
	if b.Dx() <= width {
		outW, outH = b.Dx(), b.Dy()
	}
	if outH < 1 {
		outH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
	// CatmullRom is the best-looking of x/image/draw's kernels and the cost is
	// irrelevant at thumbnail sizes.
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// store writes a thumbnail and enforces the cache cap.
//
// A failure here is logged and swallowed: the caller already has the bytes, and
// an unwritable cache should make thumbnails slow, not broken.
func (c *Cache) store(cacheRel string, b []byte) {
	root := c.fs.Root()
	if err := root.MkdirAll(path.Dir(cacheRel), c.owner.DirMode); err != nil {
		log.Printf("thumb: create cache dir: %v", err)
		return
	}
	tmp := cacheRel + ".partial"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, c.owner.FileMode)
	if err != nil {
		log.Printf("thumb: write cache: %v", err)
		return
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = root.Remove(tmp)
		return
	}
	c.owner.ApplyFile(f)
	_ = f.Close()
	if err := root.Rename(tmp, cacheRel); err != nil {
		_ = root.Remove(tmp)
		return
	}
	c.evict()
}

// evict removes least-recently-used entries until the cache is under its cap.
func (c *Cache) evict() {
	if c.cfg.ThumbCacheBytes <= 0 {
		return
	}
	c.evictMu.Lock()
	defer c.evictMu.Unlock()

	root := c.fs.Root()
	d, err := root.Open(c.dirRel())
	if err != nil {
		return
	}
	names, err := d.Readdirnames(-1)
	_ = d.Close()
	if err != nil {
		return
	}

	type entry struct {
		name string
		size int64
		used time.Time
	}
	items := make([]entry, 0, len(names))
	var total int64
	for _, n := range names {
		fi, err := root.Lstat(path.Join(c.dirRel(), n))
		if err != nil {
			continue
		}
		items = append(items, entry{n, fi.Size(), fi.ModTime()})
		total += fi.Size()
	}
	if total <= c.cfg.ThumbCacheBytes {
		return
	}

	// Oldest access first — mtime is kept current by the touch in Get.
	sort.Slice(items, func(i, j int) bool { return items[i].used.Before(items[j].used) })

	// Always keep the most recently used entry, even if it alone exceeds the
	// cap. Without this floor a cap smaller than one thumbnail evicts the
	// thumbnail that was just generated, so the next request regenerates and
	// evicts it again — a thrash loop that burns CPU forever and never caches
	// anything. A cap that small is a misconfiguration; degrading to "cache one"
	// is the honest response to it.
	for _, it := range items[:len(items)-1] {
		if total <= c.cfg.ThumbCacheBytes {
			break
		}
		if err := root.Remove(path.Join(c.dirRel(), it.name)); err == nil {
			total -= it.size
		}
	}
}

// Etag is a cache validator for a thumbnail, so a browser that already has one
// gets a 304 instead of the bytes.
func Etag(rel string, modTime time.Time, size int64, width int) string {
	return `"` + key(rel, modTime, size, width)[:16] + `"`
}

var _ = strconv.Itoa
var _ fs.FileInfo
