// Full-size lightbox (kgu.18). Progressive enhancement over the
// gallery grid: thumbnails are real <a href="/p/<hash>"> links, so
// without JS they open the server-rendered photo page. With JS this
// intercepts the click and opens an in-page overlay with swipe /
// arrow-key navigation across the currently-loaded gallery tiles.
// Plain ES5, no deps, served locally.
(function () {
  "use strict";

  var grid = document.getElementById("gal-grid");
  if (!grid) return;

  // Build the overlay once, lazily.
  var ov, ovImg, ovName, ovDl, idx = -1;

  function tiles() {
    return grid.querySelectorAll("a[href^='/p/']");
  }
  function hashOf(a) {
    return a.getAttribute("href").slice(3); // strip "/p/"
  }

  function build() {
    ov = document.createElement("div");
    ov.className = "lb";
    ov.setAttribute("role", "dialog");
    ov.setAttribute("aria-label", "Photo");
    ov.innerHTML =
      '<button class="lb-x" aria-label="Close">✕</button>' +
      '<button class="lb-nav lb-prev" aria-label="Previous">‹</button>' +
      '<img class="lb-img" alt="">' +
      '<button class="lb-nav lb-next" aria-label="Next">›</button>' +
      '<div class="lb-bar"><span class="lb-name"></span>' +
      '<a class="lb-dl" download>⤓ Download original</a></div>';
    ovImg = ov.querySelector(".lb-img");
    ovName = ov.querySelector(".lb-name");
    ovDl = ov.querySelector(".lb-dl");
    document.body.appendChild(ov);

    ov.querySelector(".lb-x").onclick = close;
    ov.querySelector(".lb-prev").onclick = function () { step(-1); };
    ov.querySelector(".lb-next").onclick = function () { step(1); };
    ov.addEventListener("click", function (e) { if (e.target === ov) close(); });

    // Touch swipe.
    var x0 = null;
    ov.addEventListener("touchstart", function (e) {
      x0 = e.touches[0].clientX;
    }, { passive: true });
    ov.addEventListener("touchend", function (e) {
      if (x0 === null) return;
      var dx = e.changedTouches[0].clientX - x0;
      x0 = null;
      if (Math.abs(dx) > 40) step(dx < 0 ? 1 : -1);
    });
  }

  function show(i) {
    var list = tiles();
    if (i < 0 || i >= list.length) return;
    idx = i;
    var a = list[i];
    var h = hashOf(a);
    ovImg.src = "/photo/" + h;
    var nm = a.getAttribute("data-name") || "";
    ovName.textContent = nm ? "Shared by " + nm : "Shared anonymously";
    ovDl.href = "/original/" + h;
    ov.querySelector(".lb-prev").style.visibility = i > 0 ? "" : "hidden";
    ov.querySelector(".lb-next").style.visibility =
      i < list.length - 1 ? "" : "hidden";
  }

  function step(d) { show(idx + d); }

  function open(i) {
    if (!ov) build();
    ov.classList.add("is-open");
    document.documentElement.style.overflow = "hidden";
    document.addEventListener("keydown", onKey);
    // Back button / swipe-back closes the overlay, not the page.
    try { history.pushState({ lb: 1 }, ""); } catch (e) {}
    show(i);
  }

  function close() {
    if (!ov) return;
    ov.classList.remove("is-open");
    document.documentElement.style.overflow = "";
    document.removeEventListener("keydown", onKey);
    if (history.state && history.state.lb) {
      try { history.back(); } catch (e) {}
    }
  }

  function onKey(e) {
    if (e.key === "Escape") close();
    else if (e.key === "ArrowLeft") step(-1);
    else if (e.key === "ArrowRight") step(1);
  }

  window.addEventListener("popstate", function () {
    if (ov && ov.classList.contains("is-open")) {
      ov.classList.remove("is-open");
      document.documentElement.style.overflow = "";
      document.removeEventListener("keydown", onKey);
    }
  });

  // Delegate clicks on grid tiles.
  grid.addEventListener("click", function (e) {
    var a = e.target.closest ? e.target.closest("a[href^='/p/']") : null;
    if (!a || e.metaKey || e.ctrlKey || e.shiftKey || e.button) return;
    var list = tiles(), i = -1;
    for (var k = 0; k < list.length; k++) {
      if (list[k] === a) { i = k; break; }
    }
    if (i < 0) return;
    e.preventDefault();
    open(i);
  });
})();
