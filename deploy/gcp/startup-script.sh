#!/bin/bash
# photo-server VM provisioning. Runs on EVERY boot via
# metadata_startup_script, so every step is idempotent.
#
# Rendered by Terraform templatefile(): the only $${...} interpolations are
# the Terraform vars (domain, admin_password, release_bucket,
# backup_bucket, data_device). Every shell variable uses the brace-free
# $VAR form so the template passes it through untouched.
set -euo pipefail

DEVICE=/dev/disk/by-id/google-${data_device}
DATA_DIR=/var/lib/photo-server

echo "[photo-server-init] starting at $(date -u)"

# 1) Mount the data disk. Format ONLY when blank — this guard is the
#    linchpin of data persistence (never reformat existing originals).
if ! blkid "$DEVICE" >/dev/null 2>&1; then
  echo "[photo-server-init] no filesystem on $DEVICE; mkfs.ext4"
  mkfs.ext4 -F "$DEVICE"
fi
mkdir -p "$DATA_DIR"
DISK_UUID=$(blkid -s UUID -o value "$DEVICE")
if ! grep -q "$DISK_UUID" /etc/fstab; then
  echo "UUID=$DISK_UUID $DATA_DIR ext4 discard,defaults,nofail 0 2" >> /etc/fstab
fi
mount -a

# 2) Packages: libvips (HEIC->JPEG), sqlite3 (backup snapshot),
#    google-cloud-cli (gsutil), Caddy (auto-HTTPS reverse proxy).
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y libvips-tools sqlite3 debian-keyring debian-archive-keyring apt-transport-https curl gnupg

if ! command -v gsutil >/dev/null 2>&1; then
  curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg | gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg
  echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" > /etc/apt/sources.list.d/google-cloud-sdk.list
  apt-get update -y
  apt-get install -y google-cloud-cli
fi

if ! command -v caddy >/dev/null 2>&1; then
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -y
  apt-get install -y caddy
fi

# 3) Service user + data ownership.
id photo-server >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin photo-server
chown -R photo-server:photo-server "$DATA_DIR"

# 4) Binary + systemd unit from the release bucket. Wait up to ~5 min so
#    the upload can race the first boot (see README); self-heals on reboot.
for i in $(seq 1 30); do
  if gsutil -q stat "gs://${release_bucket}/photo-server"; then break; fi
  echo "[photo-server-init] waiting for gs://${release_bucket}/photo-server ($i/30)"
  sleep 10
done
gsutil cp "gs://${release_bucket}/photo-server" /usr/local/bin/photo-server
chmod 0755 /usr/local/bin/photo-server
gsutil cp "gs://${release_bucket}/photo-server.service" /etc/systemd/system/photo-server.service

# 5) Environment file. App binds localhost; Caddy terminates TLS in front.
#    Quoted heredoc: the substituted password is written verbatim.
install -d -m 0750 -o photo-server -g photo-server /etc/photo-server
cat > /etc/photo-server/photo-server.env <<'ENVEOF'
PHOTO_SERVER_ADDR=127.0.0.1:8080
PHOTO_SERVER_BASE_URL=https://${domain}/
PHOTO_SERVER_DATA_DIR=/var/lib/photo-server
PHOTO_SERVER_ADMIN_PASSWORD=${admin_password}
ENVEOF
chmod 0640 /etc/photo-server/photo-server.env
chown photo-server:photo-server /etc/photo-server/photo-server.env

# 6) Caddyfile: automatic HTTPS + reverse proxy to the app.
cat > /etc/caddy/Caddyfile <<'CADDYEOF'
${domain} {
    reverse_proxy 127.0.0.1:8080
}
CADDYEOF

# 7) Backup to GCS (hourly timer): a consistent SQLite snapshot + rsync of
#    the immutable originals. Doubles as the post-event archive.
cat > /usr/local/bin/photo-server-backup.sh <<'BACKUPEOF'
#!/bin/sh
set -eu
DATA=/var/lib/photo-server
BUCKET=gs://${backup_bucket}
TS=$(date -u +%Y%m%dT%H%M%SZ)
sqlite3 "$DATA/photo-server.db" ".backup '/tmp/photo-server-$TS.db'"
gsutil cp "/tmp/photo-server-$TS.db" "$BUCKET/db/photo-server-$TS.db"
rm -f "/tmp/photo-server-$TS.db"
gsutil -m rsync -r "$DATA/originals" "$BUCKET/originals"
BACKUPEOF
chmod 0755 /usr/local/bin/photo-server-backup.sh

cat > /etc/systemd/system/photo-server-backup.service <<'SVCEOF'
[Unit]
Description=photo-server backup to GCS
After=network-online.target
Wants=network-online.target
[Service]
Type=oneshot
ExecStart=/usr/local/bin/photo-server-backup.sh
SVCEOF

cat > /etc/systemd/system/photo-server-backup.timer <<'TIMEREOF'
[Unit]
Description=Hourly photo-server backup to GCS
[Timer]
OnCalendar=hourly
Persistent=true
[Install]
WantedBy=timers.target
TIMEREOF

# 8) Enable + (re)start everything.
systemctl daemon-reload
systemctl enable --now photo-server
systemctl enable --now photo-server-backup.timer
systemctl reload caddy || systemctl restart caddy

echo "[photo-server-init] complete at $(date -u)"
