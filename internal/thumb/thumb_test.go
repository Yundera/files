package thumb

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/vfs"
)

func newCache(t *testing.T, cacheBytes int64) (*Cache, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{
		DataRoot: root, PUID: -1, PGID: -1, DirMode: 0o755, FileMode: 0o644,
		ThumbCacheBytes: cacheBytes,
	}
	f, err := vfs.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return New(f, cfg), root
}

// writePNG makes a real w x h PNG.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestThumbnailIsGeneratedAndScaled(t *testing.T) {
	c, root := newCache(t, 10<<20)
	writePNG(t, filepath.Join(root, "big.png"), 800, 600)

	b, err := c.Get("/big.png", 256)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("output is not a decodable JPEG: %v", err)
	}
	if got := img.Bounds().Dx(); got != 256 {
		t.Errorf("thumbnail width = %d; want 256", got)
	}
	// Aspect ratio preserved: 800x600 -> 256x192.
	if got := img.Bounds().Dy(); got != 192 {
		t.Errorf("thumbnail height = %d; want 192 (aspect preserved)", got)
	}
}

// A small image must not be blown up: an upscaled thumbnail is blurry and
// bigger than the file it stands in for.
func TestSmallImagesAreNotUpscaled(t *testing.T) {
	c, root := newCache(t, 10<<20)
	writePNG(t, filepath.Join(root, "tiny.png"), 48, 32)
	b, err := c.Get("/tiny.png", 512)
	if err != nil {
		t.Fatal(err)
	}
	img, _ := jpeg.Decode(bytes.NewReader(b))
	if img.Bounds().Dx() != 48 || img.Bounds().Dy() != 32 {
		t.Errorf("thumbnail is %v; want the original 48x32", img.Bounds().Size())
	}
}

// THE DECOMPRESSION BOMB GUARD. A tiny PNG can legally declare enormous
// dimensions; decoding it would allocate gigabytes. The header must be checked
// with DecodeConfig and the image refused BEFORE Decode runs.
func TestDecompressionBombIsRefusedWithoutDecoding(t *testing.T) {
	c, root := newCache(t, 10<<20)

	// A valid PNG header declaring 40000x40000 (1.6 gigapixels) with a tiny
	// IDAT. Hand-built: no encoder will produce this.
	chunk := func(typ string, data []byte) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
		payload := append([]byte(typ), data...)
		b.Write(payload)
		_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(payload))
		return b.Bytes()
	}
	var ihdr bytes.Buffer
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(40000))
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(40000))
	ihdr.Write([]byte{8, 2, 0, 0, 0}) // 8-bit RGB

	var idat bytes.Buffer
	zw := zlib.NewWriter(&idat)
	_, _ = zw.Write(make([]byte, 1024))
	_ = zw.Close()

	var bomb bytes.Buffer
	bomb.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	bomb.Write(chunk("IHDR", ihdr.Bytes()))
	bomb.Write(chunk("IDAT", idat.Bytes()))
	bomb.Write(chunk("IEND", nil))

	if err := os.WriteFile(filepath.Join(root, "bomb.png"), bomb.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := c.Get("/bomb.png", 256)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Get on a 1.6-gigapixel image = %v; want ErrUnsupported", err)
	}
}

// A format with no pure-Go decoder is refused cleanly, so the grid falls back to
// a kind icon rather than showing a broken image.
func TestUndecodableFilesAreRefused(t *testing.T) {
	c, root := newCache(t, 10<<20)
	for name, body := range map[string][]byte{
		"fake.png":   []byte("not really a png at all"),
		"photo.heic": {0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'},
		"empty.jpg":  {},
	} {
		if err := os.WriteFile(filepath.Join(root, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Get("/"+name, 256); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Get(%s) = %v; want ErrUnsupported", name, err)
		}
	}
	// A directory is not an image either.
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("/adir", 256); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Get on a directory = %v; want ErrUnsupported", err)
	}
}

// The second request must come from the cache, and editing the source must
// produce a different thumbnail without any invalidation logic — the key folds
// in mtime and size.
func TestCacheHitsAndKeyChangesWithTheSource(t *testing.T) {
	c, root := newCache(t, 10<<20)
	p := filepath.Join(root, "img.png")
	writePNG(t, p, 400, 400)

	first, err := c.Get("/img.png", 128)
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(root, "AppData", "files", "thumbs")
	entries, _ := os.ReadDir(cacheDir)
	if len(entries) != 1 {
		t.Fatalf("cache holds %d files after one request; want 1", len(entries))
	}

	if _, err := c.Get("/img.png", 128); err != nil {
		t.Fatal(err)
	}
	if entries, _ = os.ReadDir(cacheDir); len(entries) != 1 {
		t.Errorf("a repeat request added a cache entry; want a hit")
	}

	// Change the source: different dimensions, so different bytes and mtime.
	writePNG(t, p, 200, 100)
	second, err := c.Get("/img.png", 128)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Error("editing the source returned the old thumbnail; the key must fold in mtime and size")
	}
	if entries, _ = os.ReadDir(cacheDir); len(entries) != 2 {
		t.Errorf("cache holds %d files; want 2 (old entry is unreachable, not overwritten)", len(entries))
	}
}

// The cache is capped. Without eviction a media library fills the disk with
// thumbnails.
func TestCacheEvictsWhenOverCap(t *testing.T) {
	// A cap small enough that a handful of thumbnails exceeds it.
	c, root := newCache(t, 4096)
	for i := 0; i < 8; i++ {
		name := "img" + string(rune('a'+i)) + ".png"
		writePNG(t, filepath.Join(root, name), 600, 600)
		if _, err := c.Get("/"+name, 256); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(root, "AppData", "files", "thumbs"))
	var total int64
	for _, e := range entries {
		fi, _ := e.Info()
		total += fi.Size()
	}
	// Eight 600x600 thumbnails against a 4096-byte cap: everything but the most
	// recent must go. Keeping one is deliberate — see the floor in evict().
	if len(entries) != 1 {
		t.Errorf("cache holds %d entries (%d bytes) against a 4096 cap; want exactly 1 — "+
			"evict must stop at the most recent rather than emptying the cache", len(entries), total)
	}
}

func TestWidthsSnapToTheSupportedSet(t *testing.T) {
	for in, want := range map[int]int{
		1: 128, 128: 128, 150: 128, 200: 256, 256: 256, 400: 512, 512: 512, 9999: 512,
	} {
		if got := SnapWidth(in); got != want {
			t.Errorf("SnapWidth(%d) = %d; want %d", in, got, want)
		}
	}
}

// The state directory is not browsable, so it must not be thumbnailable either.
func TestThumbRefusesTheStateDir(t *testing.T) {
	c, _ := newCache(t, 10<<20)
	if _, err := c.Get("/AppData/files/trash/x.png", 256); err == nil {
		t.Error("Get succeeded on a path inside the state directory")
	}
	if _, err := c.Get("/../etc/passwd", 256); err == nil {
		t.Error("Get succeeded on a traversing path")
	}
}
