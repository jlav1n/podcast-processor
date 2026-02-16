output "service_account_email" {
  description = "Service account email"
  value       = google_service_account.podcast_processor.email
}

output "cloud_run_url" {
  description = "Cloud Run service URL"
  value       = google_cloud_run_service.podcast_processor.status[0].url
}

output "gcs_bucket_name" {
  description = "GCS bucket name"
  value       = google_storage_bucket.podcast_files.name
}

output "eventarc_trigger_id" {
  description = "Eventarc Trigger ID"
  value       = google_eventarc_trigger.podcast_upload.id
}

output "podcast_feed_url" {
  description = "URL to the podcast feed"
  value       = "${google_cloud_run_service.podcast_processor.status[0].url}/"
}
