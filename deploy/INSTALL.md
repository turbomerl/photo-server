# Bench bring-up — Dell + UAP-AC-LR + printable QR

End-to-end prototype install. Run on the Dell.

Architecture (corrected from kgu.5 issue text — recorded in
DEV_HANDOFF §9): the Ubiquiti AP runs the wireless function itself,
so **hostapd is NOT used**. The Dell is the wired L2-gateway —
static IP on `eno1`, dnsmasq for DHCP+DNS, photo-server under
systemd. The "captive-portal trigger" is built into photo-server
(no `opennds`).

```
Dell eno1 ─cat6─ [24V passive PoE injector] ─cat6─ UAP-AC-LR ((wifi)) ─ guests
   192.168.50.1/24            (no IP)              (AP-mode bridge)        DHCP .10–.200
```

## 0. Build the binary (prereq)

The install steps below all assume `./photo-server` exists in the
repo root. Build it once up front — Go must be on `PATH` (it's at
`/usr/local/go/bin` after kgu.5 / DEV_HANDOFF §5.1; `source ~/.bashrc`
if a fresh shell can't find it).

```bash
cd /home/isambard-poulson/src/photo-server
git pull --rebase
make build         # → ./photo-server
./photo-server --help 2>&1 | head -1 || ls -l photo-server
```

## 1. AP-side: provision the UniFi AP

The UAP-AC-LR is a **UniFi** AP: no standalone web UI, and **WiFiman
cannot configure it**. You set its SSID once with the free **UniFi
Network controller** software; afterwards the AP keeps broadcasting on
its own and the controller can be stopped (it is NOT needed during the
event).

Wiring: bundled **24V passive PoE injector** → mains; injector **POE**
port → AP **MAIN**; injector **LAN** port → Dell `eno1`.

Pick a controller host:

- **Laptop (Mac/Windows) — easiest.** Install "UniFi Network Server"
  from <https://ui.com/download/unifi>, put the laptop + AP on the
  same switch/LAN, open the controller, **adopt** the AP, create one
  WPA2-Personal SSID, then move the AP's LAN cable back to the Dell.
- **On the Dell.** Do §2 (gateway) FIRST so the AP gets a
  `192.168.50.x` DHCP lease, then install the UniFi Network controller
  on the Dell, browse `https://localhost:8443`, **adopt** the AP, set
  the SSID.

Either way, create exactly one WPA2-Personal SSID — **matching the env
file in §3**: name `photo-server`, passphrase `photos2026`.

## 2. Network — install (one-time)

```bash
cd /home/isambard-poulson/src/photo-server

# Unmask + install dnsmasq config (kgu.5 left it masked).
sudo systemctl unmask dnsmasq
sudo install -m 0644 -D deploy/dnsmasq/photo-server.conf \
     /etc/dnsmasq.d/photo-server.conf

# Pin eno1 to the static 192.168.50.1 via a NetworkManager keyfile.
sudo install -m 0600 -o root -g root -D \
     deploy/network/photo-server-eno1.nmconnection \
     /etc/NetworkManager/system-connections/photo-server-eno1.nmconnection
sudo nmcli connection reload
sudo nmcli connection up photo-server-eno1   # may already be auto-up

# Verify: eno1 has 192.168.50.1; dnsmasq binds only there; resolver
# unchanged on wlp6s0.
ip -br addr show eno1
sudo systemctl enable --now dnsmasq
sudo ss -tulnp | grep ':53\|:67'   # expect dnsmasq on 192.168.50.1
```

## 3. photo-server — install (one-time)

Assumes `./photo-server` already exists from §0.

```bash
# Dedicated service user.
sudo useradd --system --no-create-home --shell /usr/sbin/nologin \
     photo-server || true

# Binary + systemd unit + operator env file.
sudo install -m 0755 photo-server /usr/local/bin/photo-server
sudo install -m 0644 -D deploy/photo-server.service \
     /etc/systemd/system/photo-server.service
sudo install -m 0750 -d /etc/photo-server
sudo install -m 0640 -o root -g photo-server \
     deploy/photo-server.env.example \
     /etc/photo-server/photo-server.env

# *** EDIT NOW: at minimum set PHOTO_SERVER_ADMIN_PASSWORD. ***
sudo "${EDITOR:-nano}" /etc/photo-server/photo-server.env

sudo systemctl daemon-reload
sudo systemctl enable --now photo-server

# Health.
curl -fsS http://192.168.50.1/healthz
journalctl -u photo-server -e -n 20
```

## 4. Bench-test the prototype

1. Connect a phone to the SSID **photo-server** (`photos2026`).
2. iOS should **auto-pop the "Sign in" sheet** at
   `http://photos.wedding/`. If not, open
   `http://192.168.50.1/` manually.
3. Tap the **Polaroid** shutter → native camera → photo auto-uploads.
4. Tap **Gallery** → the photo is there.
5. Browse with another phone connected to the SSID → see each other's
   uploads.

## 5. Print the QR cards

```
http://192.168.50.1/admin/print
```
(or `http://photos.wedding/admin/print` from a guest device)

Log in as `admin` / the `PHOTO_SERVER_ADMIN_PASSWORD` you set. Click
**Print this page** → choose **Save as PDF** in the browser's print
dialog → print the PDF on a colour printer. A4 portrait gives 4
cards per page; cut to A6 for tables.

## 6. Update / re-print

Change env values → `sudo systemctl restart photo-server` → re-open
`/admin/print` to regenerate the QR.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `eno1` shows no IP | `nmcli connection up photo-server-eno1`; check the keyfile is 0600 |
| dnsmasq fails on port 53 | `systemd-resolved` is on 127.0.0.53; the keyfile's `bind-interfaces`+`interface=eno1` already isolates dnsmasq to 192.168.50.1 |
| Phone joins but no captive sheet | open `http://photos.wedding/` manually; check `PHOTO_SERVER_ALLOWED_HOSTS` matches the hostname; verify dnsmasq is wildcarding (`dig @192.168.50.1 captive.apple.com`) |
| Admin gives 404 | `PHOTO_SERVER_ADMIN_PASSWORD` is empty (fail-closed); set + restart |
| AP not discovered by WiFiman | factory reset (paperclip into AP reset hole ~10 s) |
