# Podcast Script GCP Cloud Run Deployment Guide

## Overview
This guide walks you through deploying your podcast processing script to Google Cloud Run with event-driven processing via GCS file uploads. We use **Terraform** to automate the entire setup.

## Prerequisites
- GCP project with billing enabled
- `gcloud` CLI installed and configured (`gcloud auth login`)
- **Terraform** installed (>= 1.0) - https://www.terraform.io/downloads.html
- Docker installed locally
- `gsutil` for GCS operations

## Quick Start with Terraform (Recommended)

### 1. Configure Variables

```bash
cd terraform
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` with your values:
```hcl
project_id      = "your-project-id"
region          = "us-central1"
admin_email     = "your-email@gmail.com"
gcs_bucket_name = "your-podcast-files-unique"  # Must be globally unique
container_image = "gcr.io/your-project-id/podcast-processor:latest"
```

### 2. Build & Push Container Image

From repository root:

```bash
export PROJECT_ID=$(gcloud config get-value project)

docker build -t gcr.io/$(gcloud config get-value project)/podcast-processor:latest .
gcloud auth configure-docker
docker push gcr.io/$(gcloud config get-value project)/podcast-processor:latest
```

#### TODO 

Use `gcloud builds submit --tag <TAG> .` ?

### 3. Deploy with Terraform

```bash
cd terraform
terraform init
terraform plan
terraform apply
```

Type `yes` to confirm. Terraform will create:
- Service account with proper IAM bindings
- GCS bucket for audio files
- Cloud Run service (512MB, 1 CPU)
- Eventarc trigger for automatic file processing
- All necessary permissions

### 4. Get Your Feed URL

After successful deployment, Terraform outputs the feed URL:

```bash
terraform output podcast_feed_url
```

Example: `https://podcast-processor-xxxxx.run.app/`

Use this URL in your podcast app.

## Manual Deployment (Alternative)

If you prefer not to use Terraform, see [MANUAL_DEPLOYMENT.md](./MANUAL_DEPLOYMENT.md) for step-by-step `gcloud` commands.

## Testing Locally

### Prerequisites
Install Perl dependencies:

```bash
cpan MP3::Info MP4::Info Google::Cloud::Storage Plack Plack::Runner JSON::PP
```

### Option 1: Use Application Default Credentials (Recommended)

```bash
# Authenticate with your personal account
gcloud auth application-default login

# Set environment variables
export GCP_PROJECT_ID=$(gcloud config get-value project)
export GCS_BUCKET=$(terraform output -raw gcs_bucket_name)

# Test processor script
perl process_podcast.pl

# Run HTTP server (default port 5000)
perl server.pl
```

Visit `http://localhost:5000/` to see your feed.

### Option 2: Use Service Account Impersonation

```bash
gcloud auth application-default login
export SERVICE_ACCOUNT=$(terraform output -raw service_account_email)
gcloud config set auth/impersonate_service_account $SERVICE_ACCOUNT

# Set environment variables
export GCP_PROJECT_ID=$(gcloud config get-value project)
export GCS_BUCKET=$(terraform output -raw gcs_bucket_name)

perl process_podcast.pl
perl server.pl
```

## Workflow

1. **Upload MP3 to GCS**: Drop new files in `gs://bucket-name/files/SOMETHING/`
   ```bash
   gsutil cp my-podcast.mp3 gs://bucket-name/files/test/
   ```

2. **Eventarc trigger fires**: Automatically triggers Cloud Run within seconds

3. **Processing runs**: Script reads MP3 metadata and processes files

4. **index.xml updates**: Updated feed is stored in GCS

5. **Podcast app fetches**: Your podcast app fetches the latest feed from Cloud Run endpoint

6. **New episodes appear**: Done!

## Managing the Deployment

### View Logs

```bash
# Cloud Run logs
gcloud run logs read podcast-processor --limit 50 --region us-central1

# Eventarc logs
gcloud eventarc triggers describe podcast-upload-trigger --location us-central1
```

### Monitor GCS Bucket

```bash
gsutil ls -r gs://$(terraform output -raw gcs_bucket_name)/
```

### Update Container Image

If you rebuild the container:

```bash
docker push gcr.io/$PROJECT_ID/podcast-processor:latest
terraform apply  # Terraform detects image change and redeploys
```

### Destroy All Resources

⚠️ **Warning**: This deletes everything including the GCS bucket. Backup first!

```bash
cd terraform
terraform destroy
```

## Setting Up Custom Domain (Optional)

For `podcast.joshlavin.com`:

```bash
# Get Cloud Run service URL
CLOUD_RUN_URL=$(terraform output -raw cloud_run_url)

# Add Cloud Run domain mapping
gcloud run domain-mappings create \
    --service=podcast-processor \
    --domain=podcast.joshlavin.com \
    --region=us-central1

# Follow DNS setup instructions from output
# You'll need to create DNS records in your domain registrar
```

## Cost Estimation

- **Cloud Run**: ~$0.20/month (generous free tier covers this)
- **GCS Storage**: ~$0.02/GB/month
- **Eventarc**: Free for first 100k events/month
- **Terraform State**: Free if stored locally

**Total**: Very cheap for a personal podcast! Likely free on free tier.

## Troubleshooting

### Script won't process files

```bash
# Check Cloud Run logs
gcloud run logs read podcast-processor --limit 100 --region us-central1

# Verify bucket exists
gsutil ls gs://$(terraform output -raw gcs_bucket_name)/

# Check Eventarc trigger is configured
gcloud eventarc triggers list --location us-central1
```

### Feed won't load

```bash
# Test the endpoint directly
curl $(terraform output -raw cloud_run_url)

# Check if index.xml exists in GCS
gsutil cat gs://$(terraform output -raw gcs_bucket_name)/index.xml
```

### Service account permissions issues

```bash
# Check service account exists
gcloud iam service-accounts list

# Check IAM bindings
gcloud projects get-iam-policy $(gcloud config get-value project) \
  --flatten="bindings[].members" \
  --filter="bindings.members:podcast-processor"
```

## Terraform State Management

By default, state is stored locally in `terraform.tfstate`. For team environments or better safety, use remote state:

```bash
# Create GCS bucket for state (one-time)
gsutil mb gs://terraform-state-$(gcloud config get-value project)

# Add to terraform/main.tf:
terraform {
  backend "gcs" {
    bucket = "terraform-state-YOUR_PROJECT"
    prefix = "podcast-processor"
  }
}

# Reinitialize
terraform init
```

## API Endpoints

Once deployed, Cloud Run exposes these endpoints:

- `GET /` - Serve podcast feed (XML)
- `GET /feed` - Alias for podcast feed
- `GET /index.xml` - Direct access to index.xml
- `POST /process` - Webhook for manual triggering (if integrated with Cloud Scheduler)
- `GET /health` - Health check endpoint

## Additional Resources

- [Terraform Configuration](./terraform/README.md) - Detailed Terraform docs
- [Process Script](./process_podcast.pl) - MP3 processing logic
- [Server Script](./server.pl) - HTTP endpoint serving logic
