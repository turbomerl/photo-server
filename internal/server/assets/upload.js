// Upload page (kgu.16): pick photos from the phone, queue them with a
// concurrency cap + retry/backoff, show per-file progress, and keep
// the "your recent uploads" grid fresh. Plain ES5, no deps, served
// locally. The session cookie (kgu.14) rides along via
// credentials:"same-origin"; the name field is wired by session.js.
//
// Browser reality: File bytes cannot survive a page reload (no API to
// re-read a picked file without re-selection). What persists in
// localStorage is the set of already-uploaded file signatures, so a
// re-pick of the same batch skips done items; the server also dedups
// by content hash, so any retry is idempotent. The in-memory queue
// keeps running (with backoff) while the tab is backgrounded.
(function () {
  "use strict";

  var input = document.getElementById("up-input");
  var queueEl = document.getElementById("up-queue");
  var recentEl = document.getElementById("up-recent");
  if (!input || !queueEl || !recentEl) return;

  var DONE_KEY = "ps_uploaded";
  var MAX_CONCURRENT = 3;
  var MAX_RETRIES = 4;

  // jz9: downscale large photos in the browser before upload; passthrough
  // if resize.js didn't load.
  var resize = window.psResize || function (f) { return Promise.resolve(f); };

  function doneSet() {
    try { return JSON.parse(localStorage.getItem(DONE_KEY) || "{}"); }
    catch (e) { return {}; }
  }
  function markDone(sig) {
    try {
      var d = doneSet();
      d[sig] = 1;
      localStorage.setItem(DONE_KEY, JSON.stringify(d));
    } catch (e) { /* private mode: fine, server still dedups */ }
  }
  function sigOf(f) {
    return [f.name, f.size, f.lastModified || 0].join("|");
  }

  function dropEmpty() {
    var e = document.getElementById("up-empty");
    if (e && e.parentNode) e.parentNode.removeChild(e);
  }
  function addRecentTile(thumbURL, hash) {
    dropEmpty();
    var li = document.createElement("li");
    li.className = "cell";
    var a = document.createElement("a");
    a.href = "/p/" + hash;
    var img = document.createElement("img");
    img.alt = "";
    img.loading = "lazy";
    img.src = thumbURL;
    a.appendChild(img);
    li.appendChild(a);
    if (recentEl.firstChild) recentEl.insertBefore(li, recentEl.firstChild);
    else recentEl.appendChild(li);
  }

  // --- queue --------------------------------------------------------
  var queue = [];
  var active = 0;

  function makeRow(name) {
    var li = document.createElement("li");
    li.className = "qrow";
    li.innerHTML =
      '<span class="qname"></span><span class="qbar"><i></i></span>' +
      '<span class="qstate">queued</span>';
    li.querySelector(".qname").textContent = name;
    queueEl.appendChild(li);
    return {
      el: li,
      bar: li.querySelector(".qbar i"),
      state: li.querySelector(".qstate")
    };
  }

  function enqueue(file) {
    var sig = sigOf(file);
    var row = makeRow(file.name || "photo");
    if (doneSet()[sig]) {
      row.state.textContent = "already shared ✓";
      row.bar.style.width = "100%";
      row.el.className = "qrow done";
      return;
    }
    queue.push({ file: file, sig: sig, row: row, tries: 0 });
    pump();
  }

  function pump() {
    while (active < MAX_CONCURRENT && queue.length) {
      send(queue.shift());
    }
  }

  function send(item) {
    active++;
    item.row.state.textContent = "preparing…";
    item.row.el.className = "qrow";

    resize(item.file).then(function (blob) {
      item.row.state.textContent = "uploading…";

      var fd = new FormData();
      fd.append("file", blob, item.file.name || "photo.jpg");
      var xhr = new XMLHttpRequest();
      xhr.open("POST", "/upload");
      xhr.withCredentials = true;

      xhr.upload.onprogress = function (e) {
        if (e.lengthComputable) {
          item.row.bar.style.width = Math.round((e.loaded / e.total) * 100) + "%";
        }
      };
      xhr.onload = function () {
        active--;
        var ok = false, u = null;
        try {
          var j = JSON.parse(xhr.responseText);
          u = j && j.uploaded && j.uploaded[0];
          ok = xhr.status >= 200 && xhr.status < 300 && u && u.ok;
        } catch (e) { /* ok stays false */ }
        if (ok) {
          markDone(item.sig);
          item.row.bar.style.width = "100%";
          item.row.state.textContent = u.deduped ? "already shared ✓" : "shared ✓";
          item.row.el.className = "qrow done";
          if (u.thumb_url) addRecentTile(u.thumb_url, u.hash);
          setTimeout(function () {
            if (item.row.el.parentNode) item.row.el.parentNode.removeChild(item.row.el);
          }, 2500);
        } else if (xhr.status === 415) {
          item.row.state.textContent = "not an image";
          item.row.el.className = "qrow failed";
        } else {
          retry(item);
        }
        pump();
      };
      xhr.onerror = function () { active--; retry(item); pump(); };
      xhr.send(fd);
    });
  }

  function retry(item) {
    item.tries++;
    if (item.tries > MAX_RETRIES) {
      item.row.state.textContent = "failed — tap to retry";
      item.row.el.className = "qrow failed";
      item.row.el.onclick = function () {
        item.row.el.onclick = null;
        item.tries = 0;
        queue.push(item);
        pump();
      };
      return;
    }
    var backoff = Math.min(1000 * Math.pow(2, item.tries - 1), 15000);
    item.row.state.textContent = "retrying…";
    item.row.el.className = "qrow retry";
    setTimeout(function () { queue.push(item); pump(); }, backoff);
  }

  input.addEventListener("change", function () {
    var files = input.files;
    if (files) {
      for (var i = 0; i < files.length; i++) enqueue(files[i]);
    }
    input.value = ""; // allow re-picking the same files
  });
})();
