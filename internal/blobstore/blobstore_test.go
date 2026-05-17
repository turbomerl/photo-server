package blobstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTemp(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// noTempLeftover asserts atomic writes leave no tmp-* files behind.
func noTempLeftover(t *testing.T, s *Store) {
	t.Helper()
	for _, k := range []Kind{Original, Thumb, Gallery} {
		matches, _ := filepath.Glob(filepath.Join(s.root, string(k), "tmp-*"))
		if len(matches) != 0 {
			t.Errorf("leftover temp files in %s: %v", k, matches)
		}
	}
}

func TestNewCreatesKindDirs(t *testing.T) {
	s := newTemp(t)
	for _, k := range []Kind{Original, Thumb, Gallery} {
		fi, err := os.Stat(filepath.Join(s.root, string(k)))
		if err != nil || !fi.IsDir() {
			t.Errorf("kind dir %s missing: %v", k, err)
		}
	}
}

func TestPutOriginalHashesShardsAndDedups(t *testing.T) {
	s := newTemp(t)
	content := []byte("a classic car, in HEIC spirit")
	want := sha(content)

	hash, size, err := s.PutOriginal(bytes.NewReader(content), "HEIC")
	if err != nil {
		t.Fatalf("PutOriginal: %v", err)
	}
	if hash != want {
		t.Fatalf("hash = %s, want %s", hash, want)
	}
	if size != int64(len(content)) {
		t.Errorf("size = %d, want %d", size, len(content))
	}

	// Sharded path with normalised lowercase extension.
	exp := filepath.Join(s.root, "originals", want[:2], want+".heic")
	if p := s.Path(Original, hash, ".HEIC"); p != exp {
		t.Errorf("Path = %s, want %s", p, exp)
	}
	got, err := os.ReadFile(exp)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("stored content mismatch: err=%v", err)
	}

	// Idempotent: second put of identical content is a no-op success.
	h2, _, err := s.PutOriginal(bytes.NewReader(content), ".heic")
	if err != nil || h2 != want {
		t.Fatalf("second PutOriginal: hash=%s err=%v", h2, err)
	}
	noTempLeftover(t, s)
}

func TestPutDerivedRenditions(t *testing.T) {
	s := newTemp(t)
	// A plausible original hash (not the rendition's own hash — the
	// rendition is keyed by the original's hash).
	orig := sha([]byte("original"))

	thumb := []byte("webp-bytes")
	if err := s.Put(Thumb, orig, "", bytes.NewReader(thumb)); err != nil {
		t.Fatalf("Put thumb: %v", err)
	}
	if p := s.Path(Thumb, orig, ""); !strings.HasSuffix(p, orig+".webp") {
		t.Errorf("thumb path = %s, want *.webp", p)
	}
	if !s.Exists(Thumb, orig, "") {
		t.Error("thumb should exist")
	}

	jpeg := []byte("jpeg-bytes")
	if err := s.Put(Gallery, orig, "", bytes.NewReader(jpeg)); err != nil {
		t.Fatalf("Put gallery: %v", err)
	}
	gp := s.Path(Gallery, orig, "")
	if !strings.HasSuffix(gp, orig+".jpg") {
		t.Errorf("gallery path = %s, want *.jpg", gp)
	}

	// Read back via Open.
	f, err := s.Open(Gallery, orig, "")
	if err != nil {
		t.Fatalf("Open gallery: %v", err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if !bytes.Equal(got, jpeg) {
		t.Errorf("gallery content mismatch")
	}

	// Idempotent.
	if err := s.Put(Gallery, orig, "", bytes.NewReader([]byte("ignored"))); err != nil {
		t.Fatalf("idempotent Put: %v", err)
	}
	noTempLeftover(t, s)
}

func TestPutRejectsBadHash(t *testing.T) {
	s := newTemp(t)
	if err := s.Put(Thumb, "not-a-hash", "", bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("expected error for invalid hash")
	}
	if err := s.Put("bogus", sha([]byte("x")), "", bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

func TestRemove(t *testing.T) {
	s := newTemp(t)
	h, _, err := s.PutOriginal(bytes.NewReader([]byte("z")), ".jpg")
	if err != nil {
		t.Fatalf("PutOriginal: %v", err)
	}
	if !s.Exists(Original, h, ".jpg") {
		t.Fatal("should exist before remove")
	}
	if err := s.Remove(Original, h, ".jpg"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if s.Exists(Original, h, ".jpg") {
		t.Fatal("should not exist after remove")
	}
	// Removing a missing blob is not an error.
	if err := s.Remove(Original, h, ".jpg"); err != nil {
		t.Fatalf("Remove missing: %v", err)
	}
}
