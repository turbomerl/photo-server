// Package exif extracts just the EXIF DateTimeOriginal timestamp from
// JPEG and HEIF/HEIC images — the single tag photo-server needs for
// photos.taken_at.
//
// It is deliberately minimal and best-effort: any unrecognised or
// malformed input yields ok=false (never a panic). The caller stores
// NULL on a miss, which the gallery tolerates because it orders by
// uploaded_at, not taken_at.
//
// JPEG EXIF is parsed exactly from the APP1 segment. For HEIF/HEIC the
// full ISO-BMFF iinf/iloc item chain is brittle to hand-roll, so we
// instead scan the first 1 MiB for the "Exif\0\0" signature — iPhone
// HEIC stores the Exif item in the `meta` box near the start of the
// file, well before the image mdat — and parse the TIFF block there.
// A HEIC that hides EXIF past that window simply yields no taken_at.
//
// Stdlib only.
package exif

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"time"
)

const (
	tagExifIFDPointer   = 0x8769
	tagDateTimeOriginal = 0x9003

	scanWindow  = 1 << 20 // HEIF Exif-signature search window
	maxTIFF     = 1 << 20 // cap on the TIFF block we will parse
	maxJPEGScan = 1 << 20 // cap on how far we walk JPEG segments
)

var errNoEXIF = errors.New("exif: no parseable DateTimeOriginal")

// DateTimeOriginal returns the EXIF DateTimeOriginal for the image in r
// (of length size). ok is false when there is no parseable EXIF date.
//
// EXIF stores a naive "YYYY:MM:DD HH:MM:SS" with no timezone; we
// interpret it as UTC so stored values are stable and sortable.
func DateTimeOriginal(r io.ReaderAt, size int64) (t time.Time, ok bool) {
	defer func() {
		// Belt-and-braces: a malformed photo must never panic the
		// upload path.
		if recover() != nil {
			t, ok = time.Time{}, false
		}
	}()

	tiff, err := extractTIFF(r, size)
	if err != nil {
		return time.Time{}, false
	}
	s, err := dateTimeOriginalFromTIFF(tiff)
	if err != nil {
		return time.Time{}, false
	}
	return parseEXIFDateTime(s)
}

// extractTIFF returns the TIFF block (starting at the "II"/"MM" header)
// for either a JPEG or a HEIF/HEIC input.
func extractTIFF(r io.ReaderAt, size int64) ([]byte, error) {
	head := make([]byte, 12)
	n, _ := r.ReadAt(head, 0)
	head = head[:n]

	// JPEG: 0xFFD8 SOI — walk segments to APP1.
	if len(head) >= 2 && head[0] == 0xFF && head[1] == 0xD8 {
		if tiff, err := jpegTIFF(r, size); err == nil {
			return tiff, nil
		}
		// fall through to the generic scan if the JPEG walk failed
	}

	// HEIF/HEIC (and JPEG fallback): scan a bounded window for the
	// "Exif\0\0" signature, then the TIFF header follows.
	win := size
	if win > scanWindow {
		win = scanWindow
	}
	buf := make([]byte, win)
	m, _ := r.ReadAt(buf, 0)
	buf = buf[:m]

	// "Exif\0\0" appears more than once: first as the item-type
	// declaration in the HEIF `infe` box, then again before the actual
	// Exif payload. Take the first occurrence whose following bytes are
	// a real TIFF header.
	sig := []byte("Exif\x00\x00")
	for base := 0; ; {
		rel := bytes.Index(buf[base:], sig)
		if rel < 0 {
			return nil, errNoEXIF
		}
		start := base + rel + len(sig)
		tiff := buf[start:]
		if looksLikeTIFF(tiff) {
			if len(tiff) > maxTIFF {
				tiff = tiff[:maxTIFF]
			}
			return tiff, nil
		}
		base = base + rel + 1
	}
}

