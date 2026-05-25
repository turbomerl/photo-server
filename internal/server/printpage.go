package server

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"net/url"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed assets/templates/print.html
var printTplBytes []byte

var tplPrint = template.Must(template.New("print").Parse(string(printTplBytes)))

type printData struct {
	BaseURL  string
	Host     string
	EntryURL string
	Password string
	QR       template.URL // data: URL for the entry QR
	Cards    []int        // repeat per A4 (2x2 = 4 cards)
}

// handlePrintPage renders an admin-gated, print-ready HTML card with a
// single QR that opens the album (3rz). The QR encodes the public
// BASE_URL with the shared event password baked in as ?k=, so a guest
// scan lands already through the gate (the server validates the key,
// sets the access cookie, and strips it from the URL). The card also
// prints the URL + password for anyone typing it. The operator prints
// from the browser ("Save as PDF"). PRD F12: multiple per page.
func (s *Server) handlePrintPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if s.baseURL == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta charset=utf-8><title>Print cards</title>
<body style="font:17px system-ui;padding:30px">
<p>Set <code>PHOTO_SERVER_BASE_URL</code> (and, to gate the album,
<code>PHOTO_SERVER_ACCESS_PASSWORD</code>) and reload.</p>`)
		return
	}

	entry := s.entryURL()
	png, err := qrcode.Encode(entry, qrcode.Medium, 384)
	if err != nil {
		http.Error(w, "qr error", http.StatusInternalServerError)
		return
	}

	p := printData{
		BaseURL:  s.baseURL,
		Host:     s.appHost(),
		EntryURL: entry,
		Password: s.accessPassword,
		QR:       dataPNG(png),
		Cards:    []int{0, 1, 2, 3},
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := tplPrint.Execute(w, p); err != nil {
		s.log.Error("print render", "err", err)
	}
}

// entryURL is the URL printed/encoded on the card: BASE_URL with the
// access password baked in as ?k= (when set) so a scan lands the guest
// already through the gate.
func (s *Server) entryURL() string {
	if s.accessPassword == "" {
		return s.baseURL
	}
	u, err := url.Parse(s.baseURL)
	if err != nil {
		return s.baseURL
	}
	q := u.Query()
	q.Set("k", s.accessPassword)
	u.RawQuery = q.Encode()
	return u.String()
}

func dataPNG(png []byte) template.URL {
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))
}
