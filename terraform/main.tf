terraform {
  required_version = ">= 1.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# Get project data for service agents
data "google_client_config" "current" {}

data "google_project" "current" {
  project_id = var.project_id
}

# Service Account
resource "google_service_account" "podcast_processor" {
  account_id   = var.service_account_name
  display_name = "Podcast Processor Service Account"
  description  = "Service account for processing podcast files in Cloud Run"
}

# IAM: GCS permissions for service account
resource "google_project_iam_member" "podcast_gcs_admin" {
  project = var.project_id
  role    = "roles/storage.objectAdmin"
  member  = google_service_account.podcast_processor.member
}

# IAM: Cloud Run invoke permissions for service account
resource "google_project_iam_member" "podcast_run_invoker" {
  project = var.project_id
  role    = "roles/run.invoker"
  member  = google_service_account.podcast_processor.member
}

# IAM: Eventarc Service Agent permissions for service account
resource "google_project_iam_member" "podcast_eventarc_agent" {
  project = var.project_id
  role    = "roles/eventarc.serviceAgent"
  member  = google_service_account.podcast_processor.member
}

# IAM: Eventarc Event Receiver permissions for service account
resource "google_project_iam_member" "podcast_eventarc_receiver" {
  project = var.project_id
  role    = "roles/eventarc.eventReceiver"
  member  = google_service_account.podcast_processor.member
}

# IAM: Allow user to impersonate service account
resource "google_service_account_iam_member" "impersonate_user" {
  service_account_id = google_service_account.podcast_processor.name
  role               = "roles/iam.serviceAccountUser"
  member             = "user:${var.admin_email}"
}

resource "google_service_account_iam_member" "token_creator" {
  service_account_id = google_service_account.podcast_processor.name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "user:${var.admin_email}"
}

# Allow CloudRun SA self-impersonation (required for GCS SignedURL usage)
resource "google_service_account_iam_member" "cloudrun_token_creator" {
  service_account_id = google_service_account.podcast_processor.name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = google_service_account.podcast_processor.member
}

# GCS Bucket for audio files (not public)
resource "google_storage_bucket" "podcast_files" {
  name          = var.gcs_bucket_name
  location      = var.region
  force_destroy = false

  versioning {
    enabled = false
  }

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
}

# GCS Bucket for CloudRun files (not public)
resource "google_storage_bucket" "podcast_cloudrun_files" {
  name          = var.gcs_cloudrun_bucket_name
  location      = var.region
  force_destroy = false

  versioning {
    enabled = false
  }

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
}

# Grant Pub/Sub on GCS Service Account
data "google_storage_project_service_account" "gcs_sa" {}

resource "google_project_iam_member" "gcs_pubsub" {
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = data.google_storage_project_service_account.gcs_sa.member
}

# Cloud Run Service
resource "google_cloud_run_service" "podcast_processor" {
  name     = var.service_name
  location = var.region

  template {
    spec {
      service_account_name = google_service_account.podcast_processor.email

      containers {
        image = var.container_image

        env {
          name  = "GCP_PROJECT_ID"
          value = var.project_id
        }

        env {
          name  = "GCS_BUCKET"
          value = google_storage_bucket.podcast_cloudrun_files.name
        }

        env {
          name  = "GCS_FILES_BUCKET"
          value = google_storage_bucket.podcast_files.name
        }

        env {
          name  = "GCS_INDEX_OBJECT"
          value = var.index_object_name
        }

        resources {
          limits = {
            memory = "512Mi"
            cpu    = "1000m"
          }
        }
      }

      timeout_seconds = 3600
    }

    metadata {
      annotations = {
        "autoscaling.knative.dev/minScale" = "0"
        "autoscaling.knative.dev/maxScale" = "10"
      }
    }
  }

  traffic {
    percent         = 100
    latest_revision = true
  }

  depends_on = [
    google_project_iam_member.podcast_gcs_admin,
    google_project_iam_member.podcast_run_invoker
  ]

  lifecycle {
    ignore_changes = [
      template.0.metadata.0.annotations,
    ]
  }
}

# Allow unauthenticated access to Cloud Run service
resource "google_cloud_run_service_iam_member" "public_access" {
  service  = google_cloud_run_service.podcast_processor.name
  location = google_cloud_run_service.podcast_processor.location
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# Enable required APIs
resource "google_project_service" "required_apis" {
  for_each = toset([
    "artifactregistry.googleapis.com",
    "cloudscheduler.googleapis.com",
    "eventarc.googleapis.com",
    "iamcredentials.googleapis.com",
    "run.googleapis.com",
    "storage-api.googleapis.com",
  ])

  service            = each.value
  disable_on_destroy = false
}

resource "google_eventarc_trigger" "podcast_upload" {
  name     = "${var.service_name}-upload-trigger"
  location = var.region

  matching_criteria {
    attribute = "type"
    value     = "google.cloud.storage.object.v1.finalized"
  }

  matching_criteria {
    attribute = "bucket"
    value     = google_storage_bucket.podcast_files.name
  }

  destination {
    cloud_run_service {
      service = google_cloud_run_service.podcast_processor.name
      region  = var.region
      path    = "/process"
    }
  }

  service_account = google_service_account.podcast_processor.email

  depends_on = [
    google_project_service.required_apis["eventarc.googleapis.com"],
    google_cloud_run_service.podcast_processor
  ]
}
