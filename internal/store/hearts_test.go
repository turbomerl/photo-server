package store

import (
	"testing"
	"time"
)

func mkPhoto(t *testing.T, s *Store, hash string) {
	t.Helper()
	if _, _, err := s.InsertPhoto(Photo{
		ContentHash: hash, MIME: "image/jpeg", UploadedAt: time.Now(),
	}); err != nil {
		t.Fatalf("insert %s: %v", hash, err)
	}
}

func TestToggleHeartAndPerViewerFlag(t *testing.T) {
	s := openTemp(t)
	for _, id := range []string{"alice", "bob"} {
		if err := s.UpsertSession(id, ""); err != nil {
			t.Fatal(err)
		}
	}
	mkPhoto(t, s, "p1")
	mkPhoto(t, s, "p2")

	count, hearted, ok, err := s.ToggleHeart("p1", "alice")
	if err != nil || !ok || !hearted || count != 1 {
		t.Fatalf("alice hearts p1: count=%d hearted=%v ok=%v err=%v, want 1/true/true", count, hearted, ok, err)
	}
	if count, hearted, _, _ := s.ToggleHeart("p1", "bob"); !hearted || count != 2 {
		t.Fatalf("bob hearts p1: count=%d hearted=%v, want 2/true", count, hearted)
	}
	if count, hearted, _, _ := s.ToggleHeart("p1", "alice"); hearted || count != 1 {
		t.Fatalf("alice un-hearts p1: count=%d hearted=%v, want 1/false", count, hearted)
	}
	// Re-hearting the same session is idempotent at the row level (PK).
	if count, hearted, _, _ := s.ToggleHeart("p1", "bob"); hearted || count != 0 {
		t.Fatalf("bob un-hearts p1: count=%d hearted=%v, want 0/false", count, hearted)
	}
	if _, _, ok, _ := s.ToggleHeart("does-not-exist", "alice"); ok {
		t.Error("toggling a missing photo should report ok=false")
	}

	// Gallery reflects counts + the viewer's own hearted flag.
	if _, _, _, err := s.ToggleHeart("p1", "bob"); err != nil { // bob hearts p1 again
		t.Fatal(err)
	}
	g, err := s.GalleryPhotos("bob", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]PhotoListItem{}
	for _, p := range g {
		got[p.Hash] = p
	}
	if got["p1"].HeartCount != 1 || !got["p1"].Hearted {
		t.Errorf("p1 for bob = %d/%v, want 1/true", got["p1"].HeartCount, got["p1"].Hearted)
	}
	if got["p2"].HeartCount != 0 || got["p2"].Hearted {
		t.Errorf("p2 for bob = %d/%v, want 0/false", got["p2"].HeartCount, got["p2"].Hearted)
	}
	// Anonymous viewer never shows hearted.
	anon, _ := s.GalleryPhotos("", 0, 10)
	for _, p := range anon {
		if p.Hearted {
			t.Errorf("anonymous viewer hearted=%v for %s, want false", p.Hearted, p.Hash)
		}
	}
}

func TestTopPhotosLeaderboard(t *testing.T) {
	s := openTemp(t)
	for _, id := range []string{"v1", "v2"} {
		if err := s.UpsertSession(id, ""); err != nil {
			t.Fatal(err)
		}
	}
	mkPhoto(t, s, "a")
	mkPhoto(t, s, "b")
	mkPhoto(t, s, "c") // stays at 0 hearts

	// b: 2 hearts, a: 1 heart.
	for _, v := range []string{"v1", "v2"} {
		if _, _, _, err := s.ToggleHeart("b", v); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := s.ToggleHeart("a", "v1"); err != nil {
		t.Fatal(err)
	}

	top, err := s.TopPhotos("v1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 {
		t.Fatalf("top len = %d, want 2 (zero-heart photo excluded)", len(top))
	}
	if top[0].Hash != "b" || top[1].Hash != "a" {
		t.Fatalf("top order = [%s %s], want [b a]", top[0].Hash, top[1].Hash)
	}
	if top[0].HeartCount != 2 || !top[0].Hearted {
		t.Errorf("top[0] = %d/%v, want 2/true (v1 hearted b)", top[0].HeartCount, top[0].Hearted)
	}
}
