// Package blobstore is photo-server's content-addressed file storage.
//
// Layout (rooted at the data dir, so it sits beside photo-server.db
// under the systemd StateDirectory):
//
//	originals/<hash[:2]>/<hash><ext>      uploaded file, byte-for-byte
//	thumbs/<hash[:2]>/<hash>.webp         ~400px gallery thumbnail (kgu.13)
//	gallery_jpegs/<hash[:2]>/<hash>.jpg   browser-viewable JPEG for HEIC (kgu.12)
//
// The key is the SHA-256 of the *original* content; renditions are
// filed under that same key so upload (kgu.11), transcode and
// thumbnailing can find each other without a second lookup. The 2-hex
// shard prefix keeps directory fan-out sane (~15k photos / 256 dirs).
//
// Writes are atomic and durable: stream to a temp file on the same
// filesystem, fsync it, rename into place, then fsync the containing
// directory so an ack'd upload survives a hard power-off (PRD N6).
// Stdlib only — no dependencies.
package blobstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Kind is a storage class. Its value is also its top-level directory.
type Kind string

const (
	Original Kind = "originals"
	Thumb    Kind = "thumbs"
	Gallery  Kind = "gallery_jpegs"
)

func (k Kind) valid() bool {
	return k == Original || k == Thumb || k == Gallery
}

// Store is a content-addressed blob store rooted at a data directory.
// It is safe for concurrent use: content addressing means two writers
// of the same blob produce identical bytes, and the rename is atomic.
type Store struct {
	root string
}

// New roots a store at dir and creates the per-kind directories so
// first boot comes up with a usable layout (PRD: first-boot ready).
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("blobstore: empty root")
	}
	s := &Store{root: dir}
	for _, k := range []Kind{Original, Thumb, Gallery} {
		if err := os.MkdirAll(filepath.Join(dir, string(k)), 0o750); err != nil {
			return nil, fmt.Errorf("blobstore: mkdir %s: %w", k, err)
		}
	}
	return s, nil
}

// Root returns the store's root directory.
func (s *Store) Root() string { return s.root }

// Path returns the on-disk path for a blob. The blob may not exist.
// ext is used only for Original (the uploaded file's extension, e.g.
// ".heic"); Thumb and Gallery have fixed extensions.
func (s *Store) Path(k Kind, hash, ext string) string {
	switch k {
	case Thumb:
		ext = ".webp"
	case Gallery:
		ext = ".jpg"
	default:
		ext = normExt(ext)
	}
	return filepath.Join(s.root, string(k), hash[:2], hash+ext)
}

// Exists reports whether the blob is present.
func (s *Store) Exists(k Kind, hash, ext string) bool {
	if !k.valid() || !validHash(hash) {
		return false
	}
	_, err := os.Stat(s.Path(k, hash, ext))
	return err == nil
}

// Open opens a blob for reading. The caller closes it.
func (s *Store) Open(k Kind, hash, ext string) (*os.File, error) {
	if !k.valid() {
		return nil, fmt.Errorf("blobstore: bad kind %q", k)
	}
	if !validHash(hash) {
		return nil, fmt.Errorf("blobstore: bad hash %q", hash)
	}
	return os.Open(s.Path(k, hash, ext))
}

// PutOriginal streams r to storage, computing its SHA-256 as it goes,
// and files it under that hash with extension ext. It returns the hex
// hash and the number of bytes written. Idempotent: if the original is
// already present (same content ⇒ same hash) the existing file is kept
// and its size returned. The write is atomic and fsync-durable.
func (s *Store) PutOriginal(r io.Reader, ext string) (hash string, size int64, err error) {
	ext = normExt(ext)

	tmp, err := os.CreateTemp(filepath.Join(s.root, string(Original)), "tmp-*")
	if err != nil {
		return "", 0, fmt.Errorf("blobstore: temp: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we don't successfully rename it away.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		_ = tmp.Close()
		return "", 0, fmt.Errorf("blobstore: copy: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", 0, fmt.Errorf("blobstore: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", 0, fmt.Errorf("blobstore: close temp: %w", err)
	}

	hash = hex.EncodeToString(h.Sum(nil))
	final := s.Path(Original, hash, ext)

	if _, statErr := os.Stat(final); statErr == nil {
		// Dedup: identical content already stored. Drop the temp.
		return hash, size, nil
	}
	if err := s.placeAtomically(tmpName, final); err != nil {
		return "", 0, err
	}
	tmpName = "" // renamed away; suppress deferred cleanup
	return hash, size, nil
}

// Put writes r as the blob for an already-known hash (used by the
// transcode/thumbnail steps, whose output is filed under the
// *original's* hash). Atomic, fsync-durable, idempotent.
func (s *Store) Put(k Kind, hash, ext string, r io.Reader) error {
	if !k.valid() {
		return fmt.Errorf("blobstore: bad kind %q", k)
	}
	if !validHash(hash) {
		return fmt.Errorf("blobstore: bad hash %q", hash)
	}
	final := s.Path(k, hash, ext)
	if _, err := os.Stat(final); err == nil {
		return nil // already present
	}

	tmp, err := os.CreateTemp(filepath.Join(s.root, string(k)), "tmp-*")
	if err != nil {
		return fmt.Errorf("blobstore: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("blobstore: copy: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("blobstore: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("blobstore: close temp: %w", err)
	}
	if err := s.placeAtomically(tmpName, final); err != nil {
		return err
	}
	tmpName = ""
	return nil
}

// Remove deletes a blob if present (admin hide/delete, kgu.19). Missing
// is not an error.
func (s *Store) Remove(k Kind, hash, ext string) error {
	if !k.valid() || !validHash(hash) {
		return fmt.Errorf("blobstore: bad kind/hash")
	}
	err := os.Remove(s.Path(k, hash, ext))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// placeAtomically moves tmp to final: ensure the shard dir exists,
// rename (atomic on one filesystem), then fsync the shard dir so the
// new directory entry is durable across power loss.
func (s *Store) placeAtomically(tmp, final string) error {
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("blobstore: mkdir shard: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("blobstore: rename: %w", err)
	}
	return fsyncDir(dir)
}

func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("blobstore: open dir for fsync: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("blobstore: fsync dir: %w", err)
	}
	return nil
}

// normExt lower-cases and ensures a single leading dot ("" stays "").
func normExt(ext string) string {
	if ext == "" {
		return ""
	}
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

// validHash accepts a lowercase SHA-256 hex string (64 chars). The
// 2-char shard prefix needs at least that to be well-formed.
func validHash(hash string) bool {
	if len(hash) != sha256.Size*2 {
		return false
	}
	for i := 0; i < len(hash); i++ {
		c := hash[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
