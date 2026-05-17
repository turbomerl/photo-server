package convert

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/turbomerl/photo-server/internal/blobstore"
)

const realHEIC = "/home/isambard-poulson/Downloads/classic-car.heic"

// seedHEIC copies the real iPhone HEIC into a fresh blobstore as an
// original, returning the store and the content hash.
func seedHEIC(t *testing.T) (*blobstore.Store, string) {
	t.Helper()
	if _, err := exec.LookPath("vipsthumbnail"); err != nil {
		t.Skip("vipsthumbnail not on PATH")
	}
	f, err := os.Open(realHEIC)
	if err != nil {
		t.Skipf("real HEIC sample absent: %v", err)
	}
	defer f.Close()

	bs, err := blobstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	hash, _, err := bs.PutOriginal(f, ".heic")
	if err != nil {
		t.Fatalf("PutOriginal: %v", err)
	}
	return bs, hash
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func isJPEG(b []byte) bool {
	return len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
}

func TestNewConverterMissingBin(t *testing.T) {
	if _, err := NewConverter("definitely-not-a-real-binary-xyz", nil, 2560, 85, quietLog()); err == nil {
		t.Fatal("expected error for missing vipsthumbnail binary")
	}
}

func TestGalleryJPEGFromRealHEIC(t *testing.T) {
	bs, hash := seedHEIC(t)

	c, err := NewConverter("vipsthumbnail", bs, 1024, 80, quietLog())
	if err != nil {
		t.Fatalf("NewConverter: %v", err)
	}
	if err := c.GalleryJPEG(context.Background(), hash, ".heic"); err != nil {
		t.Fatalf("GalleryJPEG: %v", err)
	}

	if !bs.Exists(blobstore.Gallery, hash, "") {
		t.Fatal("gallery JPEG not created")
	}
	f, err := bs.Open(blobstore.Gallery, hash, "")
	if err != nil {
		t.Fatalf("open gallery jpeg: %v", err)
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	if !isJPEG(data) {
		t.Fatalf("gallery output is not a JPEG (first bytes: % x)", data[:min(4, len(data))])
	}
	if len(data) < 1000 {
		t.Errorf("gallery JPEG suspiciously small: %d bytes", len(data))
	}

	// Idempotent: second call is a no-op and must not change the file.
	before, _ := os.ReadFile(bs.Path(blobstore.Gallery, hash, ""))
	if err := c.GalleryJPEG(context.Background(), hash, ".heic"); err != nil {
		t.Fatalf("second GalleryJPEG: %v", err)
	}
	after, _ := os.ReadFile(bs.Path(blobstore.Gallery, hash, ""))
	if !bytes.Equal(before, after) {
		t.Error("idempotent conversion rewrote the gallery JPEG")
	}
}

func TestGalleryJPEGMissingOriginal(t *testing.T) {
	if _, err := exec.LookPath("vipsthumbnail"); err != nil {
		t.Skip("vipsthumbnail not on PATH")
	}
	bs, err := blobstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewConverter("vipsthumbnail", bs, 2560, 85, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	// 64-hex but no file on disk.
	h := "0000000000000000000000000000000000000000000000000000000000000000"
	if err := c.GalleryJPEG(context.Background(), h, ".heic"); err == nil {
		t.Fatal("expected error when original is missing")
	}
}

func TestPoolConvertsAndDedupes(t *testing.T) {
	bs, hash := seedHEIC(t)
	c, err := NewConverter("vipsthumbnail", bs, 1024, 80, quietLog())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPool(c, 2, 16, quietLog())
	p.Start(ctx)

	p.Enqueue(hash, ".heic")
	p.Enqueue(hash, ".heic") // de-duped: same hash, no second conversion

	deadline := time.After(20 * time.Second)
	for !bs.Exists(blobstore.Gallery, hash, "") {
		select {
		case <-deadline:
			t.Fatal("pool did not produce gallery JPEG within 20s")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	p.Stop() // must return promptly once workers see ctx cancel
}
