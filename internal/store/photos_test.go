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
