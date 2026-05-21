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
	SSID, PSK, BaseURL, Host string
	WIFIQR                   template.URL // data: URL for the WIFI: QR
	Cards                    []int        // repeat per A4 (2x2 = 4 cards)
}

// handlePrintPage renders an admin-gated, print-ready HTML card with a
// SINGLE Wi-Fi-join QR + plain-text instructions — the operator prints
// from the browser ("Save as PDF"). One QR by owner request: a single
// scan can only do one action, and joining the Wi-Fi is the step
// guests can't do manually, so the QR is the WIFI: URI; the card then
// tells them to open the URL in their browser (the captive sheet can't
// run the camera). No PDF library. PRD F12: multiple per page.
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

	// Canonical field order S;T;P (Apple's documented format); some
	// Android camera parsers only recognise a wifi-join when the SSID
	// comes first, otherwise they treat it as plain text.
	wifi := "WIFI:S:" + wifiEscape(s.ssid) + ";T:WPA;P:" + wifiEscape(s.wifiPSK) + ";;"
	wifiPNG, err := qrcode.Encode(wifi, qrcode.Medium, 384)
	if err != nil {
		http.Error(w, "qr error", http.StatusInternalServerError)
		return
	}

	p := printData{
		SSID: s.ssid, PSK: s.wifiPSK, BaseURL: s.baseURL, Host: s.appHost(),
		WIFIQR: dataPNG(wifiPNG),
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
