// Hearts (kgu.23): progressive enhancement over the per-photo
// <form class="heart"> that already works without JS (POST → redirect).
// This intercepts the submit, toggles via fetch, and updates every copy
// of that photo's heart control on the page (grid tile + lightbox) so
// they stay in sync. Plain ES5, no deps, served locally.
(function () {
  "use strict";

  function paint(form, count, hearted) {
    form.classList.toggle("on", !!hearted);
    var btn = form.querySelector("button");
    if (btn) btn.setAttribute("aria-pressed", hearted ? "true" : "false");
    var hc = form.querySelector(".hc");
    if (hc) hc.textContent = count;
  }

  // Update grid tile + lightbox copies for the same photo at once.
  function syncAll(hash, count, hearted) {
    var forms = document.querySelectorAll('form.heart[data-hash="' + hash + '"]');
    for (var i = 0; i < forms.length; i++) paint(forms[i], count, hearted);
  }
  window.PSHeartSync = syncAll;

  document.addEventListener("submit", function (e) {
    var form = e.target;
    if (!form || !form.classList || !form.classList.contains("heart")) return;
    e.preventDefault();
    if (form.dataset.busy) return;
    form.dataset.busy = "1";
    var hash = form.getAttribute("data-hash");
    fetch(form.getAttribute("action"), {
      method: "POST",
      headers: { Accept: "application/json" },
      credentials: "same-origin"
    })
      .then(function (r) { return r.ok ? r.json() : Promise.reject(); })
      .then(function (j) { syncAll(hash, j.count, j.hearted); })
      .catch(function () { /* leave UI as-is; a reload reconciles */ })
      .then(function () { delete form.dataset.busy; });
  });
})();
