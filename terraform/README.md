# Terraform Deployment Guide

This directory contains Terraform files to deploy the podcast processor to GCP Cloud Run.

## Prerequisites

1. Install Terraform: https://www.terraform.io/downloads.html
2. Install Google Cloud SDK: https://cloud.google.com/sdk/docs/install
3. Authenticate: `gcloud auth login`
4. Set default project: `gcloud config set project YOUR_PROJECT_ID`
5. Build and push Docker image (see main README)

## Quick Start

### 1. Prepare Configuration

```bash
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` with your values:
- `project_id`: Your GCP project ID
- `admin_email`: Your Google account email (for impersonation)
- `gcs_bucket_name`: Unique globally unique bucket name
- `container_image`: Your container image URI

### 2. Build and Push Container Image

From the repository root:

```bash
export PROJECT_ID=your-project-id

docker build -t gcr.io/$PROJECT_ID/podcast-processor:latest .
gcloud auth configure-docker
docker push gcr.io/$PROJECT_ID/podcast-processor:latest
```

Update `terraform.tfvars` with the image URI if different.

### 3. Initialize Terraform

```bash
cd terraform
terraform init
```

### 4. Review Plan

```bash
terraform plan
```

This shows what resources will be created.

### 5. Apply

```bash
terraform apply
```

Review the output and type `yes` to confirm.

### 6. Get Outputs

After successful apply, Terraform will print:
- Service account email
- Cloud Run URL
- GCS bucket name
- Podcast feed URL

Example output:
```
cloud_run_url = "https://podcast-processor-xxxxx.run.app/"
gcs_bucket_name = "your-podcast-files-unique-name"
podcast_feed_url = "https://podcast-processor-xxxxx.run.app/"
```

## What Gets Created

- **Service Account** - For Cloud Run to access GCS
- **GCS Bucket** - Stores audio files and index.xml
- **Cloud Run Service** - HTTP endpoint serving the podcast feed
- **Eventarc Trigger** - Automatically processes files on upload
- **IAM Bindings** - Grants necessary permissions

## After Deployment

### Upload Test Files

```bash
gsutil cp my-podcast.mp3 gs://your-podcast-files-bucket/files/test/
```

The Eventarc trigger will automatically invoke Cloud Run, which processes the file and updates index.xml.

### Monitor Cloud Run Logs

```bash
gcloud run logs read podcast-processor --limit 50 --region us-central1
```

### Test the Feed

```bash
curl https://podcast-processor-xxxxx.run.app/
```

## Managing Impersonation Locally

After Terraform creates the service account, you can impersonate it locally:

```bash
gcloud auth application-default login

# Set impersonation (optional)
gcloud config set auth/impersonate_service_account \
  podcast-processor@YOUR_PROJECT_ID.iam.gserviceaccount.com
```

## Cleanup

To destroy all resources:

```bash
terraform destroy
```

⚠️ This will delete the GCS bucket and all contents. Make sure to backup first!

## Terraform State

By default, state is stored locally in `terraform.tfstate`. For team environments, consider using remote state:

```bash
# Create a GCS bucket for state (one-time setup)
gsutil mb gs://terraform-state-your-project

# Add to main.tf:
terraform {
  backend "gcs" {
    bucket = "terraform-state-your-project"
    prefix = "podcast-processor"
  }
}

# Reinitialize
terraform init
```

## Updating the Image

If you rebuild the container image:

```bash
docker push gcr.io/$PROJECT_ID/podcast-processor:latest

# Update Cloud Run
terraform apply
```

Terraform will detect the image change and redeploy.

## Cost Estimates

- **Cloud Run**: ~$0.20/month (covers free tier)
- **GCS**: ~$0.02/GB/month
- **Eventarc**: Free for first 100k events/month

Total: Very cheap!
