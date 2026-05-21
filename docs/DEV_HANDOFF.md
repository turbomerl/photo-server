# Dev Handoff — moving development to the Dell

**Purpose:** everything you (or a fresh Claude Code session) need to pick
this project up directly on the Dell mini-PC and continue building it.

**Decision (2026-05-16):** development moves from the laptop to the Dell.
Rationale below.

---

## 1. Project in 60 seconds

A one-off, self-hosted LAN photo-upload appliance for the owner's wedding.
≤150 guests, courtyard + marquee venue, no internet. The Dell broadcasts
its own wifi (via one Ubiquiti UAP-AC-LR in the marquee); guests scan a
QR code, land on a mobile web page served by the Dell, upload photos for
the day, and browse a shared gallery. A `/slideshow` URL drives a TV in
the marquee.

Full product spec: `docs/PRD.md`. Read that before changing scope.

## 2. Why develop on the Dell

- The Dell *is* the target. Building on the same kernel / glibc / ext4
  removes a whole class of "works on my machine" surprises.
- The networking pieces — `hostapd`, `dnsmasq`, `nodogsplash`, captive
  portal behaviour — can only really be exercised on the target box
  anyway.
- `libvips` HEIC behaviour and SQLite fsync semantics on the actual
  storage are what we'll ship on.
- The Dell is sitting idle and has nothing to lose.
- The Go binary is trivially built locally; no cross-compile needed.

The laptop stays as a fallback if the Dell fails the rehearsal
(see `kgu.2`).

## 3. What's already in this repo

```
photo-server/
├── AGENTS.md          # beads agent instructions (auto-generated)
├── CLAUDE.md          # Claude Code project instructions
├── .beads/            # embedded Dolt issue database (committed)
│   └── issues.jsonl   # human-readable export of issues
├── .claude/           # Claude Code hooks for beads sync
└── docs/
    ├── PRD.md         # full product spec (v0.3)
    └── DEV_HANDOFF.md # this file
```

No code yet. The first ready issues are server skeleton (`kgu.8`) and
hardware verification (`kgu.2`).

## 4. Getting the project onto the Dell

Pick **one** of these. (a) is recommended because the GitHub remote also
gives you offsite backup of the beads database — losing the wedding
photos *or the issue history* a week before the event would be bad.

### (a) Via a private GitHub repo (recommended)

On the **laptop**:

```bash
cd ~/src/photo-server
# Make sure all in-progress work is committed first
git status
git add -A && git commit -m "Snapshot before Dell handoff"

# Create a private repo and push (gh CLI)
gh repo create photo-server --private --source=. --remote=origin --push
```

On the **Dell**:

```bash
sudo apt update && sudo apt install -y git gh
gh auth login                  # personal token; use HTTPS
git clone https://github.com/<you>/photo-server.git
cd photo-server
```

### (b) Direct rsync over the LAN

```bash
# From the laptop:
rsync -av --exclude='.beads/dolt-tmp' \
  ~/src/photo-server/ \
  user@dell.local:/home/user/photo-server/
```

The `.beads/` directory is safe to copy whole; it carries the issue DB.

### (c) Direct git over ssh

```bash
# On the Dell:
mkdir -p ~/src && cd ~/src && git init --bare photo-server.git
# On the laptop:
git remote add dell user@dell.local:~/src/photo-server.git
git push dell main
# On the Dell:
git clone ~/src/photo-server.git
```

## 5. One-time Dell setup

Tested for Ubuntu 22.04 / 24.04.

### 5.1 Dev toolchain

```bash
sudo apt update
sudo apt install -y \
  build-essential pkg-config git curl \
  libvips-dev libvips-tools \
  libheif-dev libheif-examples \
  libheif-plugin-libde265 libheif-plugin-dav1d libheif-plugin-aomenc \
  sqlite3 \
  jq

# NOTE (2026-05-16, Ubuntu 24.04): libheif1/libheif-dev alone do NOT
# decode HEIC. The decoder lives in a plugin — `libheif-plugin-libde265`
# is mandatory for iPhone HEIC; dav1d/aomenc cover AVIF. Without
# libde265 the kgu.2 `vips copy sample.heic` smoke test fails (PRD R2).

# Go: install the latest stable (apt's version is usually behind).
# Get the current version + sha256 from go.dev/dl (JSON endpoint):
#   curl -fsSL 'https://go.dev/dl/?mode=json' | jq -r '.[0].version'
# As of 2026-05-16 the current stable was go1.26.3 (NOT the 1.22 this
# doc originally pinned — 4 minor versions stale). Verify the sha256.
GOVER=go1.26.3   # <-- re-check go.dev/dl; do not blindly trust this
curl -LO "https://go.dev/dl/${GOVER}.linux-amd64.tar.gz"
sudo tar -C /usr/local -xzf "${GOVER}.linux-amd64.tar.gz"
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version    # should print the version you installed
```

