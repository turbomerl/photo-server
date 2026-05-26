package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.httpSrv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestPagesRenderWithThreeTabNav(t *testing.T) {
	s := newTestServer(t)

	for _, tc := range []struct{ path, active string }{
		{"/", "polaroid"},
		{"/upload", "upload"},
		{"/gallery", "gallery"},
	} {
		rec := get(t, s, tc.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s Content-Type = %q", tc.path, ct)
		}
		b := rec.Body.String()
		// All three tabs present as links on every page (Gallery is
		// reachable with JS disabled).
		for _, href := range []string{`href="/"`, `href="/upload"`, `href="/gallery"`} {
			if !strings.Contains(b, href) {
				t.Errorf("GET %s missing nav link %s", tc.path, href)
			}
		}
		if !strings.Contains(b, `aria-current="page"`) {
			t.Errorf("GET %s has no active tab", tc.path)
		}
		// Offline-first (PRD N1): no external asset origins anywhere.
		if strings.Contains(b, "http://") || strings.Contains(b, "https://") {
			t.Errorf("GET %s references an external URL (must be offline)", tc.path)
		}
	}
}

func TestIndexIsPolaroidWithCaptureInput(t *testing.T) {
	s := newTestServer(t)
	b := get(t, s, "/").Body.String()
	if !strings.Contains(b, "<title>Snap") {
		t.Error("/ is not the Polaroid (Snap) page")
	}
	if !strings.Contains(b, `id="ps-shot"`) || !strings.Contains(b, "capture=") {
		t.Error("Polaroid page missing the <input capture> shutter")
	}
	if !strings.Contains(b, `class="tab is-active"`) {
		t.Error("Polaroid tab not marked active on /")
	}
	if !strings.Contains(b, `src="/static/polaroid.js"`) {
		t.Error("Polaroid page does not include polaroid.js")
	}
}

func TestGalleryNeedsNoJS(t *testing.T) {
	s := newTestServer(t)
	b := get(t, s, "/gallery").Body.String()
	// The gallery shell must not depend on the Polaroid script.
	if strings.Contains(b, "/static/polaroid.js") {
		t.Error("gallery page should not load polaroid.js")
	}
	if !strings.Contains(b, "Gallery") {
		t.Error("gallery page content missing")
	}
}

func TestStaticAssets(t *testing.T) {
	s := newTestServer(t)

	css := get(t, s, "/static/app.css")
	if css.Code != http.StatusOK ||
		!strings.HasPrefix(css.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("app.css: code=%d ct=%q", css.Code, css.Header().Get("Content-Type"))
	}
	// Offline-first (PRD N1): no external fetches. Fonts are self-hosted
	// (url(/static/fonts/…)); the only "http" left is the SVG namespace
	// inside the grain data-URI, which is never fetched.
	cb := css.Body.String()
	for _, ext := range []string{"googleapis", "gstatic", "url(http", `url("http`, `url('http`} {
		if strings.Contains(cb, ext) {
			t.Errorf("app.css references external resource %q (must be offline)", ext)
		}
	}

	js := get(t, s, "/static/polaroid.js")
	if js.Code != http.StatusOK ||
		!strings.HasPrefix(js.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("polaroid.js: code=%d ct=%q", js.Code, js.Header().Get("Content-Type"))
	}
	if !strings.Contains(js.Body.String(), "/upload") {
		t.Error("polaroid.js does not post to /upload")
	}
}

func TestUnknownPathStill404(t *testing.T) {
	s := newTestServer(t)
	if rec := get(t, s, "/definitely-not-a-route"); rec.Code != http.StatusNotFound {
		t.Fatalf("GET /definitely-not-a-route = %d, want 404 (root must not be a catch-all)", rec.Code)
	}
}
