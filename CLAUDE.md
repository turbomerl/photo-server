# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->


## Project Overview

**photo-server** is a single Go binary that lets wedding guests upload,
browse, download, and "heart" photos from their phones over a public
HTTPS URL. It runs **cloud-hosted** (one small VM); guests reach it over
their own cellular — no app to install, no account to create. A shared
**event password** (baked into a QR) gates the album.

> **History:** this began as an offline **LAN appliance** (its own Wi-Fi
> AP, dnsmasq, captive portal, a Dell mini-PC). It **pivoted to cloud on
> 2026-05-25** because the venue has ~10 Mbps cellular — the networking
> layer was dropped and the application layer carried over unchanged.
> `docs/CLOUD_HANDOVER.md` records the pivot. In `docs/PRD.md` the
> offline/networking requirements are historical; the rest still holds.

Target: the owner's wedding — one-off, single day, ≤150 guests,
trusted-guest model (no moderation/quotas/accounts; uploads encouraged).

## Status — live in production

Deployed and verified behind Caddy auto-HTTPS on a GCE VM. Shipped:
upload (multipart, sha256 dedup, EXIF, libvips HEIC→JPEG via an async
worker pool) · **client-side downscale before upload** (2048px/q0.82) ·
gallery (reverse-chrono + lazy thumbs) + full-size viewer · **anonymous
hearts + a "Most loved" leaderboard** · admin (hide/delete/shutdown +
print-QR) · **event access gate + single auto-login QR** · guest sessions
(cookie + localStorage) · hourly **GCS backup** of originals + DB ·
wedding-theme UI with **self-hosted fonts**.

Remaining work is in `bd ready` (e.g. `ycl` gate rate-limiting, `kgu.25`
on-site rehearsal, `kgu.26` day-of cards/runbook).

## Build & Test

Single Go binary; **stdlib + two pure-Go deps** (`modernc.org/sqlite`,
`github.com/skip2/go-qrcode`). `libvips` (`vipsthumbnail`) is shelled out
for HEIC, never linked, so the binary stays a portable single artifact.
Go 1.26+ (toolchain pinned in `go.mod`); on the dev laptop `go` lives at
`~/sdk/go/bin` (official go.dev archive).

```bash
make build        # -> ./photo-server (host binary)
make build-linux  # -> ./photo-server-linux-amd64 (static linux/amd64, for the VM)
make test         # go test ./...
make vet
make check        # vet + test (pre-commit gate)
make run          # local: http://localhost:8080, data dir ./data
```

Health check: `curl -fsS http://127.0.0.1:8080/healthz` → `{"status":"ok",...}`.

Layout:

- `cmd/photo-server/` — entrypoint: config, logger, graceful shutdown.
- `internal/config/` — env-only config (`PHOTO_SERVER_*`), honours systemd
  `$STATE_DIRECTORY`. Key vars: `BASE_URL` (https in prod), `ADMIN_PASSWORD`,
  `ACCESS_PASSWORD` (event gate), `DATA_DIR`.
- `internal/server/` — HTTP routing, pages, API, upload, gallery, admin,
  the access gate (`access.go`), and hearts. CSS/JS/templates/fonts/images
  are embedded via `//go:embed`.
- `internal/store/` — SQLite (schema + numbered migrations);
  `internal/blobstore/` — content-addressed originals/thumbs/gallery JPEGs.
- `deploy/photo-server.service` — hardened systemd unit.
- `deploy/gcp/` — **Terraform** for the cloud deploy (VM + persistent disk
  + Caddy + GCS); runbook in `deploy/gcp/README.md`.

## Architecture (cloud, as built)

- **Host:** Compute Engine VM (Ubuntu) + persistent disk (holds SQLite +
  the blob store) + a reserved static IP. **Caddy** terminates HTTPS
  (Let's Encrypt) and reverse-proxies to the app on localhost:8080.
  Provisioned by the Terraform in `deploy/gcp/`; event-window lifecycle
  (provision → event → `terraform destroy`, with the backup bucket
  retained).
- **Entry / auth:** one QR → `https://<domain>/?k=<event-password>` → the
  gate validates the key, sets an HttpOnly cookie, and strips the key.
  Account-less; the password is the event-level gate (PRD N11 threat
  model). No Wi-Fi join, no captive portal.
- **Storage / durability:** content-addressed blob store + SQLite on the
  mounted disk; an hourly systemd timer backs up `originals/` + a SQLite
  snapshot to a versioned GCS bucket.

## Conventions & Patterns

- Track all work in beads (`bd`), not markdown TODOs.
- **The app makes no third-party fetches.** Fonts, CSS, and JS are
  self-hosted/embedded — no CDNs, no web fonts, no telemetry, no
  third-party auth. (Guests use their own connectivity to *reach* the app;
  the app itself pulls nothing external.) This is the surviving core of the
  original offline-first ethos.
- Keep the dependency set tiny and pure-Go so the binary stays one static
  artifact; `libvips` is the only external tool, and it's shelled out.
- Prefer boring, well-supported tech; the app must survive an unattended
  all-day event on a single modest VM.
