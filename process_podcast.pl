#!/bin/env perl

use strict;
use warnings;

use MP3::Info 'get_mp3info';
use MP4::Info 'get_mp4info';
use POSIX 'strftime';

##
## Processes MP3 files from a GCS bucket and updates index.xml
## 
## Expects environment variables:
##   - GCP_PROJECT_ID: GCP project ID
##   - GCS_BUCKET: GCS bucket containing audio files
##   - GCS_INDEX_OBJECT: Path to index.xml in bucket (e.g., "index.xml")
##

my $bucket_name = $ENV{GCS_BUCKET} or die "GCS_BUCKET not set";
my $index_object = $ENV{GCS_INDEX_OBJECT} || "index.xml";

my $xml = <<EOX;
     <item>
         <title>%s</title>
         <pubDate>%s</pubDate>
         <enclosure url="https://joshlavin.com/feeds/%s" length="%s" type="audio/mpeg" />
     </item>
EOX

# Get list of objects in GCS bucket using gsutil
my @out;
my $list_cmd = "gsutil ls -r gs://$bucket_name/files/";
my @objects = `$list_cmd`;
die "Failed to list GCS objects: $?" if $?;

foreach my $file (@objects) {
    chomp $file;
    next if $file =~ /\/$/;  # Skip directories
    next unless $file =~ /\.(mp3|m4a)$/i;
    
    # Extract just the path part from gs://bucket/path
    my $path = $file;
    $path =~ s/^gs:\/\/$bucket_name\///;
    
    warn "Processing: $file\n";
    
    my $title = $path;
    $title =~ s/.*\/(.*)\.(?:mp3|m4a)/$1/i;
    $title =~ s/[_\d]/ /g;
    $title =~ s/\s+$//;
    
    my $date = strftime('%c', localtime);
    
    # Download file temporarily to get metadata
    my $temp_file = "/tmp/$title.tmp";
    system("gsutil", "cp", $file, $temp_file);
    die "Failed to download $file" if $?;
    
    my $info;
    if ($file =~ /\.mp3$/i) {
        $info = get_mp3info($temp_file);
    } else {
        $info = get_mp4info($temp_file);
    }
    
    die "Failed to get info for $file: $@" if $@;
    
    push @out, sprintf $xml, $title, $date, $path, $info->{SIZE};
    
    unlink $temp_file;
    
    warn "  Title: $title\n";
    warn "  Date: $date\n";
}

# Read existing index.xml from GCS if it exists
my @existing_lines;
eval {
    my $temp_index = "/tmp/index.xml.tmp";
    system("gsutil", "cp", "gs://$bucket_name/$index_object", $temp_index);
    if (-e $temp_index) {
        open my $fh, '<', $temp_index or die "Cannot open $temp_index: $!";
        @existing_lines = <$fh>;
        close $fh;
        unlink $temp_index;
        
        # Remove closing tags from the end
        while (@existing_lines && $existing_lines[-1] =~ /^\s*<\/(channel|rss)>\s*$/) {
            pop @existing_lines;
        }
    }
};

# Build new index.xml
my $new_content = '';
if (@existing_lines) {
    $new_content = join '', @existing_lines;
}
$new_content .= join "\n", @out;
$new_content .= "\n</channel>\n</rss>\n";

# Write updated index.xml back to GCS
my $temp_index = "/tmp/index.xml.new";
open my $fh, '>', $temp_index or die "Cannot write to $temp_index: $!";
print $fh $new_content;
close $fh;

system("gsutil", "cp", $temp_index, "gs://$bucket_name/$index_object");
die "Failed to upload index.xml" if $?;

unlink $temp_index;

warn "Updated $index_object in GCS bucket\n";
