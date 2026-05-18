// photo-server guest session bootstrap (kgu.14).
//
// The session token lives in an HttpOnly cookie (server-managed, not
// readable here). We keep a JS-visible copy in localStorage so that if
// iOS Private Relay / Safari ITP drops the cookie mid-event, the next
// page load re-presents the saved token and the server rebinds the
// same identity — the guest keeps their name and their uploads stay
// grouped. No build step: plain ES5, bundled, served locally.
(function () {
  "use strict";
  var KEY = "ps_session";

  function lsGet() {
    try { return window.localStorage.getItem(KEY) || ""; } catch (e) { return ""; }
  }
  function lsSet(v) {
    try { window.localStorage.setItem(KEY, v); } catch (e) { /* private mode */ }
  }

  function publish(s) {
    window.psSession = s || null;
    try {
      document.dispatchEvent(new CustomEvent("ps:session", { detail: s }));
    } catch (e) { /* old browsers: window.psSession still set */ }
  }

  var saved = lsGet();
  var req;
  if (saved) {
    // Cookie may be gone; re-present the saved token to rebind it.
    req = fetch("/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: saved }),
      credentials: "same-origin"
    });
  } else {
    // First contact: server issues a fresh token + cookie.
    req = fetch("/session", { method: "GET", credentials: "same-origin" });
  }

  req.then(function (r) { return r.json(); })
    .then(function (s) {
      if (s && s.token) lsSet(s.token);
      publish(s);
    })
    .catch(function () { publish(null); });
})();
