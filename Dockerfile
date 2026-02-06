# Perl slim image - much smaller than full perl
FROM perl:5.36-slim

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    libmp3-info-perl \
    libmp4-info-perl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install Perl modules
RUN cpan -iT \
    MP3::Info \
    MP4::Info \
    Google::Cloud::Storage \
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