// jpegTIFF walks JPEG marker segments and returns the TIFF block inside
// the first APP1 "Exif" segment.
func jpegTIFF(r io.ReaderAt, size int64) ([]byte, error) {
	off := int64(2) // past SOI
	hdr := make([]byte, 4)
	limit := size
	if limit > maxJPEGScan {
		limit = maxJPEGScan
	}
	for off+4 <= limit {
		if _, err := r.ReadAt(hdr, off); err != nil {
			return nil, err
		}
		if hdr[0] != 0xFF {
			return nil, errNoEXIF
		}
		marker := hdr[1]
		if marker == 0xDA || marker == 0xD9 { // SOS / EOI: no more metadata
			return nil, errNoEXIF
		}
		segLen := int(binary.BigEndian.Uint16(hdr[2:4])) // includes the 2 length bytes
		if segLen < 2 {
			return nil, errNoEXIF
		}
		if marker == 0xE1 { // APP1
			payLen := segLen - 2
			if payLen > maxTIFF+6 {
				payLen = maxTIFF + 6
			}
			pay := make([]byte, payLen)
			if _, err := r.ReadAt(pay, off+4); err != nil {
				return nil, err
			}
			if bytes.HasPrefix(pay, []byte("Exif\x00\x00")) {
				tiff := pay[6:]
				if looksLikeTIFF(tiff) {
					return tiff, nil
				}
			}
		}
		off += 2 + int64(segLen) // 2 marker bytes + segment
	}
	return nil, errNoEXIF
}

func looksLikeTIFF(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	return (b[0] == 'I' && b[1] == 'I' && b[2] == 0x2A && b[3] == 0x00) ||
		(b[0] == 'M' && b[1] == 'M' && b[2] == 0x00 && b[3] == 0x2A)
}

// dateTimeOriginalFromTIFF reads IFD0 → ExifIFD → DateTimeOriginal.
func dateTimeOriginalFromTIFF(t []byte) (string, error) {
	if !looksLikeTIFF(t) {
		return "", errNoEXIF
	}
	var bo binary.ByteOrder
	if t[0] == 'I' {
		bo = binary.LittleEndian
	} else {
		bo = binary.BigEndian
	}
	if len(t) < 8 {
		return "", errNoEXIF
	}
	ifd0 := bo.Uint32(t[4:8])

	exifIFD, ok := ifdLongValue(t, bo, int(ifd0), tagExifIFDPointer)
	if !ok {
		return "", errNoEXIF
	}
	s, ok := ifdASCIIValue(t, bo, int(exifIFD), tagDateTimeOriginal)
	if !ok {
		return "", errNoEXIF
	}
	return s, nil
}

// ifdEntries iterates the 12-byte entries of the IFD at off, calling fn
// with (tag, typ, count, valueField). It is fully bounds-checked.
func ifdEntries(t []byte, bo binary.ByteOrder, off int, fn func(tag, typ uint16, count uint32, val []byte) bool) {
	if off < 0 || off+2 > len(t) {
		return
	}
	n := int(bo.Uint16(t[off : off+2]))
	p := off + 2
	for i := 0; i < n; i++ {
		if p+12 > len(t) {
			return
		}
		tag := bo.Uint16(t[p : p+2])
		typ := bo.Uint16(t[p+2 : p+4])
		count := bo.Uint32(t[p+4 : p+8])
		val := t[p+8 : p+12]
		if fn(tag, typ, count, val) {
			return
		}
		p += 12
	}
}

func ifdLongValue(t []byte, bo binary.ByteOrder, off int, want uint16) (uint32, bool) {
	var out uint32
	var found bool
	ifdEntries(t, bo, off, func(tag, typ uint16, _ uint32, val []byte) bool {
		if tag == want {
			out = bo.Uint32(val)
			found = true
			return true
		}
		return false
	})
	return out, found
}

func ifdASCIIValue(t []byte, bo binary.ByteOrder, off int, want uint16) (string, bool) {
	var out string
	var found bool
	ifdEntries(t, bo, off, func(tag, typ uint16, count uint32, val []byte) bool {
		if tag != want {
			return false
		}
		n := int(count)
		if n <= 0 || n > 64 { // DateTimeOriginal is 20 bytes; sanity cap
			return true
		}
		var raw []byte
		if n <= 4 { // inline in the value field
			raw = val[:n]
		} else {
			p := int(bo.Uint32(val))
			if p < 0 || p+n > len(t) {
				return true
			}
			raw = t[p : p+n]
		}
		out = string(raw)
		found = true
		return true
	})
	return out, found
}

// parseEXIFDateTime parses "YYYY:MM:DD HH:MM:SS" (trimming NUL/space
// padding) as UTC, rejecting the all-zero sentinel some cameras write.
func parseEXIFDateTime(s string) (time.Time, bool) {
	s = strings.TrimRight(s, "\x00")
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "0000") {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006:01:02 15:04:05", s, time.UTC)
	if err != nil || t.Year() < 1970 {
		return time.Time{}, false
	}
	return t, true
}
