# Perl base image with dependencies for audio processing
FROM perl:5.36

# Install system dependencies for MP3/MP4 processing
RUN apt-get update && apt-get install -y \
    libmp3-info-perl \
    libmp4-info-perl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install Perl modules
RUN cpan -i \
    MP3::Info \
    MP4::Info \
    Google::Cloud::Storage

WORKDIR /app

# Copy podcast processing script
COPY process_podcast.pl .

# Copy existing index.xml template
COPY index.xml.template .

# Cloud Run port
ENV PORT=8080

# Copy HTTP server script
COPY server.pl .

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD perl -e "use LWP::UserAgent; my $ua = LWP::UserAgent->new; exit($ua->get('http://localhost:8080/health')->is_success ? 0 : 1)"

CMD ["perl", "server.pl"]