> Always re-check `go.dev/dl` for the current stable — this doc has
> already gone stale once (1.22 → 1.26.3). Pin to whatever is current.
> Note: the Bash shell here does not persist PATH between calls; until
> you `source ~/.bashrc` in your own shell, invoke `/usr/local/go/bin/go`.

### 5.2 Network packages (install now, configure later)

```bash
sudo apt install -y hostapd dnsmasq iw rfkill

# Captive portal: nodogsplash was DROPPED from Ubuntu 24.04 (not in apt,
# even in universe). Its maintained successor `opennds` IS in apt
# (10.2.0 on 24.04). Decision (2026-05-16): use opennds — the PRD's
# stack line already says "nodogsplash (or equivalent)". Filed as a
# bead; PRD §9.6 / stack line updated.
sudo apt install -y opennds

# CRITICAL — disable AND mask, do not merely "not enable":
# dnsmasq and opennds auto-START a systemd service on install and will
# collide with the running NetworkManager + systemd-resolved (port 53).
# "Not enabling" is insufficient — a reboot before kgu.5 exists would
# bring them up. Mask them so they cannot start until kgu.5 deliberately
# unmasks + configures them. hostapd ships masked already (idempotent).
sudo systemctl disable --now dnsmasq opennds 2>/dev/null || true
sudo systemctl mask dnsmasq hostapd opennds
```

These are needed for the network-side issues (`kgu.5`, `kgu.6`). They
are installed-but-masked on purpose; `kgu.5` owns unmasking and
configuring them. Confirm `systemctl is-active NetworkManager` still
reports `active` after this step.

> **Architecture correction (2026-05-21, kgu.5/kgu.6 sprint).** The
> Ubiquiti AP runs the wireless function itself, so **hostapd is NOT
> used** — `kgu.5`'s issue title is misleading. And `opennds` is also
> **not used**: `kgu.6` is a ~30-line in-server middleware that 302s
> foreign `Host` requests to BASE_URL, which fires the OS captive
> sheet via dnsmasq's DNS wildcard. Both packages stay installed but
> masked (idempotent, harmless). The actual gateway install is
> `deploy/INSTALL.md`; configs live in `deploy/dnsmasq/`,
> `deploy/network/`, `deploy/photo-server.{service,env.example}`.

### 5.3 Beads (issue tracker)

```bash
# Easiest: download the latest release from
# https://github.com/steveyegge/beads/releases
# Pick the linux-amd64 tarball, e.g.:
curl -LO https://github.com/steveyegge/beads/releases/latest/download/bd-linux-amd64.tar.gz
mkdir -p ~/.local/bin
tar -xzf bd-linux-amd64.tar.gz -C ~/.local/bin
chmod +x ~/.local/bin/bd
echo 'export PATH=$PATH:$HOME/.local/bin' >> ~/.bashrc
source ~/.bashrc
bd --version
```

Then, in the repo — **`bd bootstrap` FIRST, before any other bd command**:

```bash
cd ~/photo-server   # wherever you cloned it
bd bootstrap --yes  # imports the 27 issues from git-tracked issues.jsonl
bd ready            # NOW lists the same ready issues you saw on the laptop
```

> ⚠️ HARD-WON LESSON (2026-05-16). The release asset is no longer
> `bd-linux-amd64.tar.gz` and the repo moved: it is now
> `gastownhall/beads`, asset `beads_<ver>_linux_amd64.tar.gz`.
>
> More importantly: `.beads/embeddeddolt/` is **git-ignored**, so a
> fresh clone has **no issue database** — only the git-tracked
> `.beads/issues.jsonl` snapshot. You MUST run `bd bootstrap --yes`
> first; it creates the DB and imports the 27 issues with stable IDs.
>
> Running ANY other bd command first (`bd ready`, `bd prime`, `bd
> import`) against the uninitialised DB triggers bd's **auto-export
> against an empty DB, which DELETES `.beads/issues.jsonl`** — wiping
> the only copy of the issues in the working tree. Recovery if this
> happens: `git restore .beads/issues.jsonl && rm -rf
> .beads/embeddeddolt && bd bootstrap --yes`.

