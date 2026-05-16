# Photo Server — Product Requirements Document

**Status:** Draft v0.3
**Owner:** isambardpoulson@gmail.com
**Last updated:** 2026-05-01

**Target deployment:** the owner's own wedding — one-off, single-day,
up to 150 guests. Venue is a courtyard/house + a marquee that are *not*
line-of-sight. Trusted-guest model — no moderation, no quotas, no
account creation. Encourage uploads.

**This is a one-off build.** There is no plan to sell, rent, or reuse
this beyond the single event. That means: optimise for "works on the
day," not for fleet manageability, polish, or BOM economics. Pre-event
rehearsal at the actual venue is acceptable (and expected).

---

## 1. Summary

A small, self-contained server appliance that lets a group of people in a
location with **no internet and no existing wifi** share photos. The device
broadcasts its own wifi network; people join it from a phone or laptop, open
a web page, and can upload, browse, and download photos. Everything runs
locally on the device.

## 2. Problem & Motivation

Smartphones make it trivial to take photos but hard to share them when
there's no connectivity. At weddings, festivals, expeditions, field
research sites, refugee camps, sports events, classrooms, retreats, and
remote villages, people end up trading files via Bluetooth, AirDrop silos,
or "I'll send it later" promises that never arrive. Cloud services require
the very thing that's missing: internet.

A pocket-sized box that creates a local network and a shared photo album
solves this without any infrastructure — plug it in, share a wifi name and
password, done.

## 3. Goals

- **G1.** Allow any device with a modern web browser to join the server's
  wifi and upload photos in under 60 seconds, without installing an app or
  creating an account.
- **G2.** Allow any joined device to browse and download all photos
  uploaded so far.
- **G3.** Operate fully offline — no internet, no cloud services, no
  external auth.
- **G4.** Run unattended for the duration of an event (target: 24+ hours)
  on battery or modest mains power.
- **G5.** Survive being unplugged: no data loss for photos already
  uploaded; clean restart on power-up.
- **G6.** Be deployable by a non-technical user (plug in, see a label with
  wifi name + password + URL).

## 4. Non-Goals

- Internet connectivity, remote access, or sync to cloud storage.
- Real-time chat, social-graph features, comments, likes, follower lists.
- Video uploads (initial release; revisit later).
- User accounts with passwords or OAuth.
- Mobile app — web only for v1.
- Editing, filters, or AI tagging on the device.
- Multi-event tenancy on one box (one event per device per power cycle).

## 5. Target Users & Scenarios

### The wedding (the only scenario this is being built for)

- **Scale:** up to 150 guests, assume ~120 carry a smartphone, ~60–80
  concurrently active during peak moments (speeches, first dance).
- **Duration:** ~12 hours from setup through end of reception, plus a
  buffer the next morning to grab everything before travelling.
- **Venue:** a house + courtyard, plus a marquee in the grounds. The
  two are **not line-of-sight** — there's a wall / building between
  them. Most photo activity will happen **in the marquee** (reception,
  speeches, first dance); the house/courtyard sees lighter use
  (arrivals, drinks, candid shots).
- **Trust:** guests are personal friends and family. No moderation, no
  quotas, no account creation.
- **Operator:** the owner / a friend. Technical enough to set this up
  themselves, but on the day they want it to be handled.

### Future scenarios

Out of scope. This is a one-off. Anything that would only pay off on a
second deployment (theming engine, multi-event tenancy, fleet update
mechanism, sellable installer) is **not** built.

## 5a. UI Shape (decided)

A small **mobile-first web app** served from the Dell. No app install,
no SPA framework, no build step beyond `go build`. The Go binary serves
HTML + bundled CSS + a few KB of vanilla JS for upload progress and
lazy thumbnail loading.

Two tabs at the bottom of the screen:

1. **Upload** — the default landing tab. Big primary button: *"Add
   photos."* Tapping it opens the OS photo picker (multi-select).
   Per-file progress bars; uploads survive page reload and brief
   wifi drops. A small input at the top lets the guest set a display
   name once; it's remembered on their device for the rest of the
   day. Below the button: the guest's own recent uploads, so they
   can confirm something went up.
2. **Gallery** — reverse-chronological grid of thumbnails. Tap a
   thumbnail for the full-size view with a download button. Infinite
   scroll. No filters, no search, no sort options in v1 — keep it
   "Instagram-simple".

Plus an unlinked **/admin** page (password-protected) for the operator:
storage usage, hide/delete a photo, USB export, shutdown.

