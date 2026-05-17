package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// jpegWithEXIF builds a tiny JPEG (SOI + APP1/Exif/TIFF + EOI) whose
// EXIF DateTimeOriginal is dt ("YYYY:MM:DD HH:MM:SS"). Not a decodable
// image — kgu.11 never decodes, it only hashes/stores/reads EXIF.
func jpegWithEXIF(dt string) []byte {
	bo := binary.LittleEndian
	str := append([]byte(dt), 0)
	const (
		ifd0Off = 8
		ifd0Len = 2 + 12 + 4
		exifOff = ifd0Off + ifd0Len
		exifLen = 2 + 12 + 4
		strOff  = exifOff + exifLen
	)
	tiff := make([]byte, strOff+len(str))
	copy(tiff[0:2], "II")
	bo.PutUint16(tiff[2:4], 0x2A)
	bo.PutUint32(tiff[4:8], ifd0Off)
	bo.PutUint16(tiff[ifd0Off:], 1)
	bo.PutUint16(tiff[ifd0Off+2:], 0x8769) // ExifIFDPointer
	bo.PutUint16(tiff[ifd0Off+4:], 4)
	bo.PutUint32(tiff[ifd0Off+6:], 1)
	bo.PutUint32(tiff[ifd0Off+10:], exifOff)
	bo.PutUint16(tiff[exifOff:], 1)
	bo.PutUint16(tiff[exifOff+2:], 0x9003) // DateTimeOriginal
	bo.PutUint16(tiff[exifOff+4:], 2)
	bo.PutUint32(tiff[exifOff+6:], uint32(len(str)))
	bo.PutUint32(tiff[exifOff+10:], strOff)
	copy(tiff[strOff:], str)

	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8, 0xFF, 0xE1})
	seg := append([]byte("Exif\x00\x00"), tiff...)
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(2+len(seg)))
	b.Write(l[:])
	b.Write(seg)
	b.Write([]byte{0xFF, 0xD9})
	return b.Bytes()
}

// multipartBody builds a multipart form: fields + named file parts
// {fieldname: {filename, content}}.
func multipartBody(t *testing.T, fields map[string]string, files []struct {
	field, name string
	data        []byte
}) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	for _, f := range files {
		fw, err := mw.CreateFormFile(f.field, f.name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(f.data)
	}
	mw.Close()
	return mw.FormDataContentType(), &buf
}

func doUpload(t *testing.T, s *Server, ct string, body *bytes.Buffer, cookie string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", ct)
	if cookie != "" {
		req.Header.Set("Cookie", "ps_session="+cookie)
	}
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func firstResult(out map[string]any) map[string]any {
	up, _ := out["uploaded"].([]any)
	if len(up) == 0 {
		return nil
	}
	m, _ := up[0].(map[string]any)
	return m
}

func TestUploadNewThenDedup(t *testing.T) {
	s := newTestServer(t)
	img := jpegWithEXIF("2023:10:22 09:39:48")

	ct, body := multipartBody(t, nil, []struct {
		field, name string
		data        []byte
	}{{"file", "classic.jpg", img}})
	rec, out := doUpload(t, s, ct, body, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	r := firstResult(out)
	if r == nil || r["ok"] != true {
		t.Fatalf("result not ok: %v", r)
	}
	if r["mime"] != "image/jpeg" {
		t.Errorf("mime = %v, want image/jpeg", r["mime"])
	}
	if r["deduped"] == true {
		t.Errorf("first upload should not be deduped")
	}
	id := int64(r["photo_id"].(float64))

	gotTaken, ok, err := s.st.PhotoTakenAt(id)
	if err != nil || !ok {
		t.Fatalf("taken_at not set: ok=%v err=%v", ok, err)
	}
	want := time.Date(2023, 10, 22, 9, 39, 48, 0, time.UTC)
	if !gotTaken.Equal(want) {
		t.Errorf("taken_at = %v, want %v", gotTaken, want)
	}

	// Re-upload identical bytes → dedup, no second row.
	ct2, body2 := multipartBody(t, nil, []struct {
		field, name string
		data        []byte
	}{{"file", "again.jpg", img}})
	_, out2 := doUpload(t, s, ct2, body2, "")
	r2 := firstResult(out2)
	if r2["deduped"] != true || r2["hash"] != r["hash"] {
		t.Errorf("expected dedup to same hash: %v", r2)
	}
	var count int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM photos`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("photos count = %d, want 1 (dedup)", count)
	}
}

func TestUploadWithSessionAndName(t *testing.T) {
	s := newTestServer(t)
	img := jpegWithEXIF("2024:01:02 03:04:05")

	ct, body := multipartBody(t, map[string]string{"display_name": "Aunt Sue"},
		[]struct {
			field, name string
			data        []byte
		}{{"file", "p.jpg", img}})
	rec, out := doUpload(t, s, ct, body, "sess-123")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	id := int64(firstResult(out)["photo_id"].(float64))

	sess, ok, err := s.st.GetSession("sess-123")
	if err != nil || !ok {
		t.Fatalf("session not created: ok=%v err=%v", ok, err)
	}
	if sess.DisplayName != "Aunt Sue" {
		t.Errorf("session name = %q, want Aunt Sue", sess.DisplayName)
	}
	var sid, dn string
	if err := s.st.DB().QueryRow(
		`SELECT uploader_session_id, display_name FROM photos WHERE id=?`, id,
	).Scan(&sid, &dn); err != nil {
		t.Fatal(err)
	}
	if sid != "sess-123" || dn != "Aunt Sue" {
		t.Errorf("photo tag = (%q,%q), want (sess-123, Aunt Sue)", sid, dn)
	}
}

func TestUploadMixedTypes(t *testing.T) {
	s := newTestServer(t)
	img := jpegWithEXIF("2022:02:02 02:02:02")

	ct, body := multipartBody(t, nil, []struct {
		field, name string
		data        []byte
	}{
		{"file", "good.jpg", img},
		{"file", "notes.txt", []byte("this is definitely not an image")},
	})
	rec, out := doUpload(t, s, ct, body, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (one good file)", rec.Code)
	}
	up := out["uploaded"].([]any)
	if len(up) != 2 {
		t.Fatalf("results = %d, want 2", len(up))
	}
	var okCount, badCount int
	for _, e := range up {
		m := e.(map[string]any)
		if m["ok"] == true {
			okCount++
		} else {
			badCount++
		}
	}
	if okCount != 1 || badCount != 1 {
		t.Errorf("ok=%d bad=%d, want 1/1", okCount, badCount)
	}
}

func TestUploadAllUnsupported(t *testing.T) {
	s := newTestServer(t)
	ct, body := multipartBody(t, nil, []struct {
		field, name string
		data        []byte
	}{{"file", "a.txt", []byte("nope")}})
	rec, _ := doUpload(t, s, ct, body, "")
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestUploadNoFiles(t *testing.T) {
	s := newTestServer(t)
	ct, body := multipartBody(t, map[string]string{"display_name": "x"}, nil)
	rec, _ := doUpload(t, s, ct, body, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
