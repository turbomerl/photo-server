// Gallery (kgu.17): the first page is server-rendered (works with JS
// disabled). This adds reverse-chronological infinite scroll and
// IntersectionObserver-based lazy image loading with skeleton
// placeholders. Plain ES5, no deps, served locally.
(function () {
  "use strict";

  var grid = document.getElementById("gal-grid");
  var sentinel = document.getElementById("gal-sentinel");
  if (!grid || !sentinel) return;

  var cursor = parseInt(sentinel.getAttribute("data-next-before"), 10) || 0;

  // No IntersectionObserver (old browser): the server-rendered first
  // page still shows; just no infinite scroll. Acceptable degradation.
  if (!("IntersectionObserver" in window) || !cursor) return;

  // Reveal images only as their tile nears the viewport.
  var lazy = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) {
      if (!e.isIntersecting) return;
      var img = e.target;
      lazy.unobserve(img);
      img.onload = function () {
        if (img.parentNode) img.parentNode.classList.remove("skeleton");
      };
      img.src = img.getAttribute("data-src");
    });
  }, { rootMargin: "400px" });

  function appendTile(p) {
    var li = document.createElement("li");
    li.className = "cell skeleton";
    var img = document.createElement("img");
    img.alt = "";
    img.setAttribute("data-src", p.thumb_url);
    li.appendChild(img);
    grid.appendChild(li);
    lazy.observe(img);
  }

  var loading = false;
  var io = new IntersectionObserver(function (entries) {
    if (entries[0].isIntersecting) loadMore();
  }, { rootMargin: "600px" });
  io.observe(sentinel);

  function stop() {
    io.disconnect();
    if (sentinel.parentNode) sentinel.parentNode.removeChild(sentinel);
  }

  function loadMore() {
    if (loading || !cursor) return;
    loading = true;
    fetch("/api/photos?before=" + cursor, { credentials: "same-origin" })
      .then(function (r) { return r.json(); })
      .then(function (j) {
        var list = (j && j.photos) || [];
        list.forEach(appendTile);
        cursor = (j && j.next_before) || 0;
        loading = false;
        if (!cursor) stop();
        // Tall screen: the sentinel may still be visible — keep going.
        else if (isVisible(sentinel)) loadMore();
      })
      .catch(function () {
        // Transient: let the next scroll re-trigger.
        loading = false;
      });
  }

  function isVisible(el) {
    var r = el.getBoundingClientRect();
    return r.top < (window.innerHeight + 600) && r.bottom > 0;
  }
})();
