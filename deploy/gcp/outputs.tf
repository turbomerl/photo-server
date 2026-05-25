output "static_ip" {
  description = "Reserved external IP. Create the DNS A-record pointing at this."
  value       = google_compute_address.static.address
}

output "dns_instructions" {
  description = "A-record to create at your registrar."
  value       = "Create at your DNS registrar:  ${var.domain}.  A  ${google_compute_address.static.address}  (TTL 300)"
}

output "instance_name" {
  value = google_compute_instance.vm.name
}

output "release_bucket" {
  value = google_storage_bucket.release.name
}

output "backup_bucket" {
  value = google_storage_bucket.backup.name
}

output "upload_command" {
  description = "Run from the repo root after `make build-linux` to stage the binary + unit."
  value       = "gsutil cp photo-server-linux-amd64 gs://${google_storage_bucket.release.name}/photo-server && gsutil cp deploy/photo-server.service gs://${google_storage_bucket.release.name}/photo-server.service"
}

output "ssh_command" {
  value = "gcloud compute ssh ${google_compute_instance.vm.name} --zone ${var.zone} --tunnel-through-iap"
}
