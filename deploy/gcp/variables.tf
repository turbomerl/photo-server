variable "project_id" {
  type        = string
  description = "GCP project ID to deploy into."
}

variable "region" {
  type        = string
  description = "Region for the static IP, disk, and buckets. Pick the one nearest the venue."
  default     = "europe-west2"
}

variable "zone" {
  type        = string
  description = "Zone for the VM + data disk (must be within var.region)."
  default     = "europe-west2-a"
}

variable "domain" {
  type        = string
  description = "FQDN guests open, e.g. photos.example.com. Used for the env BASE_URL, the Caddyfile (auto-HTTPS), and the DNS A-record you create at your registrar."
}

variable "machine_type" {
  type        = string
  description = "VM size. e2-small (2 vCPU burst, 2GB) is fine; bump to e2-medium for a heavier HEIC upload burst (vipsthumbnail is CPU-bound)."
  default     = "e2-small"
}

variable "disk_size_gb" {
  type        = number
  description = "Persistent data disk size (originals + renditions + SQLite). ~150 guests x ~50 photos x ~4MB ~= 30GB; 50 gives headroom."
  default     = 50
}

variable "admin_password" {
  type        = string
  description = "PHOTO_SERVER_ADMIN_PASSWORD (gates /admin). Set in terraform.tfvars (gitignored). Empty disables the admin surface (fail-closed)."
  sensitive   = true
}

variable "iap_ssh_member" {
  type        = string
  description = "Identity granted roles/iap.tunnelResourceAccessor for IAP-tunnel SSH, e.g. 'user:you@example.com'. Empty skips the binding (grant it yourself, or you already have it as project owner)."
  default     = ""
}

variable "name_prefix" {
  type        = string
  description = "Prefix for resource names."
  default     = "photo-server"
}
