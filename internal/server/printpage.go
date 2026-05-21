package server

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed assets/templates/print.html
var printTplBytes []byte

var tplPrint = template.Must(template.New("print").Parse(string(printTplBytes)))

type printData struct {
	SSID, PSK, BaseURL string
	WIFIQR             template.URL // data: URL for the WIFI: QR
	URLQR              template.URL // data: URL for the URL QR
	Cards              []int        // repeat per A4 (2x2 = 4 cards)
}

// handlePrintPage renders an admin-gated, print-ready HTML page with
// the WIFI-join QR + the URL QR + plain-text labels — the operator
// prints from the browser ("Save as PDF" / "Print"). No PDF library
// dependency. The OS QR camera auto-joins WPA2 from the WIFI: URI;
// the URL QR opens the app (or just-tap if captive-portal triggered).
// PRD F12: multiple per page for table cards.
func (s *Server) handlePrintPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if s.ssid == "" || s.wifiPSK == "" || s.baseURL == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta charset=utf-8><title>Print cards</title>
<body style="font:17px system-ui;padding:30px">
<p>Set <code>PHOTO_SERVER_SSID</code>, <code>PHOTO_SERVER_WIFI_PSK</code>
and <code>PHOTO_SERVER_BASE_URL</code> in the systemd unit and reload.</p>`)
		return
	}

	wifi := "WIFI:T:WPA;S:" + wifiEscape(s.ssid) + ";P:" + wifiEscape(s.wifiPSK) + ";;"
	wifiPNG, err := qrcode.Encode(wifi, qrcode.Medium, 384)
	if err != nil {
		http.Error(w, "qr error", http.StatusInternalServerError)
		return
	}
	urlPNG, err := qrcode.Encode(s.baseURL, qrcode.Medium, 384)
	if err != nil {
		http.Error(w, "qr error", http.StatusInternalServerError)
		return
	}

	p := printData{
		SSID: s.ssid, PSK: s.wifiPSK, BaseURL: s.baseURL,
		WIFIQR: dataPNG(wifiPNG),
		URLQR:  dataPNG(urlPNG),
		Cards:  []int{0, 1, 2, 3},
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := tplPrint.Execute(w, p); err != nil {
		s.log.Error("print render", "err", err)
	}
}

func dataPNG(png []byte) template.URL {
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))
}

// wifiEscape escapes the special characters in the WIFI: URI grammar
// (backslash MUST be doubled first, then ; , : " get backslash-escaped).
func wifiEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	for _, c := range []string{`;`, `,`, `:`, `"`} {
		s = strings.ReplaceAll(s, c, `\`+c)
	}
	return s
}
