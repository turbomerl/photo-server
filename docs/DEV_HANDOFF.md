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
  libvips-dev libheif-dev libheif-examples \
  sqlite3 \
  jq

# Go: install the latest stable (apt's version is usually behind)
curl -LO https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version    # should print go1.22.x
```

> Check `go.dev/dl` for the current version when you actually do this —
> pin to the latest stable rather than 1.22 if a newer one is out.

### 5.2 Network packages (install now, configure later)

```bash
sudo apt install -y hostapd dnsmasq iw rfkill
# Captive portal — nodogsplash is in apt on 22.04+; if not, build from source
sudo apt install -y nodogsplash || echo "fallback: build nodogsplash from source"
```

These are needed for the network-side issues (`kgu.5`, `kgu.6`). Don't
enable / start them yet — they'll fight whatever NetworkManager is
currently doing on the Dell. Configuration is its own beads ticket.

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

Then, in the repo:

```bash
cd ~/photo-server   # wherever you cloned it
bd ready            # should list the same ready issues you saw on the laptop
```

The `.beads/` directory is portable; the issue history travels with the
repo. If you've set up the GitHub remote, `bd dolt push` syncs the beads
DB so both machines stay in step.

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
- At end of session: `git push && bd dolt push` (see `CLAUDE.md` for the
  full session-close protocol — beads enforces it via hooks).
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
