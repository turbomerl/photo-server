package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/turbomerl/photo-server/internal/store"
)

func seedPhotos(t *testing.T, s *Server, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, _, err := s.st.InsertPhoto(store.Photo{
			ContentHash: fmt.Sprintf("h%04d", i),
			MIME:        "image/jpeg",
			UploadedAt:  time.Now(),
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
}

func TestGalleryEmptyState(t *testing.T) {
	s := newTestServer(t)
	b := get(t, s, "/gallery").Body.String()
	if !strings.Contains(b, "No photos yet") {
		t.Error("empty gallery should show the empty state")
	}
	if !strings.Contains(b, `src="/static/gallery.js"`) {
		t.Error("gallery page does not include gallery.js")
	}
	if !strings.Contains(b, `data-next-before="0"`) {
		t.Error("empty gallery should have cursor 0")
	}
}

func TestGalleryFirstPageServerRendered(t *testing.T) {
	s := newTestServer(t)
	seedPhotos(t, s, galleryPageSize+5) // 35

	b := get(t, s, "/gallery").Body.String()
	// First page is real <img src> (works with JS disabled) — not
	// data-src (that's only for JS-appended tiles).
	got := strings.Count(b, `<img loading="lazy" src="/thumb/`)
	if got != galleryPageSize {
		t.Fatalf("server-rendered tiles = %d, want %d", got, galleryPageSize)
	}
	if strings.Contains(b, "data-src=") {
		t.Error("server-rendered first page must not use data-src (no-JS must work)")
	}
	// Newest first: the last-seeded hash appears, the oldest does not.
	if !strings.Contains(b, "/thumb/h0034") {
		t.Error("newest photo missing from first page")
	}
	if strings.Contains(b, "/thumb/h0000") {
		t.Error("oldest photo should be on a later page, not the first")
	}
	if strings.Contains(b, `data-next-before="0"`) {
		t.Error("full first page should expose a non-zero cursor")
	}
}

func TestGalleryHidesHidden(t *testing.T) {
	s := newTestServer(t)
	seedPhotos(t, s, 2)
	if _, err := s.st.DB().Exec(
		`UPDATE photos SET hidden_at=1 WHERE content_hash='h0001'`); err != nil {
		t.Fatal(err)
	}
	b := get(t, s, "/gallery").Body.String()
	if strings.Contains(b, "/thumb/h0001") {
		t.Error("hidden photo must not appear in the gallery")
	}
	if !strings.Contains(b, "/thumb/h0000") {
		t.Error("visible photo should appear")
	}
}

func TestPhotosFeedPagination(t *testing.T) {
	s := newTestServer(t)
	seedPhotos(t, s, galleryPageSize+7) // 37 → page1 (30) + page2 (7)

	dec := func(path string) (tiles []photoTile, next int64) {
		rec := get(t, s, path)
		if rec.Code != http.StatusOK ||
			!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s code=%d ct=%q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		var body struct {
			Photos     []photoTile `json:"photos"`
			NextBefore int64       `json:"next_before"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return body.Photos, body.NextBefore
	}

	p1, next := dec("/api/photos")
	if len(p1) != galleryPageSize || next == 0 {
		t.Fatalf("page1 len=%d next=%d, want %d and non-zero", len(p1), next, galleryPageSize)
	}
	if p1[0].ThumbURL != "/thumb/h0036" {
		t.Errorf("page1[0] = %s, want newest /thumb/h0036", p1[0].ThumbURL)
	}
	p2, next2 := dec(fmt.Sprintf("/api/photos?before=%d", next))
	if len(p2) != 7 || next2 != 0 {
		t.Fatalf("page2 len=%d next=%d, want 7 and 0 (end)", len(p2), next2)
	}
}

func TestGalleryJSServed(t *testing.T) {
	s := newTestServer(t)
	rec := get(t, s, "/static/gallery.js")
	if rec.Code != http.StatusOK ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("gallery.js code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "IntersectionObserver") || !strings.Contains(body, "/api/photos") {
		t.Error("gallery.js missing IO infinite-scroll against /api/photos")
	}
}

func TestGalleryHeartsAndTopMode(t *testing.T) {
	s := newTestServer(t) // gate off; sessions enabled
	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	for _, h := range []string{a, b} {
		if _, _, err := s.st.InsertPhoto(store.Photo{
			ContentHash: h, MIME: "image/jpeg", UploadedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.st.UpsertSession("voter", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.st.ToggleHeart(b, "voter"); err != nil { // b: 1 heart
		t.Fatal(err)
	}

	// All view: tabs, a heart form per photo, both photos listed.
	all := get(t, s, "/gallery").Body.String()
	for _, want := range []string{
		`class="gallery-tabs"`,
		`action="/photo/` + a + `/heart"`,
		"Most loved",
		`src="/static/heart.js"`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("/gallery missing %q", want)
		}
	}
	if !strings.Contains(all, a) || !strings.Contains(all, b) {
		t.Error("/gallery should list both photos")
	}

	// Top view: only the hearted photo; the zero-heart one is excluded.
	top := get(t, s, "/gallery?sort=top").Body.String()
	if !strings.Contains(top, b) {
		t.Error("/gallery?sort=top should include the hearted photo")
	}
	if strings.Contains(top, a) {
		t.Error("/gallery?sort=top should exclude the zero-heart photo")
	}
}