The `.beads/` directory is portable, but the issue history travels via
the git-committed `issues.jsonl`, NOT the (git-ignored) Dolt dir. There
is no Dolt remote on origin (`git ls-remote origin 'refs/dolt/*'` is
empty); cross-machine sync is: commit `issues.jsonl` → push → on the
other machine `git pull` then `bd bootstrap`/`bd import`.

### 5.4 Verify Dell suitability (this is `kgu.2`)

While you're at it, knock off the hardware-check issue:

```bash
df -h /                       # need >150 GB free; if not, plan a USB SSD
ip link                       # confirm a working gigabit ethernet port (e.g. eno1)
nproc && free -h              # rough sense of headroom
# Quick libvips HEIC smoke test — copy a HEIC from your phone first:
vips copy sample.heic /tmp/out.jpg
```

If any check fails, note it on `kgu.2` and we re-plan that issue before
starting build work.

**Status (2026-05-16):** disk (235 GB free on `/`), NIC (`eno1`
gigabit), CPU/RAM (8 cores / 15 GiB) and a clean Ubuntu 24.04.4 all
PASS — recorded on `kgu.2`. The single-file HEIC smoke test runs once a
real phone HEIC is supplied. The acceptance criteria also call for a
**~20-concurrent HEIC→JPEG soak test**; that is rehearsal-grade and
tracked as its own follow-up bead (do it under `kgu.25` conditions),
so `kgu.2` is closed on the hardware checks, not the soak.

## 6. Where to start

`bd ready` will show four things with no blockers. The natural ordering:

1. **`kgu.2`** — Confirm Dell suitability (do this as part of §5.4 above).
2. **`kgu.8`** — Go service skeleton + systemd unit. Pure code, no
   networking yet. Lays the foundation for everything else in the
   server-core group (`kgu.9`–`kgu.14`).
3. **`kgu.1`** — Procurement is already partly done (AP ordered);
   cables next. Independent of code.

The single **P0** is `kgu.25` (venue rehearsal), gated on hardware
install + network + UI + slideshow. Don't ship without it.

## 7. Working conventions (reminder)

- Track all work in beads (`bd ready`, `bd update <id> --claim`,
  `bd close <id>`). No markdown TODOs.
- At end of session, to persist issue state correctly:

  ```bash
  bd export -o .beads/issues.jsonl    # NOT bare `bd export` — that
                                      # prints to stdout, a no-op!
  git add .beads/issues.jsonl <other files>
  git commit --no-verify -m "..."     # --no-verify: the beads
                                      # pre-commit hook otherwise
                                      # re-stages a *throttled stale*
                                      # auto-export, clobbering this
  git push
  # verify:
  git show HEAD:.beads/issues.jsonl | jq -r 'select(.id=="<id>").status'
  ```

  Why: bd's *auto*-export (the thing that normally maintains the file)
  is throttled ~60s, so a commit straight after `bd close` snapshots a
  *stale* JSONL (issue still `in_progress`). This bit kgu.2/kgu.8/kgu.9
  before the cause was understood. Explicit `bd export -o <file>`
  bypasses the throttle; `--no-verify` stops the pre-commit hook
  re-clobbering it. `bd dolt push` is a no-op here (no Dolt remote on
  origin) — issue history syncs via the git-committed `issues.jsonl`.
  See `CLAUDE.md` for the full protocol.
- Offline-first applies even to dev: no fonts/JS/CSS from CDNs in the
  shipped binary. Dev-time tooling can use the internet freely.

## 8. Useful commands cheat-sheet

```bash
# Beads
bd ready                       # what can I work on now?
bd show <id>                   # full detail of an issue
bd update <id> --claim         # take ownership
bd note <id> "..."             # append a working note
bd close <id>                  # mark done
bd dolt push                   # sync issue DB to git remote

# Building (once kgu.8 exists)
go build ./cmd/photo-server
sudo systemctl restart photo-server
journalctl -u photo-server -f

# Networking (later — don't run before kgu.5 is designed)
sudo systemctl status hostapd dnsmasq nodogsplash
```

## 9. If something looks off

The PRD (§9) lists the design decisions still open and the rationale for
the ones already taken. If a tool's behaviour on the Dell pushes back on
a decision (e.g. `nodogsplash` is missing on your Ubuntu version, or
HEIC transcode is slower than expected), file a new bead and update the
PRD rather than silently working around it.
