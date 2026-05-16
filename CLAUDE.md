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

**photo-server** is a self-contained local server that lets people on a LAN
upload, browse, and download photos in environments with no internet or
existing wifi. The server creates its own access point; clients connect to it
directly from their phones or laptops.

The product requirements live in `docs/PRD.md`. Read it before making
non-trivial changes — the offline-first constraint affects nearly every
design decision (no cloud auth, no CDNs, no third-party fonts, no telemetry).

## Status

Pre-implementation but design decisions are mostly settled:

- **Target hardware:** existing Dell mini-PC running Ubuntu (server) +
  Ubiquiti UAP-AC-LR (single AP, marquee, PoE-powered via bundled
  passive 24 V injector).
- **Stack:** single Go binary under `systemd`, SQLite for metadata,
  `libvips` for HEIC/thumbnailing, `dnsmasq` for DHCP/DNS, `nodogsplash`
  (or equivalent) for the captive portal.
- **Development is happening directly on the Dell** — see
  `docs/DEV_HANDOFF.md` for setup. Building on the same kernel / glibc /
  ext4 as the target removes a class of bugs, and the networking pieces
  can only be exercised on the box anyway.

See `docs/PRD.md` for full rationale and any decisions still open.

## Build & Test

_To be filled in once the stack is chosen._

## Architecture Overview

See `docs/PRD.md` for the current product spec. Architecture decisions
(hardware choice, OS, web framework, storage layout) will be tracked as
beads issues and summarised here once settled.

## Conventions & Patterns

- Track all work in beads (`bd`), not in markdown TODOs.
- Anything that ships on the device must run fully offline. Reject
  dependencies that phone home, fetch fonts/CSS from CDNs, or require
  account sign-in to function.
- Prefer boring, well-supported tech that survives unattended operation
  on modest ARM hardware.
