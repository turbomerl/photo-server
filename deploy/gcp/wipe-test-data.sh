#!/usr/bin/env bash
# Wipe ALL guest data (photos, hearts, sessions + their blobs) from the live
# VM, leaving a pristine empty album. For clearing test data before the event
# — safe to re-run. It stops the service, deletes the SQLite DB + blob files,
# and restarts; the app recreates an empty DB (migrations re-run) on boot.
# The persistent disk, the gate password, and all config are untouched.
#
# Usage (gcloud configured; from anywhere):
#   deploy/gcp/wipe-test-data.sh          # prompts for confirmation
#   deploy/gcp/wipe-test-data.sh --yes    # no prompt (scripted)
#
# Overridable: PHOTO_SERVER_VM (default photo-server), PHOTO_SERVER_ZONE
# (default europe-west2-a).
set -euo pipefail

INSTANCE="${PHOTO_SERVER_VM:-photo-server}"
ZONE="${PHOTO_SERVER_ZONE:-europe-west2-a}"

if [ "${1:-}" != "--yes" ]; then
  echo "⚠  Permanently deletes ALL photos, hearts, and sessions on"
  echo "   ${INSTANCE} (${ZONE}). Gate password + config are kept."
  printf "   Type 'wipe' to confirm: "
  read -r ans
  [ "$ans" = "wipe" ] || { echo "Aborted."; exit 1; }
fi

# Remote script (runs on the VM; sudo for the photo-server-owned data dir).
# Single-quoted heredoc: $VARS / $(...) are expanded remotely, not locally.
read -r -d '' REMOTE <<'EOS' || true
set -e
DIR=/var/lib/photo-server
DB="$DIR/photo-server.db"
echo "before: $(sudo sqlite3 "$DB" 'SELECT COUNT(*) FROM photos;' 2>/dev/null || echo '?') photos"
sudo systemctl stop photo-server
sudo rm -f "$DB" "$DB-wal" "$DB-shm"
sudo rm -rf "$DIR"/originals/* "$DIR"/thumbs/* "$DIR"/gallery_jpegs/*
sudo systemctl start photo-server
sleep 1
echo "after:  $(curl -s localhost:8080/healthz)  ·  $(sudo find "$DIR"/originals -type f 2>/dev/null | wc -l | tr -d ' ') originals on disk"
EOS

echo "Wiping ${INSTANCE} …"
gcloud compute ssh "$INSTANCE" --zone "$ZONE" --tunnel-through-iap --quiet --command="$REMOTE"
echo "✓ Done — clean slate."
