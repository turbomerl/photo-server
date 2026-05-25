# photo-server on GCP — Terraform runbook

Stands up the cloud deployment chosen in `docs/CLOUD_HANDOVER.md`: a
Compute Engine VM + persistent data disk + Caddy auto-HTTPS, fronted by a
reserved static IP, with a GCS bucket for backups/archive. Event-window
lifecycle: provision → rehearsal → event → final backup → `terraform destroy`.

**Architecture decisions:** beads `photo_server-rrh` (host/domain/TLS),
`9wv` (persistent disk), `apt` (libvips on the host), `kgu.24` (GCS backup).
Cookie `Secure` gating (`ycl`) ships in the app binary.

> **Working directory:** run every command below **from the repo root**
> (`photo-server/`), where `make build-linux` drops the binary. Terraform
> lives in `deploy/gcp/`, so all `terraform` commands use
> `-chdir=deploy/gcp`. (Or `cd deploy/gcp` and drop `-chdir`, but then the
> binary is at `../../photo-server-linux-amd64`.)

## Prerequisites

- `terraform`, `gcloud`, `gsutil` installed; `gcloud auth login` +
  `gcloud auth application-default login` done.
- A GCP project with billing enabled and these APIs on:
  `gcloud services enable compute.googleapis.com storage.googleapis.com iap.googleapis.com`
- A domain you control (DNS at any registrar).
- Go toolchain (to cross-compile the binary): `make build-linux`.

## Step 0 — one-time: create the Terraform state bucket (out-of-band)

The GCS backend can't bootstrap itself. Create the bucket once:

```bash
gsutil mb -b on -l europe-west2 gs://<project>-tfstate
gsutil versioning set on gs://<project>-tfstate
```

## Step 1 — configure

```bash
cp deploy/gcp/terraform.tfvars.example deploy/gcp/terraform.tfvars   # then edit (gitignored)
terraform -chdir=deploy/gcp init -backend-config="bucket=<project>-tfstate"
```

## Step 2 — build + stage the binary

The disposable VM downloads the binary from the release bucket on boot, so
the release bucket must exist and hold the binary first.

```bash
make build-linux                       # -> ./photo-server-linux-amd64

# create just the release bucket + its IAM, so we can upload before the VM:
terraform -chdir=deploy/gcp apply \
  -target=google_storage_bucket.release \
  -target=google_storage_bucket_iam_member.release_read

# upload the binary + systemd unit. The upload_command output has the
# resolved bucket name and repo-root-relative paths baked in:
eval "$(terraform -chdir=deploy/gcp output -raw upload_command)"
```

<details><summary>…or upload explicitly</summary>

```bash
BUCKET=$(terraform -chdir=deploy/gcp output -raw release_bucket)
gsutil cp photo-server-linux-amd64    gs://$BUCKET/photo-server
gsutil cp deploy/photo-server.service gs://$BUCKET/photo-server.service
```
</details>

(The startup script also waits up to ~5 min for the binary, so a single
`terraform apply` works too as long as you upload within that window; it
self-heals on reboot regardless.)

## Step 3 — apply

```bash
terraform -chdir=deploy/gcp apply
terraform -chdir=deploy/gcp output dns_instructions   # -> create this A-record at your registrar
```

Create the A-record, then confirm it resolves:
```bash
dig +short <domain>      # should print the static_ip output
```

## Step 4 — verify

```bash
gcloud compute ssh "$(terraform -chdir=deploy/gcp output -raw instance_name)" --zone <zone> --tunnel-through-iap
#   on the box:
sudo systemctl status photo-server caddy
findmnt /var/lib/photo-server
curl -fsS http://127.0.0.1:8080/healthz

# from your laptop, once DNS + cert are up (Caddy issues on first request):
curl -fsS https://<domain>/healthz
curl -sI https://<domain>/ | grep -i set-cookie   # ps_session ... Secure; HttpOnly; SameSite=Lax
```

Then browser smoke test: set a name, upload a HEIC (exercises
`vipsthumbnail`), see the thumbnail in `/gallery`, download the original,
log into `/admin`.

## Step 5 — backups

Runs hourly automatically. Force one (e.g. before teardown):
```bash
# on the box:
sudo systemctl start photo-server-backup.service
# from your laptop:
gsutil ls gs://$(terraform -chdir=deploy/gcp output -raw backup_bucket)/db/
gsutil ls gs://$(terraform -chdir=deploy/gcp output -raw backup_bucket)/originals/
```

## Step 6 — teardown (after the event)

```bash
# 1) final archive sync (on the box):
sudo systemctl start photo-server-backup.service
# 2) destroy everything billable in one shot:
terraform -chdir=deploy/gcp destroy
```

`destroy` removes the VM, static IP, firewalls, service account, release
bucket, **and the disposable data disk**. The **backup bucket is
`prevent_destroy` + versioned**, so the photo archive survives. Confirm:
```bash
gcloud compute instances list   # empty
gcloud compute addresses list   # empty
gcloud compute disks list       # empty (data disk gone)
gsutil ls gs://$(terraform -chdir=deploy/gcp output -raw backup_bucket 2>/dev/null || echo <project>-backup)/   # archive still there
```

To later remove the archive too: temporarily set `force_destroy = true`
and drop `prevent_destroy` on `google_storage_bucket.backup`, then delete.

## Notes

- **Secrets:** `admin_password` is rendered into the VM's startup script
  (instance metadata) via `terraform.tfvars`. Fine for the solo/trusted
  threat model. Upgrade: store it in Secret Manager, grant the VM SA
  `roles/secretmanager.secretAccessor`, and fetch it at boot in the
  startup script.
- **Updating the app:** `make build-linux`, re-upload to the release
  bucket, then `gcloud compute instances reset` (or SSH + re-run the
  startup steps). The script re-downloads the binary on every boot.
- **DNS via Cloud DNS instead of a registrar:** add a
  `google_dns_record_set` against your managed zone; the static IP output
  is the rrdata.
- **Cloudflare (or any proxied DNS):** the `photos` record must be
  **"DNS only" (grey cloud)**, not proxied. A proxied record returns
  Cloudflare's anycast IPs (`104.21.x` / `172.67.x`), so the name never
  resolves to the static IP, Caddy can't complete its Let's Encrypt
  challenge, and traffic never reaches the VM. Grey-cloud lets Caddy own
  TLS directly. To keep the orange cloud you'd need a Cloudflare Origin
  Certificate or Caddy's DNS-01 challenge (`caddy-dns/cloudflare` + an API
  token) — more setup, and mind Cloudflare's request-body upload limit.
