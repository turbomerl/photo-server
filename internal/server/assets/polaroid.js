// Polaroid mode (kgu.15): tap shutter -> native camera -> the photo
// auto-uploads immediately to /upload, no review. Plain ES5, no deps,
// served locally (offline-first). The session cookie (kgu.14) rides
// along via credentials:"same-origin", so uploads are attributed.
(function () {
  "use strict";

  var input = document.getElementById("ps-shot");
  var strip = document.getElementById("ps-strip");
  if (!input || !strip) return;

  // The shared display-name field is wired by session.js.

  // --- one captured frame -> a "developing" tile -> /upload ---------
  function makeTile(objURL) {
    var li = document.createElement("li");
    li.className = "shot developing";
    var img = document.createElement("img");
    img.alt = "";
    img.src = objURL;
    var cap = document.createElement("span");
    cap.className = "cap";
    cap.textContent = "developing…";
    li.appendChild(img);
    li.appendChild(cap);
    if (strip.firstChild) strip.insertBefore(li, strip.firstChild);
    else strip.appendChild(li);
    return { li: li, img: img, cap: cap };
  }

  function send(file, tile) {
    var fd = new FormData();
    fd.append("file", file, file.name || "photo.jpg");
    fetch("/upload", {
      method: "POST",
      body: fd,
      credentials: "same-origin"
    })
      .then(function (r) {
        if (!r.ok) throw new Error("status " + r.status);
        return r.json();
      })
      .then(function (j) {
        var u = j && j.uploaded && j.uploaded[0];
        if (!u || !u.ok) throw new Error((u && u.error) || "rejected");
        tile.li.className = "shot";
        tile.cap.textContent = u.deduped ? "already shared ✓" : "shared ✓";
        if (u.thumb_url) {
          var real = new Image();
          real.onload = function () { tile.img.src = u.thumb_url; };
          real.src = u.thumb_url; // kgu.13 lazily regenerates on miss
        }
      })
      .catch(function () {
        tile.li.className = "shot failed";
        tile.cap.textContent = "tap to retry";
        tile.li.onclick = function () {
          tile.li.onclick = null;
          tile.li.className = "shot developing";
          tile.cap.textContent = "developing…";
          send(file, tile);
        };
      });
  }

  input.addEventListener("change", function () {
    var files = input.files;
    if (!files || !files.length) return;
    for (var i = 0; i < files.length; i++) {
      var f = files[i];
      var url = (window.URL || window.webkitURL).createObjectURL(f);
      send(f, makeTile(url));
    }
    // Clear so the next tap reopens the camera (even for an identical
    // shot) and fires `change` again.
    input.value = "";
  });
})();
