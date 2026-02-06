# Podcast Script GCP Cloud Run Deployment Guide

## Overview
This guide walks you through deploying your podcast processing script to Google Cloud Run with event-driven processing via GCS file uploads.

## Prerequisites
- GCP project with billing enabled
- `gcloud` CLI installed and configured
- Docker installed locally (for testing)
- Service account with appropriate permissions

## Setup Steps

### 1. Create GCS Buckets

```bash
# Bucket for audio files
gsutil mb gs://your-podcast-files

# (Optional) Separate bucket for index.xml if you prefer
gsutil mb gs://your-podcast-feeds
```

### 2. Create Service Account

```bash
# Create service account
gcloud iam service-accounts create podcast-processor \
    --display-name="Podcast Processor"

# Grant GCS permissions
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
    --member="serviceAccount:podcast-processor@YOUR_PROJECT_ID.iam.gserviceaccount.com" \
    --role="roles/storage.objectAdmin"

# Grant Cloud Run invoke permissions
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
    --member="serviceAccount:podcast-processor@YOUR_PROJECT_ID.iam.gserviceaccount.com" \
    --role="roles/run.invoker"

# Allow your user account to impersonate the service account
gcloud iam service-accounts add-iam-policy-binding \
    podcast-processor@YOUR_PROJECT_ID.iam.gserviceaccount.com \
    --member="user:YOUR_EMAIL@gmail.com" \
    --role="roles/iam.serviceAccountUser"

gcloud iam service-accounts add-iam-policy-binding \
    podcast-processor@YOUR_PROJECT_ID.iam.gserviceaccount.com \
    --member="user:YOUR_EMAIL@gmail.com" \
    --role="roles/iam.serviceAccountTokenCreator"
```

**Note**: Replace `YOUR_EMAIL@gmail.com` with your actual Google account email.

### 3. Build and Push Docker Image

```bash
# Set variables
export PROJECT_ID=your-project-id
export REGION=us-central1
export IMAGE_NAME=podcast-processor

# Build image
docker build -t gcr.io/$PROJECT_ID/$IMAGE_NAME:latest .

# Configure Docker auth
gcloud auth configure-docker

# Push to Container Registry
docker push gcr.io/$PROJECT_ID/$IMAGE_NAME:latest
```

### 4. Deploy to Cloud Run

```bash
gcloud run deploy podcast-processor \
    --image gcr.io/$PROJECT_ID/$IMAGE_NAME:latest \
    --platform managed \
    --region $REGION \
    --memory 512Mi \
    --timeout 3600 \
    --service-account podcast-processor@$PROJECT_ID.iam.gserviceaccount.com \
    --set-env-vars GCP_PROJECT_ID=$PROJECT_ID,GCS_BUCKET=your-podcast-files,GCS_INDEX_OBJECT=index.xml \
    --allow-unauthenticated \
    --no-gen2
```

### 5. Set Up Event-Driven Processing (Eventarc)

```bash
# Enable required APIs
gcloud services enable eventarc.googleapis.com
gcloud services enable cloudrun.googleapis.com

# Create Eventarc trigger for GCS uploads
gcloud eventarc triggers create podcast-upload-trigger \
    --location=$REGION \
    --destination-run-service=podcast-processor \
    --destination-run-region=$REGION \
    --event-filters="type=google.cloud.storage.object.v1.finalized" \
    --event-filters="bucket=your-podcast-files" \
    --service-account=podcast-processor@$PROJECT_ID.iam.gserviceaccount.com
```

### 6. Set Up Custom Domain (Optional)

For `podcast.joshlavin.com`:

```bash
# Add Cloud Run domain mapping
gcloud run domain-mappings create \
    --service=podcast-processor \
    --domain=podcast.joshlavin.com \
    --region=$REGION

# Follow DNS setup instructions in output
# You'll need to create DNS records pointing to Cloud Run
```

### 7. Update Your Podcast App

Use this URL in your podcast app:
```
https://podcast.joshlavin.com/
```

Or if not using custom domain:
```
https://podcast-processor-XXXXX.run.app/
```

## Manual Processing

To manually trigger processing when needed:

```bash
# Build a Cloud Build job to run the processor
gcloud builds submit . \
    --config=cloudbuild.yaml \
    --substitutions _BUCKET=your-podcast-files
```

Or invoke Cloud Run directly:

```bash
gcloud run jobs create process-podcast \
    --image=gcr.io/$PROJECT_ID/podcast-processor \
    --service-account=podcast-processor@$PROJECT_ID.iam.gserviceaccount.com \
    --set-env-vars GCP_PROJECT_ID=$PROJECT_ID,GCS_BUCKET=your-podcast-files

gcloud run jobs execute process-podcast
```

## Testing Locally

### Option 1: Use Application Default Credentials (Recommended)

```bash
# Authenticate with your personal account
gcloud auth application-default login

# Set environment variables
export GCP_PROJECT_ID=your-project-id
export GCS_BUCKET=your-podcast-files

# Run processor script
perl process_podcast.pl

# Run HTTP server
perl server.pl
```

### Option 2: Use Service Account Impersonation

```bash
# Authenticate and set impersonation
gcloud auth application-default login
gcloud config set auth/impersonate_service_account podcast-processor@YOUR_PROJECT_ID.iam.gserviceaccount.com

# Set environment variables
export GCP_PROJECT_ID=your-project-id
export GCS_BUCKET=your-podcast-files

# Run processor script
perl process_podcast.pl
```

Visit `http://localhost:5000/` to see your feed.

## Workflow

1. **Upload MP3 to GCS**: Drop new files in `gs://your-podcast-files/files/SOMETHING/`
2. **Eventarc trigger fires**: Automatically triggers Cloud Run
3. **Processing runs**: Script processes new MP3s
4. **index.xml updates**: Updated feed is stored in GCS
5. **Podcast app fetches**: Your podcast app fetches the latest feed from Cloud Run endpoint
6. **New episodes appear**: Done!

## Cost Estimation

- **Cloud Run**: ~$0.20/month (with generous free tier)
- **GCS Storage**: ~$0.02/GB/month
- **Eventarc**: Free for first 100k events/month

Total: Very cheap for a personal podcast!

## Troubleshooting

Check Cloud Run logs:
```bash
gcloud run logs read podcast-processor --limit 50
```

Check Eventarc trigger status:
```bash
gcloud eventarc triggers describe podcast-upload-trigger --location=$REGION
```

Monitor GCS bucket:
```bash
gsutil ls -r gs://your-podcast-files/
```
