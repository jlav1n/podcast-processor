#!/bin/env perl

use strict;
use warnings;

use MP3::Info 'get_mp3info';
use MP4::Info 'get_mp4info';
use POSIX 'strftime';
use Google::Cloud::Storage;

##
## Processes MP3 files from a GCS bucket and updates index.xml
## 
## Expects environment variables:
##   - GCP_PROJECT_ID: GCP project ID
##   - GCS_BUCKET: GCS bucket containing audio files
##   - GCS_INDEX_OBJECT: Path to index.xml in bucket (e.g., "index.xml")
##

my $project_id = $ENV{GCP_PROJECT_ID} or die "GCP_PROJECT_ID not set";
my $bucket_name = $ENV{GCS_BUCKET} or die "GCS_BUCKET not set";
my $index_object = $ENV{GCS_INDEX_OBJECT} || "index.xml";

my $storage = Google::Cloud::Storage->new(project => $project_id);
my $bucket = $storage->bucket($bucket_name);

my $xml = <<EOX;
     <item>
         <title>%s</title>
         <pubDate>%s</pubDate>
         <enclosure url="https://joshlavin.com/feeds/%s" length="%s" type="audio/mpeg" />
     </item>
EOX

# Get list of objects in GCS bucket (e.g., files/SOMETHING/*.mp3)
my @out;
my @objects = $bucket->objects(prefix => 'files/');

while (my $obj = $objects->next) {
    my $file = $obj->name;
    next unless $file =~ /\.(mp3|m4a)$/i;
    
    warn "Processing: $file\n";
    
    my $title = $file;
    $title =~ s/.*\/(.*)\.(?:mp3|m4a)/$1/i;
    $title =~ s/[_\d]/ /g;
    $title =~ s/\s+$//;
    
    my $date = strftime('%c', localtime);
    
    # Download file temporarily to get metadata
    my $temp_file = "/tmp/$title.tmp";
    $obj->download({ file => $temp_file });
    
    my $info;
    if ($file =~ /\.mp3$/i) {
        $info = get_mp3info($temp_file);
    } else {
        $info = get_mp4info($temp_file);
    }
    
    die "Failed to get info for $file: $@" if $@;
    
    push @out, sprintf $xml, $title, $date, $file, $info->{SIZE};
    
    unlink $temp_file;
    
    warn "  Title: $title\n";
    warn "  Date: $date\n";
}

# Read existing index.xml from GCS if it exists
my @existing_lines;
eval {
    my $index_obj = $bucket->object($index_object);
    my $content = $index_obj->download_as_string;
    @existing_lines = split /\n/, $content;
    
    # Remove closing tags from the end
    while (@existing_lines && $existing_lines[-1] =~ /^\s*<\/(channel|rss)>\s*$/) {
        pop @existing_lines;
    }
};

# Build new index.xml
my $new_content = '';
if (@existing_lines) {
    $new_content = join "\n", @existing_lines;
    $new_content .= "\n";
}
$new_content .= join "\n", @out;
$new_content .= "\n</channel>\n</rss>\n";

# Write updated index.xml back to GCS
my $index_obj = $bucket->object($index_object);
$index_obj->upload($new_content, { content_type => 'application/xml' });

warn "Updated $index_object in GCS bucket\n";
