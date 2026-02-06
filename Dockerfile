# Perl slim image - much smaller than full perl
FROM perl:5.36-slim

# Install system dependencies and Google Cloud SDK
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    libmp3-info-perl \
    libmp4-info-perl \
    ca-certificates \
    curl \
    gnupg \
    && rm -rf /var/lib/apt/lists/*

# Install Google Cloud SDK
RUN echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | tee -a /etc/apt/sources.list.d/google-cloud-sdk.list && \
    curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key --keyring /usr/share/keyrings/cloud.google.gpg add - && \
    apt-get update && apt-get install -y --no-install-recommends \
    google-cloud-sdk \
    && rm -rf /var/lib/apt/lists/*

# Install Perl modules
RUN cpan -iT \
    MP3::Info \
    MP4::Info \
    Plack \
    Plack::Runner \
    JSON::PP \
    LWP::UserAgent

# Remove build tools to reduce size
RUN apt-get remove -y build-essential && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy application files
COPY process_podcast.pl .
COPY index.xml.template .
COPY server.pl .

ENV PORT=8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD perl -e "use LWP::UserAgent; my $ua = LWP::UserAgent->new; exit($ua->get('http://localhost:8080/health')->is_success ? 0 : 1)"

CMD ["perl", "server.pl"]