Plus an unlinked **/slideshow** page (see F18) that auto-advances
through recent uploads at full screen, designed to be pinned in a
browser on a TV/projector in the marquee.

### Explicit non-features in the UI

- No upvotes / likes / reactions visible to other guests (rationale:
  popularity-contest dynamics are awkward at a wedding; the
  social-cost-to-value ratio is wrong for a one-off).
- No comments.
- No per-photo permalinks shared off-LAN — there is no off-LAN.
- No filters, tags, or albums in v1.

## 6. User Flows

### 6.1 Joining the server
1. Guest scans a **printed QR code** placed on tables / at the entrance.
   The QR encodes the wifi join (SSID + WPA2 passphrase) using the
   `WIFI:` URI scheme so iOS and Android join automatically.
2. A second QR (or the same one via captive-portal redirect) opens the
   upload page at a friendly URL (e.g. `http://photos.wedding`).
3. Once joined, the device **stays joined and stays authorised for the
   whole day**. No re-auth, no re-entering a name, no session timeout
   that throws them back to a login screen between the ceremony and the
   first dance.
4. The page loads with no spinner and no external requests.

### 6.2 Uploading
1. Guest taps **Upload**, picks photos from their gallery (multi-select).
2. Upload progress is visible per-file; uploads survive page reload, app
   backgrounding, and brief wifi drops (resume on retry).
3. On success, thumbnails appear in the gallery immediately.
4. Optional: guest enters a display name once (e.g. "Aunt Sue"), stored
   on their device, attached to all subsequent uploads that day. They
   can leave it blank — uploads still work, just shown as "Anonymous".
5. No quotas, no rate limits visible to guests. Encourage volume.

### 6.3 Browsing & downloading
1. Default view: reverse-chronological grid of thumbnails.
2. Tap a thumbnail → full-size view with download / share-on-LAN options.
3. Optional filters: by uploader name, by tag, by time window.
4. "Download all" is rate-limited and chunked so it doesn't crash the
   server on a 500-guest deployment.

### 6.4 Operator (event host)
1. Plug in the device. Within ~60 seconds the wifi is broadcasting and
   the server is reachable.
2. An admin URL (with a password printed on the device) gives access to:
   - View storage usage and battery state
   - Pause uploads
   - Hide/delete a photo
   - Export everything to a USB stick
   - Shut down cleanly

## 7. Functional Requirements

### Must-have (v1)
- **F1.** Self-hosted wifi access point (WPA2-PSK, configurable SSID / passphrase).
- **F2.** DHCP + DNS so clients can resolve a friendly hostname (e.g. `photos.wedding`).
- **F3.** Captive-portal redirect that opens the upload page on join (best-effort across iOS / Android / desktop OSes).
- **F4.** Web UI served over HTTP on the LAN. HTTPS optional and only with self-issued cert (the offline-first constraint makes public CAs unworkable).
- **F5.** Multi-photo upload from mobile browsers, JPEG/PNG/HEIC at minimum.
- **F6.** Server-side conversion of HEIC → JPEG (or a format every browser can show) for the gallery view; original preserved.
- **F7.** Thumbnail generation, kept on disk; gallery never reads originals.
- **F8.** Reverse-chronological gallery with infinite scroll or paged browsing.
- **F9.** Single-photo view + original-resolution download.
- **F10.** Per-guest optional display name, set once and remembered on the device for the rest of the day (no re-prompt).
- **F11.** **Persistent guest session** — once a device has joined the wifi and loaded the page, it stays authorised for the full event without re-auth.
- **F12.** **Printed QR codes**: one for wifi join (`WIFI:` URI), one for the upload URL. Bundled as a printable PDF the operator generates from the admin page.
- **F13.** Operator admin page protected by a device-local password.
- **F14.** USB-stick export of all originals + a manifest.
- **F15.** Clean shutdown on power button press; safe-by-default on yank.

### Should-have (v1.1+)
- **F16.** Bulk download as ZIP (couple-only, post-event).
- **F17.** **Slideshow mode** — promoted to **strongly recommended** for v1. A `/slideshow` URL that auto-refreshes/auto-advances through recent uploads at full screen, pinned in a browser on a TV/projector in the marquee. This is the project's main engagement mechanic in lieu of likes/upvotes: guests upload partly to see their photo appear on the big screen.
- **F18.** *(optional, only if cheap to build)* anonymous "heart" tap on a photo. Counts are **not shown to other guests**; surfaced only to the couple post-event as a "guest favourites" sort. Skip if it adds any meaningful complexity.

