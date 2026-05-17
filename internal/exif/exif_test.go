package exif

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
	"time"
)

// buildTIFF builds a minimal valid EXIF TIFF block with IFD0 →
// ExifIFD → DateTimeOriginal = dt, in the given byte order.
func buildTIFF(bo binary.ByteOrder, dt string) []byte {
	if len(dt) != 19 {
		panic("dt must be 19 chars YYYY:MM:DD HH:MM:SS")
	}
	str := append([]byte(dt), 0) // 20 bytes incl NUL

	const (
		hdrLen  = 8
		ifd0Off = 8
		ifd0Len = 2 + 12 + 4 // count + 1 entry + next
		exifOff = ifd0Off + ifd0Len
		exifLen = 2 + 12 + 4
		strOff  = exifOff + exifLen
	)
	buf := make([]byte, strOff+len(str))

	// TIFF header.
	if bo == binary.LittleEndian {
		copy(buf[0:2], "II")
		bo.PutUint16(buf[2:4], 0x2A)
	} else {
		copy(buf[0:2], "MM")
		bo.PutUint16(buf[2:4], 0x2A)
	}
	bo.PutUint32(buf[4:8], ifd0Off)

	// IFD0: one entry, ExifIFDPointer (LONG) → exifOff.
	bo.PutUint16(buf[ifd0Off:], 1)
	e := ifd0Off + 2
	bo.PutUint16(buf[e:], tagExifIFDPointer)
	bo.PutUint16(buf[e+2:], 4) // LONG
	bo.PutUint32(buf[e+4:], 1)
	bo.PutUint32(buf[e+8:], exifOff)
	bo.PutUint32(buf[ifd0Off+2+12:], 0) // next IFD = 0

	// Exif IFD: one entry, DateTimeOriginal (ASCII[20]) → strOff.
	bo.PutUint16(buf[exifOff:], 1)
	e = exifOff + 2
	bo.PutUint16(buf[e:], tagDateTimeOriginal)
	bo.PutUint16(buf[e+2:], 2) // ASCII
	bo.PutUint32(buf[e+4:], uint32(len(str)))
	bo.PutUint32(buf[e+8:], strOff)
	bo.PutUint32(buf[exifOff+2+12:], 0)

	copy(buf[strOff:], str)
	return buf
}

func wantTime(t *testing.T, got time.Time, ok bool, y int, mo time.Month, d, h, mi, s int) {
	t.Helper()
	if !ok {
		t.Fatalf("ok = false, wanted a parsed time")
	}
	w := time.Date(y, mo, d, h, mi, s, 0, time.UTC)
	if !got.Equal(w) {
		t.Fatalf("time = %v, want %v", got, w)
	}
}

func TestJPEG_APP1_LittleEndian(t *testing.T) {
	tiff := buildTIFF(binary.LittleEndian, "2023:10:22 09:39:48")

	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8}) // SOI
	b.Write([]byte{0xFF, 0xE1}) // APP1
	seg := append([]byte("Exif\x00\x00"), tiff...)
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(2+len(seg)))
	b.Write(l[:])
	b.Write(seg)
	b.Write([]byte{0xFF, 0xD9}) // EOI

	data := b.Bytes()
	got, ok := DateTimeOriginal(bytes.NewReader(data), int64(len(data)))
	wantTime(t, got, ok, 2023, time.October, 22, 9, 39, 48)
}

func TestHEIF_SignatureScan_BigEndian(t *testing.T) {
	tiff := buildTIFF(binary.BigEndian, "2021:07:04 18:30:00")

	var b bytes.Buffer
	// Plausible ISO-BMFF prefix so we exercise the non-JPEG path.
	ftyp := []byte{0, 0, 0, 0x18}
	ftyp = append(ftyp, "ftyp"...)
	ftyp = append(ftyp, "heic"...)
	ftyp = append(ftyp, 0, 0, 0, 0)
	ftyp = append(ftyp, "mif1heic"...)
	b.Write(ftyp)
	b.Write(make([]byte, 64)) // filler box bytes
	b.Write([]byte("Exif\x00\x00"))
	b.Write(tiff)

	data := b.Bytes()
	got, ok := DateTimeOriginal(bytes.NewReader(data), int64(len(data)))
	wantTime(t, got, ok, 2021, time.July, 4, 18, 30, 0)
}

func TestGarbageAndEdgeCasesReturnFalse(t *testing.T) {
	cases := map[string][]byte{
		"empty":          {},
		"random":         []byte("not an image at all, no exif here"),
		"jpeg no app1":   {0xFF, 0xD8, 0xFF, 0xD9},
		"exif sig only":  []byte("....Exif\x00\x00short"),
		"bad tiff magic": append([]byte("Exif\x00\x00"), []byte("XX\x00\x00rest")...),
	}
	for name, data := range cases {
		if _, ok := DateTimeOriginal(bytes.NewReader(data), int64(len(data))); ok {
			t.Errorf("%s: ok = true, want false", name)
		}
	}
}

func TestZeroDateRejected(t *testing.T) {
	tiff := buildTIFF(binary.LittleEndian, "0000:00:00 00:00:00")
	data := append([]byte("Exif\x00\x00"), tiff...)
	if _, ok := DateTimeOriginal(bytes.NewReader(data), int64(len(data))); ok {
		t.Error("all-zero EXIF date accepted, want rejected")
	}
}

// Real iPhone HEIC, if present on this box (the kgu.2 sample). Skipped
// elsewhere so the suite stays self-contained.
func TestRealiPhoneHEIC(t *testing.T) {
	const p = "/home/isambard-poulson/Downloads/classic-car.heic"
	f, err := os.Open(p)
	if err != nil {
		t.Skipf("real HEIC not present (%v)", err)
	}
	defer f.Close()
	fi, _ := f.Stat()
	got, ok := DateTimeOriginal(f, fi.Size())
	if !ok {
		t.Fatal("failed to extract DateTimeOriginal from real iPhone HEIC")
	}
	// `file` reported datetime=2023:10:22 09:39:48 for this sample.
	wantTime(t, got, ok, 2023, time.October, 22, 9, 39, 48)
}
