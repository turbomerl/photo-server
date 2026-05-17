// Package convert produces browser-viewable gallery JPEGs from
// uploaded originals (kgu.12). It shells out to the `vipsthumbnail`
// CLI rather than linking libvips via cgo: the appliance stays a
// single static binary, and a libvips fault on a malformed HEIC dies
// in the child process, not the server (PRD N8 "all-day rock-solid").
//
// The original file is only ever read, never modified. Conversion runs
// off the upload hot path via Pool (PRD R4/R5).
package convert

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/turbomerl/photo-server/internal/blobstore"
)

// Converter renders a stored original to a gallery JPEG via
// vipsthumbnail.
type Converter struct {
	bin     string // resolved absolute path to vipsthumbnail
	blobs   *blobstore.Store
	maxPx   int
	quality int
	log     *slog.Logger
}

// NewConverter resolves bin on PATH (fail fast if libvips tooling is
// missing — kgu.2 verified it is installed). maxPx caps the longest
// edge; the original full-resolution file is preserved untouched and
// downloaded separately (kgu.18).
func NewConverter(bin string, blobs *blobstore.Store, maxPx, quality int, log *slog.Logger) (*Converter, error) {
	p, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("convert: %q not found on PATH: %w", bin, err)
	}
	return &Converter{bin: p, blobs: blobs, maxPx: maxPx, quality: quality, log: log}, nil
}

// GalleryJPEG writes gallery_jpegs/<hash>.jpg from the stored original
// (hash, ext). Idempotent: if the gallery JPEG already exists it is a
// no-op (the rendition is content-addressed by the original's hash).
func (c *Converter) GalleryJPEG(ctx context.Context, hash, ext string) error {
	if c.blobs.Exists(blobstore.Gallery, hash, "") {
		return nil
	}
	src := c.blobs.Path(blobstore.Original, hash, ext)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("convert: original %s missing: %w", hash, err)
	}

	tmp, err := os.CreateTemp("", "ps-gallery-*.jpg")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpName)

	// vipsthumbnail downscales only (never upscales), auto-rotates
	// from EXIF orientation, and `strip` drops metadata from the
	// derived JPEG (privacy N2; the original keeps its EXIF).
	out := fmt.Sprintf("%s[Q=%d,strip]", tmpName, c.quality)
	cmd := exec.CommandContext(ctx, c.bin,
		src,
		"--size", fmt.Sprintf("%dx%d", c.maxPx, c.maxPx),
		"-o", out,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("convert %s: vipsthumbnail: %v: %s", hash, err, stderr.String())
	}

	f, err := os.Open(tmpName)
	if err != nil {
		return err
	}
	defer f.Close()
	// blobstore.Put is atomic + fsync-durable into gallery_jpegs/.
	return c.blobs.Put(blobstore.Gallery, hash, "", f)
}
