#!/usr/bin/env perl

use strict;
use warnings;

use Plack::Runner;
use Plack::Request;
use Plack::Response;
use Google::Cloud::Storage;
use JSON::PP;

my $project_id = $ENV{GCP_PROJECT_ID} or die "GCP_PROJECT_ID not set";
my $bucket_name = $ENV{GCS_BUCKET} or die "GCS_BUCKET not set";
my $index_object = $ENV{GCS_INDEX_OBJECT} || "index.xml";

my $storage = Google::Cloud::Storage->new(project => $project_id);
my $bucket = $storage->bucket($bucket_name);

# Cache index.xml content with TTL
my ($cached_content, $cache_time) = (undef, 0);
my $CACHE_TTL = 60; # 1 minute

sub get_index_xml {
    my $now = time;
    
    # Return cached if fresh
    if (defined $cached_content && ($now - $cache_time) < $CACHE_TTL) {
        return $cached_content;
    }
    
    # Fetch from GCS
    eval {
        my $obj = $bucket->object($index_object);
        $cached_content = $obj->download_as_string;
        $cache_time = $now;
        return $cached_content;
    };
    
    if ($@) {
        warn "Error fetching index.xml: $@\n";
        return undef;
    }
}

my $app = sub {
    my $env = shift;
    my $req = Plack::Request->new($env);
    my $res = Plack::Response->new(200);
    
    # Health check endpoint
    if ($req->path eq '/health') {
        $res->content_type('application/json');
        $res->body(encode_json({ status => 'ok' }));
        return $res->finalize;
    }
    
    # Serve podcast feed
    if ($req->path eq '/' || $req->path eq '/feed' || $req->path eq '/index.xml') {
        my $content = get_index_xml();
        
        if ($content) {
            $res->content_type('application/rss+xml; charset=utf-8');
            $res->body($content);
        } else {
            $res->status(500);
            $res->content_type('application/json');
            $res->body(encode_json({ error => 'Failed to fetch podcast feed' }));
        }
        return $res->finalize;
    }
    
    # Handle Pub/Sub push from Eventarc (triggers processing)
    if ($req->path eq '/process' && $req->method eq 'POST') {
        # This endpoint receives events when files are uploaded to GCS
        # In production, you'd trigger the processing script here
        # For now, just return 200 OK
        
        $res->content_type('application/json');
        $res->body(encode_json({ status => 'processing' }));
        return $res->finalize;
    }
    
    # 404
    $res->status(404);
    $res->content_type('application/json');
    $res->body(encode_json({ error => 'Not found' }));
    return $res->finalize;
};

# Run on Cloud Run PORT (default 8080)
my $port = $ENV{PORT} || 8080;
my $runner = Plack::Runner->new;
$runner->run($app);
