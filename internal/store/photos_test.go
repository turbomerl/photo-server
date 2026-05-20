package store

import (
	"testing"
	"time"
)

func TestInsertPhotoAndDedup(t *testing.T) {
	s := openTemp(t)

	taken := time.Date(2023, 10, 22, 9, 39, 48, 0, time.UTC)
	p := Photo{
		ContentHash:      "hashA",
		MIME:             "image/heic",
		OriginalFilename: "classic-car.heic",
		DisplayName:      "Anonymous",
		TakenAt:          taken,
		UploadedAt:       time.Now(),
	}

	id, deduped, err := s.InsertPhoto(p)
	if err != nil || deduped || id == 0 {
		t.Fatalf("first insert: id=%d deduped=%v err=%v", id, deduped, err)
	}

	gotTaken, ok, err := s.PhotoTakenAt(id)
	if err != nil || !ok || !gotTaken.Equal(taken) {
		t.Fatalf("taken_at = %v ok=%v err=%v, want %v", gotTaken, ok, err, taken)
	}

	// Same content hash → dedup to the same row, no new insert.
	id2, deduped2, err := s.InsertPhoto(p)
	if err != nil || !deduped2 || id2 != id {
		t.Fatalf("dedup insert: id=%d deduped=%v err=%v (want id=%d deduped)", id2, deduped2, err, id)
	}

	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM photos`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("photos count = %d, want 1", count)
	}
}

func TestInsertPhotoNullTakenAt(t *testing.T) {
	s := openTemp(t)
	id, _, err := s.InsertPhoto(Photo{
		ContentHash: "hashNull",
		MIME:        "image/jpeg",
		UploadedAt:  time.Now(),
		// TakenAt zero → NULL
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, ok, err := s.PhotoTakenAt(id); ok || err != nil {
		t.Fatalf("taken_at should be NULL: ok=%v err=%v", ok, err)
	}
}

func TestAllPhotosAndByHash(t *testing.T) {
	s := openTemp(t)
	now := time.Now()
	for _, p := range []Photo{
		{ContentHash: "h-heic", MIME: "image/heic", UploadedAt: now},
		{ContentHash: "h-heif", MIME: "image/heif", UploadedAt: now},
		{ContentHash: "h-jpeg", MIME: "image/jpeg", UploadedAt: now},
	} {
		if _, _, err := s.InsertPhoto(p); err != nil {
			t.Fatalf("insert %s: %v", p.ContentHash, err)
		}
	}

	refs, err := s.AllPhotos()
	if err != nil {
		t.Fatalf("AllPhotos: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("got %d refs, want 3 (%v)", len(refs), refs)
	}

	r, ok, err := s.PhotoByHash("h-jpeg")
	if err != nil || !ok {
		t.Fatalf("PhotoByHash(h-jpeg): ok=%v err=%v", ok, err)
	}
	if r.MIME != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg", r.MIME)
	}
	if _, ok, _ := s.PhotoByHash("nope"); ok {
		t.Error("PhotoByHash(nope) should be ok=false")
	}
}

func TestSessionAndGalleryPhotoFeeds(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertSession("sessA", "A"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSession("sessB", "B"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	mk := func(hash, sess string, hidden bool) {
		if _, _, err := s.InsertPhoto(Photo{
			ContentHash: hash, MIME: "image/jpeg", UploaderSessionID: sess,
			UploadedAt: now,
		}); err != nil {
			t.Fatalf("insert %s: %v", hash, err)
		}
		if hidden {
			if _, err := s.DB().Exec(
				`UPDATE photos SET hidden_at=1 WHERE content_hash=?`, hash); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("p1", "sessA", false)
	mk("p2", "sessB", false)
	mk("p3", "sessA", true) // hidden
	mk("p4", "sessA", false)

	// SessionPhotos: only sessA's visible, newest (highest id) first.
	mine, err := s.SessionPhotos("sessA", 10)
	if err != nil {
		t.Fatalf("SessionPhotos: %v", err)
	}
	if len(mine) != 2 || mine[0].Hash != "p4" || mine[1].Hash != "p1" {
		t.Fatalf("SessionPhotos = %+v, want [p4 p1]", mine)
	}

	// GalleryPhotos: all visible newest-first, keyset paginated.
	page1, err := s.GalleryPhotos(0, 2)
	if err != nil {
		t.Fatalf("GalleryPhotos p1: %v", err)
	}
	if len(page1) != 2 || page1[0].Hash != "p4" || page1[1].Hash != "p2" {
		t.Fatalf("gallery page1 = %+v, want [p4 p2]", page1)
	}
	page2, err := s.GalleryPhotos(page1[1].ID, 2)
	if err != nil {
		t.Fatalf("GalleryPhotos p2: %v", err)
	}
	if len(page2) != 1 || page2[0].Hash != "p1" {
		t.Fatalf("gallery page2 = %+v, want [p1] (p3 hidden)", page2)
	}
}

func TestAdminPhotosCountsHideDelete(t *testing.T) {
	s := openTemp(t)
	for _, h := range []string{"a", "b", "c"} {
		if _, _, err := s.InsertPhoto(Photo{
			ContentHash: h, MIME: "image/jpeg",
			OriginalFilename: h + ".jpg",
			UploadedAt:       time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Hide "b"; admin list still sees it, gallery feed does not.
	if err := s.SetHidden("b", true); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	v, hh, err := s.PhotoCounts()
	if err != nil || v != 2 || hh != 1 {
		t.Fatalf("counts (%d,%d) err=%v, want (2,1)", v, hh, err)
	}
	rows, err := s.AdminPhotos(10)
	if err != nil || len(rows) != 3 {
		t.Fatalf("AdminPhotos len=%d err=%v, want 3", len(rows), err)
	}
	var sawHidden bool
	for _, r := range rows {
		if r.Hash == "b" {
			if r.HiddenAt == nil || *r.HiddenAt <= 0 {
				t.Errorf("b should have HiddenAt set, got %v", r.HiddenAt)
			}
			sawHidden = true
		}
	}
	if !sawHidden {
		t.Error("AdminPhotos must include hidden rows")
	}

	// Unhide brings it back to visible.
	if err := s.SetHidden("b", false); err != nil {
		t.Fatal(err)
	}
	v, hh, _ = s.PhotoCounts()
	if v != 3 || hh != 0 {
		t.Errorf("after unhide counts = (%d,%d), want (3,0)", v, hh)
	}

	// Delete "a" → row gone, mime returned for blob cleanup.
	mime, ok, err := s.DeletePhoto("a")
	if err != nil || !ok || mime != "image/jpeg" {
		t.Fatalf("DeletePhoto(a) mime=%q ok=%v err=%v", mime, ok, err)
	}
	if _, _, err := s.PhotoMeta("a"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.PhotoMeta("a"); ok {
		t.Error("deleted photo should not be reachable via PhotoMeta")
	}
	// Deleting again → ok=false, no error.
	if _, ok, err := s.DeletePhoto("a"); err != nil || ok {
		t.Errorf("re-DeletePhoto(a) ok=%v err=%v, want (false, nil)", ok, err)
	}
}

func TestPhotoMetaVisibleOnly(t *testing.T) {
	s := openTemp(t)
	if _, _, err := s.InsertPhoto(Photo{
		ContentHash: "vis", MIME: "image/heic",
		OriginalFilename: "IMG_1234.HEIC", DisplayName: "Aunt Sue",
		UploadedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	m, ok, err := s.PhotoMeta("vis")
	if err != nil || !ok {
		t.Fatalf("PhotoMeta(vis): ok=%v err=%v", ok, err)
	}
	if m.MIME != "image/heic" || m.OriginalFilename != "IMG_1234.HEIC" || m.DisplayName != "Aunt Sue" {
		t.Fatalf("PhotoMeta = %+v", m)
	}

	if _, ok, _ := s.PhotoMeta("nope"); ok {
		t.Error("unknown hash should be ok=false")
	}

	// Hidden photos must not be reachable via direct URL.
	if _, err := s.DB().Exec(
		`UPDATE photos SET hidden_at=1 WHERE content_hash='vis'`); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.PhotoMeta("vis"); ok {
		t.Error("hidden photo should report ok=false")
	}
}

func TestInsertPhotoSessionFK(t *testing.T) {
	s := openTemp(t)

	// Unknown session → FK violation.
	_, _, err := s.InsertPhoto(Photo{
		ContentHash:       "hashFK",
		MIME:              "image/jpeg",
		UploaderSessionID: "no-such-session",
		UploadedAt:        time.Now(),
	})
	if err == nil {
		t.Fatal("expected FK error for unknown uploader_session_id")
	}

	// After upserting the session it succeeds and is tagged.
	if err := s.UpsertSession("sess-x", "Bob"); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	id, _, err := s.InsertPhoto(Photo{
		ContentHash:       "hashFK2",
		MIME:              "image/jpeg",
		UploaderSessionID: "sess-x",
		UploadedAt:        time.Now(),
	})
	if err != nil {
		t.Fatalf("insert with valid session: %v", err)
	}
	var sid string
	if err := s.DB().QueryRow(
		`SELECT uploader_session_id FROM photos WHERE id = ?`, id,
	).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if sid != "sess-x" {
		t.Errorf("uploader_session_id = %q, want sess-x", sid)
	}
}