### Explicitly dropped for v1
- Album / tag grouping (deferred — keep the gallery flat and chronological).
- Public upvotes / likes / reactions visible to others.
- Comments.
- Moderation queue (trusted guests).
- Per-guest storage quotas (encourage uploads).


### Nice-to-have / later
- Video uploads with on-device transcoding.
- Multiple-device replication (mesh of photo-servers at large events).
- Light-touch face grouping done locally (no cloud).
- Read-only "guest" mode that lets people browse but not upload.

## 8. Non-Functional Requirements

- **N1. Offline-first.** Zero outbound network calls at runtime. The build pipeline may pull dependencies online; the running device must not.
- **N2. Privacy.** Photos never leave the device unless the operator explicitly exports them. No analytics, no telemetry, no error reporting that leaves the LAN.
- **N3. Capacity (target, wedding scenario).** 150 guests, ~120 active devices, ~80 concurrent at peak. Working assumption: 100 photos/guest = 15,000 photos at ~5 MB average ≈ **75 GB**. Plan for 256 GB headroom.
- **N4. Throughput (target).** 20 simultaneous uploads at typical phone-camera resolutions without browser timeouts.
- **N5. Latency.** Gallery first-paint < 2 s on a mid-range phone; thumbnail load < 200 ms once cached.
- **N6. Durability.** No data loss for any upload that the server has ack'd, even on hard power-off (use `fsync` on writes; journaled filesystem).
- **N7. Power.** Mains-powered for v1 (wedding venues have outlets). Optional UPS/power-bank to ride out brief cuts. Idle draw target < 10 W.
- **N8. Reliability.** "All-day rock-solid" — the device must run from setup (~2 h before ceremony) through end of reception with no operator intervention. Auto-restart on crash. No required reboots mid-event.
- **N9. Operability.** A non-technical operator can deploy and run the wedding end-to-end using only the printed cards and the admin web UI.
- **N10. Coverage.** Wifi covers the **marquee** (where ~95 % of the photo activity is). The courtyard / drinks reception is **explicitly not a coverage target** — photos taken there live on guests' phones and upload when they reach the marquee. Position the marquee AP near the courtyard-facing entrance so coverage bleeds into the closer half of the courtyard as a bonus.
- **N11. Security posture.** Trusted-guest LAN. Defenses focus on accidents and one curious guest, not adversaries. WPA2-PSK is sufficient.
- **N12. Internationalisation.** UI defaults to English; HEIC / non-Latin filenames must round-trip safely.

## 9. Open Questions (to resolve next)

Scenario, venue, and ownership are locked. The remaining unknowns are
hardware and stack:

