locals {
  name = var.name_prefix
  tags = [var.name_prefix]
}

# --- Networking: reserved external IP (the DNS A-record target) ----------

resource "google_compute_address" "static" {
  name   = "${local.name}-ip"
  region = var.region
}

# --- Data disk (disposable: destroyed with the VM; the durable archive ---
#     lives in the backup bucket). Mounted at /var/lib/photo-server.

resource "google_compute_disk" "data" {
  name = "${local.name}-data"
  type = "pd-balanced"
  zone = var.zone
  size = var.disk_size_gb
}

# --- VM identity ----------------------------------------------------------

resource "google_service_account" "vm" {
  account_id   = "${local.name}-vm"
  display_name = "photo-server VM"
}

# --- Object storage: release artifact (binary + unit) and backup/archive --

resource "google_storage_bucket" "release" {
  name                        = "${var.project_id}-release"
  location                    = upper(var.region)
  uniform_bucket_level_access = true
  force_destroy               = true # just the binary; safe to wipe on destroy
}

resource "google_storage_bucket" "backup" {
  name                        = "${var.project_id}-backup"
  location                    = upper(var.region)
  uniform_bucket_level_access = true
  force_destroy               = false

  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      days_since_noncurrent_time = 90
    }
    action {
      type = "Delete"
    }
  }

  # The canonical post-event archive. Survives `terraform destroy` so the
  # only copy of the photos is never on a single disposable disk.
  lifecycle {
    prevent_destroy = true
  }
}

resource "google_storage_bucket_iam_member" "release_read" {
  bucket = google_storage_bucket.release.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.vm.email}"
}

resource "google_storage_bucket_iam_member" "backup_write" {
  bucket = google_storage_bucket.backup.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.vm.email}"
}

# IAP-tunnel SSH access for the operator (no public SSH).
resource "google_project_iam_member" "operator_iap" {
  count   = var.iap_ssh_member == "" ? 0 : 1
  project = var.project_id
  role    = "roles/iap.tunnelResourceAccessor"
  member  = var.iap_ssh_member
}

# --- Firewall -------------------------------------------------------------

resource "google_compute_firewall" "web" {
  name          = "${local.name}-allow-web"
  network       = "default"
  direction     = "INGRESS"
  source_ranges = ["0.0.0.0/0"]
  target_tags   = local.tags

  allow {
    protocol = "tcp"
    ports    = ["80", "443"]
  }
}

resource "google_compute_firewall" "ssh_iap" {
  name          = "${local.name}-allow-ssh-iap"
  network       = "default"
  direction     = "INGRESS"
  source_ranges = ["35.235.240.0/20"] # Google IAP forwarders only
  target_tags   = local.tags

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }
}

# --- The VM ---------------------------------------------------------------

data "google_compute_image" "ubuntu" {
  family  = "ubuntu-2404-lts-amd64"
  project = "ubuntu-os-cloud"
}

resource "google_compute_instance" "vm" {
  name         = local.name
  machine_type = var.machine_type
  zone         = var.zone
  tags         = local.tags

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
      size  = 20
      type  = "pd-balanced"
    }
  }

  attached_disk {
    source      = google_compute_disk.data.id
    device_name = "photo-data" # -> /dev/disk/by-id/google-photo-data
  }

  network_interface {
    network = "default"
    access_config {
      nat_ip = google_compute_address.static.address
    }
  }

  metadata_startup_script = templatefile("${path.module}/startup-script.sh", {
    domain          = var.domain
    admin_password  = var.admin_password
    access_password = var.access_password
    release_bucket  = google_storage_bucket.release.name
    backup_bucket   = google_storage_bucket.backup.name
    data_device     = "photo-data"
  })

  service_account {
    email  = google_service_account.vm.email
    scopes = ["cloud-platform"]
  }

  shielded_instance_config {
    enable_secure_boot          = true
    enable_vtpm                 = true
    enable_integrity_monitoring = true
  }

  allow_stopping_for_update = true

  # The SA must be able to read the release bucket before first boot.
  depends_on = [
    google_storage_bucket_iam_member.release_read,
    google_storage_bucket_iam_member.backup_write,
  ]
}
