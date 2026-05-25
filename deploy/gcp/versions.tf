terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }

  # Remote state in GCS so a solo operator can `terraform destroy` cleanly
  # weeks later from any checkout (a lost local state would orphan the
  # billable static IP + disk). The bucket must exist BEFORE `init` — see
  # README step 0 — and is supplied at init time:
  #   terraform init -backend-config="bucket=<project>-tfstate"
  backend "gcs" {
    prefix = "deploy/gcp"
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}
