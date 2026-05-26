// Client-side downscale (jz9): shrink large photos in the browser before
// upload so ~150 guests on a shared ~10 Mbps link stay smooth. Decodes
// with EXIF orientation applied, scales the longest edge to MAX, and
// re-encodes JPEG at Q (~300-500KB for a typical phone photo). Falls back
// to the untouched file whenever the browser can't decode it (e.g. some
// HEIC on Android) — the server then transcodes the original.
//
// Exposes window.psResize(file) -> Promise<Blob|File>. Plain ES5, no deps,
// served locally (offline-first).
(function () {
  "use strict";

  var MAX = 2048;       // longest edge, px
  var Q = 0.82;         // JPEG quality
  var TIMEOUT = 15000;  // ms before giving up and sending the original

  function canEncode() {
    try {
      return !!window.createImageBitmap &&
        typeof document.createElement("canvas").toBlob === "function";
    } catch (e) { return false; }
  }

  window.psResize = function (file) {
    return new Promise(function (resolve) {
      var done = false;
      function fin(out) { if (!done) { done = true; resolve(out); } }

      // Only raster images; bail (send original) if we can't process it.
      if (!file || !/^image\//.test(file.type || "") || !canEncode()) {
        fin(file);
        return;
      }
      // Safety net: never block the upload forever on a decode/encode hang.
      setTimeout(function () { fin(file); }, TIMEOUT);

      var p;
      try {
        // imageOrientation:"from-image" bakes EXIF rotation into the
        // pixels, so the re-encoded JPEG (which carries no EXIF) is upright.
        p = window.createImageBitmap(file, { imageOrientation: "from-image" });
      } catch (e) { fin(file); return; }

      p.then(function (bmp) {
        var scale = Math.min(1, MAX / Math.max(bmp.width, bmp.height));
        var tw = Math.max(1, Math.round(bmp.width * scale));
        var th = Math.max(1, Math.round(bmp.height * scale));
        var c = document.createElement("canvas");
        c.width = tw; c.height = th;
        c.getContext("2d").drawImage(bmp, 0, 0, tw, th);
        if (bmp.close) { try { bmp.close(); } catch (e) { /* ignore */ } }
        c.toBlob(function (blob) {
          // Never upload something larger than the original (e.g. an
          // already-tiny image, or a PNG that bloats as JPEG-of-bigger).
          fin(blob && blob.size < file.size ? blob : file);
        }, "image/jpeg", Q);
      }).catch(function () { fin(file); });
    });
  };
})();
