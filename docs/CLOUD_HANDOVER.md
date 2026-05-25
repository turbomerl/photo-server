# Cloud Pivot — Handover

**Date:** 2026-05-25 · **Author of this note:** wrapping up the on-Dell
prototype session before development moves to the laptop.

## Decision (read first)

We tested the venue's cellular and it's **~10 Mbps — good enough**. So we
are **abandoning the local-LAN appliance** (the Dell + Ubiquiti AP +
dnsmasq + NAT + captive portal) and building a **cloud-hosted version of
the same app**. Guests will scan a single QR → open a public HTTPS URL
over their own cellular → upload/browse. No Wi-Fi to join.

Why this is a good trade: it **deletes the hardest, most fragile work**
(AP provisioning, static gateway, dnsmasq, the internet-uplink NAT, the
captive-portal saga, the Wi-Fi-join QR, and *all* the hardware-on-the-day
risk) while **keeping ~90% of what's already built** — the entire
application layer is host-agnostic.

The painful networking detours are documented in git history and in the
closed beads (kgu.5, kgu.6, kgu.3/4/7, ha1) if we ever need to revert.

---

## 0. Pulling the repo on the laptop — DO THIS FIRST (beads gotcha)

`.beads/embeddeddolt/` is **gitignored**, so a fresh clone has **no issue
DB**. You MUST bootstrap from the git-tracked JSONL **before any other
`bd` command**, or beads auto-exports against an empty DB and **deletes
`.beads/issues.jsonl`**:

```bash
git clone <remote> photo-server && cd photo-server
bd bootstrap --yes        # imports the issues from .beads/issues.jsonl — run BEFORE bd ready/prime/anything
bd ready                  # now safe
```

Recovery if you trip it: `git restore .beads/issues.jsonl && rm -rf
.beads/embeddeddolt && bd bootstrap --yes`.

Toolchain: **Go 1.22+** (the routing uses method+path patterns) and
**libvips** (`vipsthumbnail`) for HEIC. Then:

```bash
make build     # -> ./photo-server
make check     # vet + test (should be green; the tree was left clean)
make run       # local: http://localhost:8080, data in ./data
curl -fsS http://127.0.0.1:8080/healthz
```

---

## 1. What you KEEP (reusable as-is, host-agnostic)

The whole application layer ports to the cloud unchanged:

- **Upload pipeline:** multipart upload → sha256 dedup → EXIF
  orientation → async bounded libvips worker pool → web thumbnail +
  downscaled gallery JPEG. (`internal/server`, `internal/convert`,
  `internal/exif`)
- **Storage:** content-addressed blob store (sharded, atomic
  tmp+rename+fsync) + SQLite metadata. (`internal/blobstore`,
  `internal/store`)
- **Guest sessions:** 256-bit token, HttpOnly cookie + localStorage
  mirror, cookie-loss recovery, display-name. (`internal/session`)
- **UI:** Polaroid / Upload / Gallery / full-size viewer — server-rendered
  with JS progressive enhancement (works no-JS). Admin: hide / delete /
  shutdown. (`internal/server/assets`, `pages.go`, admin handlers)
- **Stack discipline:** stdlib + **two pure-Go deps** only
  (`modernc.org/sqlite`, `github.com/skip2/go-qrcode`); libvips is
  shelled out (`vipsthumbnail`), not cgo, so the binary stays portable.
- **Config + unit:** env-driven `internal/config` and the hardened
  `deploy/photo-server.service` port straight to a VPS.

## 2. What you DROP

AP provisioning (UniFi/Docker), the NM keyfile + static gateway,
dnsmasq, the NAT/uplink work, the captive portal (already removed), and
the **Wi-Fi-join QR + SSID/PSK**. The entry QR becomes a single public
URL. `deploy/dnsmasq/`, `deploy/network/`, and `deploy/INSTALL.md` are
now LAN-history — leave them or delete in the QR/cleanup task.

## 3. What you ADD (the cloud backlog — beads)

| Bead | P | What |
| --- | --- | --- |
| `photo_server-rrh` | 1 | **Host + domain + TLS.** Recommended: small VPS + persistent volume + **Caddy** for auto-HTTPS (reuses the systemd unit; app stays plain HTTP behind it). Alt: Fly.io (Docker + volume + managed TLS). |
| `photo_server-9wv` | 1 | **Persistent storage.** Keep SQLite + blob store on a **mounted volume** (boring, works). Ephemeral/serverless FS breaks both — only go there if you switch to S3-compatible object storage + a networked DB (real rework). |
| `photo_server-ycl` | 1 | **Harden for public internet.** Cookies `Secure=true` on HTTPS (see §4), request **rate-limiting**, keep the upload cap, and an **event access gate** (unguessable URL or shared event password) so the album isn't world-readable. |
| `photo_server-gj4` | 1 | **Reconcile PRD + CLAUDE.md** to the cloud architecture (they're offline-first today — see the banners). |
| `photo_server-3rz` | 2 | **Single-URL QR.** Drop the WIFI: QR + SSID/PSK; `/admin/print` just encodes `https://<domain>`. |
| `photo_server-apt` | 2 | **Runtime image with libvips** (`vipsthumbnail`) so HEIC transcodes on the host. |
| `photo_server-jz9` | 2 | **Bandwidth resilience** for shared ~10 Mbps: downscale/compress images **client-side before upload**, lazy-load gallery thumbnails. This is what keeps 150 guests on one tower smooth. |

Still-valid app-layer work (unchanged by the pivot): `dgx` Polaroid
hardening, `o5j` transcode soak, `kgu.20` originals export, `kgu.22`
slideshow, `kgu.23` hearts, `kgu.26` day-of runbook/cards. Retargeted:
`kgu.25` (on-site test → load the deployed app over venue cellular from
several phones at once), `kgu.24` (USB rsync → cloud backup of
originals + DB).

## 4. Config deltas for cloud

- `PHOTO_SERVER_BASE_URL` → `https://<your-domain>/`
- **Cookies:** `internal/session/session.go` `setCookie` sets
  `Secure=false` (was correct for HTTP LAN). On HTTPS, flip to
  `Secure=true` — gate it on an `https://` BASE_URL or a config flag.
- Drop `PHOTO_SERVER_SSID` / `PHOTO_SERVER_WIFI_PSK` (and the Wi-Fi QR
  path). `PHOTO_SERVER_ALLOWED_HOSTS` is already gone.
- Keep `PHOTO_SERVER_ADMIN_PASSWORD` (fail-closed) and
  `PHOTO_SERVER_DATA_DIR` (point it at the mounted volume).

## 5. Privacy

Photos now leave the venue to a server you run. Put the album behind an
unguessable URL or an event password (bead `ycl`), and decide a
post-event deletion plan. No third-party analytics/CDNs — that part of
the original ethos still holds.

## 6. The ~10 Mbps reality check

10 Mbps is comfortable for one phone but it's **shared** — ~150 guests
on one or two towers. Don't fight it with full-res uploads: the
client-side downscale in bead `jz9` is the single highest-leverage thing
to keep the day smooth. Heavy traffic is uploads; lazy-load the gallery
so browsing stays light.

---

*Current tree state: clean, `make check` green, everything pushed to
`main`. Start on the laptop with §0, then `bd ready`.*