1. ~~Compute~~ — **decided:** repurpose the existing Dell mini-PC running Ubuntu as the server. Owner's laptop is the fallback if the Dell turns out to be unsuitable after the rehearsal. No hardware to buy.
2. **AP hardware**: single PoE AP in the marquee. Wired backhaul from the server in the house alongside the marquee mains feed, into a PoE switch (or PoE injector) → AP. Leaning **Ubiquiti U6-Lite** for the PoE + decent client capacity at this price point; alternatives welcome.
3. **Storage**: internal NVMe vs. external USB SSD. Either is fine at 75 GB; preference is whatever makes "hand the couple the photos afterwards" simplest.
4. ~~OS / stack~~ — **decided:** Ubuntu (already on the Dell) + a single **Go binary under `systemd`**. SQLite for metadata. `libvips` (with HEIC support) for thumbnailing and HEIC → JPEG conversion. `dnsmasq` for DHCP/DNS. `opennds` for the captive-portal redirect
(the originally-named `nodogsplash` was dropped from Ubuntu 24.04;
`opennds` 10.2.0 is its maintained successor and is the "or
equivalent" — substitution decided 2026-05-16, see DEV_HANDOFF §5.2).
One process, one config file, one binary to copy.
5. **Persistent session mechanism**: long-lived `localStorage` token + cookie issued on first visit. Confirm it survives iOS Safari "Private Relay" weirdness and Android Chrome backgrounding.
6. **Captive-portal handling**: `opennds` (nodogsplash's maintained
   successor; nodogsplash is gone from Ubuntu 24.04). Accept that
   ~5 % of guests will manually open the URL.
7. **Hostname / TLS**: HTTP behind the captive portal at a friendly mDNS name. Self-signed HTTPS adds a scary browser warning to the QR-and-go flow — skip unless we *need* it.
8. **Backup**: USB stick left plugged in for periodic rsync; the couple keeps the SSD afterwards anyway. Skip RAID.
9. **Distribution**: not applicable — single device, hand-built. We just need a clean install procedure that we can re-run if the SSD dies the week before.

## 10. Hardware Sketch

To be finalised in the next discussion. Working assumption:

- **Compute + storage**: the owner's existing **Dell mini-PC running
  Ubuntu**. No hardware purchase. Confirm at rehearsal that:
  - free disk space ≥ 150 GB (75 GB photos + thumbnails + headroom);
    add an external USB SSD if not.
  - it can sustain HEIC → JPEG transcode at the speeches/first-dance
    burst (target: ~20 concurrent uploads without queue blowout).
  - it has a usable Ethernet port for backhaul to the AP switch.
  Owner's laptop is the **fallback** if the Dell fails the rehearsal.
- **Wifi — single AP, decided:**
  - **Marquee AP**: PoE-powered, dual-band, sized for ~80 concurrent
    clients. Mounted high inside the marquee, **positioned near the
    courtyard-facing entrance** so coverage bleeds into the close edge
    of the courtyard.
  - No courtyard AP. Photos taken in the courtyard live on the phone
    and upload when the guest enters the marquee. The persistent
    session means the resumed upload is automatic.
  - Single AP avoids any roaming / "no-internet warning" flapping that
    a partial-coverage two-AP setup would cause.
- **Backhaul — wired, decided:** an outdoor-rated Cat6 run from the
  server (in the house) out to the marquee, piggy-backing the **same
  trench / route as the marquee mains feed** (the marquee needs power
  anyway — one dig, two services). Server → small **PoE switch** (or
  PoE injector) → marquee AP over Ethernet. **No wireless mesh
  backhaul.**
- **Power**: standard mains at the server end; PoE for the APs if we
  pick PoE models. Optional small UPS for brown-out resilience.
- **Backup**: a USB stick left plugged in for periodic rsync.
- **Setup gear** (one-time): HDMI cable + screen + USB keyboard for
  initial install; can be put away once the box is provisioned.

The marquee AP carries the bulk of the load. The house AP exists so that
guests in the courtyard at drinks reception can still upload — it does
not need to handle peak burst.

## 11. Software Sketch (placeholder)

Likewise to be filled in after discussion. Constraints, not choices:
- Runs on Linux.
- Single binary or a very small set of services managed by `systemd`.
- Ships with versioned migrations; first-boot brings up a usable state.
- All assets (fonts, icons, JS, CSS) bundled — no CDNs, ever.
- Logs locally; no remote logging.

## 12. Success Metrics

Measured at the device, no telemetry leaves the box:
- **M1.** Time from "device powered on" to "first guest can upload" < 90 s.
- **M2.** % of uploaded photos that are viewable in the gallery within 5 s ≥ 99 %.
- **M3.** Zero photo loss across the wedding day (verified by manifest export).
- **M4.** ≥ 70 % of guests with smartphones upload at least one photo (proxy for "the QR-and-go flow actually works").
- **M5.** Zero re-auth events for any device that connected before the ceremony and stayed within wifi range.

## 13. Risks

- **R1.** Browser captive-portal behaviour is inconsistent across iOS / Android versions; a meaningful minority of guests will need to open the URL manually. Mitigation: print the URL on the QR card next to the wifi QR.
- **R2.** HEIC support varies; server-side conversion must be reliable.
- **R3.** Courtyard has no coverage by design. Risk is that some guests assume "no upload page = broken" and don't try again in the marquee. Mitigation: place QR cards at the marquee entrance (not in the courtyard) so first contact happens once they're in range; brief MC announcement.
- **R4.** Speech-time / first-dance burst — dozens of guests uploading in the same minute. Mitigation: client-side queueing with backoff; the upload API is the bottleneck to size for.
- **R5.** Heat / sustained transcoding on a fanless SBC; the device may throttle if 80 phones drop HEICs in a 5-minute window. Mitigation: pick hardware with margin, queue conversion off the upload path.
- **R6.** Persistent session token is lost (private browsing, cleared cookies) — guest re-prompted for name. Mitigation: re-prompt only for *name*, never block uploads.
- **R7.** Legal / consent: the couple are effectively the data controllers. Splash screen should carry a one-line "photos here are visible to all guests" notice they can edit.

## 14. Out of Scope (explicit)

- Public-internet-facing deployments.
- Federated / peer-to-peer photo sharing across multiple sites.
- Generative AI features.
- Paid tiers / billing.
