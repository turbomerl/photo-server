// Package convert produces browser renditions of uploaded originals:
// a gallery JPEG for HEIC (kgu.12) and a webp grid thumbnail for every
// photo (kgu.13). It shells out to the `vipsthumbnail` CLI rather than
// linking libvips via cgo: the appliance stays a single static binary,
// and a libvips fault on a malformed file dies in the child process,
// not the server (PRD N8 "all-day rock-solid").
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

// Converter renders stored originals via vipsthumbnail.
type Converter struct {
	bin       string // resolved absolute path to vipsthumbnail
	blobs     *blobstore.Store
	galleryPx int
	galleryQ  int
	thumbPx   int
	thumbQ    int
	log       *slog.Logger
}

// NewConverter resolves bin on PATH (fail fast if libvips tooling is
// missing — kgu.2 verified it is installed). galleryPx caps the
// browser-viewable JPEG; thumbPx the grid thumbnail. The original
// full-resolution file is preserved untouched and downloaded
// separately (kgu.18).
func NewConverter(bin string, blobs *blobstore.Store, galleryPx, galleryQ, thumbPx, thumbQ int, log *slog.Logger) (*Converter, error) {
	p, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("convert: %q not found on PATH: %w", bin, err)
	}
	return &Converter{
		bin:       p,
		blobs:     blobs,
		galleryPx: galleryPx,
		galleryQ:  galleryQ,
		thumbPx:   thumbPx,
		thumbQ:    thumbQ,
		log:       log,
	}, nil
}

// GalleryJPEG writes gallery_jpegs/<hash>.jpg from the stored original.
// Idempotent (content-addressed by the original's hash).
func (c *Converter) GalleryJPEG(ctx context.Context, hash, ext string) error {
	return c.render(ctx, blobstore.Gallery, hash, ext, c.galleryPx, c.galleryQ, ".jpg")
}

// Thumbnail writes thumbs/<hash>.webp from the stored original.
// Idempotent.
func (c *Converter) Thumbnail(ctx context.Context, hash, ext string) error {
	return c.render(ctx, blobstore.Thumb, hash, ext, c.thumbPx, c.thumbQ, ".webp")
}

// render runs vipsthumbnail to produce one rendition and atomically
// files it under kind. It downscales only (never upscales),
// auto-rotates from EXIF orientation, and `strip`s metadata from the
// derived image (privacy N2; the original keeps its EXIF). A no-op if
// the rendition already exists.
func (c *Converter) render(ctx context.Context, kind blobstore.Kind, hash, srcExt string, px, q int, outExt string) error {
	if c.blobs.Exists(kind, hash, "") {
		return nil
	}
	src := c.blobs.Path(blobstore.Original, hash, srcExt)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("convert: original %s missing: %w", hash, err)
	}

	// The temp suffix selects the encoder (vips infers format from the
	// output filename's extension).
	tmp, err := os.CreateTemp("", "ps-conv-*"+outExt)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpName)

	out := fmt.Sprintf("%s[Q=%d,strip]", tmpName, q)
	cmd := exec.CommandContext(ctx, c.bin,
		src,
		"--size", fmt.Sprintf("%dx%d", px, px),
		"-o", out,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("convert %s (%s): vipsthumbnail: %v: %s", hash, kind, err, stderr.String())
	}

	f, err := os.Open(tmpName)
	if err != nil {
		return err
	}
	defer f.Close()
	// blobstore.Put is atomic + fsync-durable into <kind>/.
	return c.blobs.Put(kind, hash, "", f)
}

// ExtForMIME maps a stored photo's MIME to the original's on-disk
// extension (originals are filed as <hash><ext>). Used by the startup
// backfill and lazy-regenerate paths, which only have the DB row.
func ExtForMIME(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/heic":
		return ".heic"
	case "image/heif":
		return ".heif"
	default:
		return ""
	}
}
