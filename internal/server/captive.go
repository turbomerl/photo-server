package server

import (
	"io"
	"net/http"
	"strings"
)

// captiveRedirect handles requests for hosts that aren't the app
// (kgu.6). dnsmasq wildcards every DNS name to the appliance, so a
// joined phone's OS connectivity probes (Apple's captive.apple.com,
// Android's …/generate_204, Windows' ncsi) land here.
//
// We answer those probes with the OS-expected "you have internet"
// response, so the phone **validates the network and joins cleanly** —
// no "Sign in to network" sheet and no Android "no internet, use this
// network as is?" nag (owner's choice; the printed card carries the
// "open photos.wedding" step). Any other foreign host gets a soft 302
// to the /welcome landing in case a guest navigates somewhere.
//
// When allowed is empty (tests, dev) the middleware is a no-op.
func captiveRedirect(allowed map[string]bool, target string, next http.Handler) http.Handler {
	if len(allowed) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if i := strings.IndexByte(host, ':'); i >= 0 {
			host = host[:i]
		}
		if allowed[host] {
			next.ServeHTTP(w, r)
			return
		}
		if osConnectivityProbe(w, host, r.URL.Path) {
			return // told the OS "internet is fine" → clean join
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
}

// osConnectivityProbe writes the success response a captive-detection
// probe expects, returning true if the request was one. Satisfying
// these makes phones treat the (offline) network as validated.
func osConnectivityProbe(w http.ResponseWriter, host, path string) bool {
	switch {
	// Android / ChromeOS: any …/generate_204|gen_204 → HTTP 204.
	case strings.HasSuffix(path, "/generate_204") || strings.HasSuffix(path, "/gen_204"):
		w.WriteHeader(http.StatusNoContent)
		return true
	// iOS / macOS: the exact Apple "Success" page.
	case host == "captive.apple.com" || path == "/hotspot-detect.html":
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>\n")
		return true
	// Windows: connecttest / NCSI.
	case host == "www.msftconnecttest.com" || path == "/connecttest.txt":
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "Microsoft Connect Test")
		return true
	case host == "www.msftncsi.com" || path == "/ncsi.txt":
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "Microsoft NCSI")
		return true
	}
	return false
}

// hostsSet turns the config list into the lookup map the middleware
// needs.
func hostsSet(hosts []string) map[string]bool {
	if len(hosts) == 0 {
		return nil
	}
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		m[strings.ToLower(h)] = true
	}
	return m
}
