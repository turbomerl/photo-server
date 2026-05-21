package server

import (
	"net/http"
	"strings"
)

// captiveRedirect is the in-server captive-portal trigger (kgu.6 —
// the "cheap" version that replaces the opennds dependency).
//
// dnsmasq wildcards every DNS name to the appliance, so when a phone
// joins the wifi and the OS pings its canary URL (Apple's
// captive.apple.com, Android's connectivitycheck.gstatic.com, Windows
// ncsi.txt, …) the request lands here with a non-matching Host. We
// 302 it to baseURL — the OS sees the redirect and pops its
// "Sign in to network" sheet directly at our app. iOS users tap once;
// Android shows a banner that taps to open. No external dep.
//
// When allowed is empty (tests, dev) the middleware is a no-op.
func captiveRedirect(allowed map[string]bool, baseURL string, next http.Handler) http.Handler {
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
		http.Redirect(w, r, baseURL, http.StatusFound)
	})
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
