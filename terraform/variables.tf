variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "admin_email" {
  description = "Email address of the admin user for service account impersonation"
  type        = string
}

variable "service_account_name" {
  description = "Name for the service account"
  type        = string
  default     = "podcast-processor"
}

variable "service_name" {
  description = "Name for the Cloud Run service"
  type        = string
  default     = "podcast-processor"
}

variable "gcs_bucket_name" {
  description = "Name for the GCS bucket (must be globally unique)"
  type        = string
}

variable "container_image" {
  description = "Container image URI (e.g., gcr.io/PROJECT_ID/podcast-processor:latest)"
  type        = string
}

variable "index_object_name" {
  description = "Path to index.xml in GCS bucket"
  type        = string
  default     = "index.xml"
}
