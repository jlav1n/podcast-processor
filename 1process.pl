#!/bin/env perl

use strict;
use warnings;

use MP3::Info 'get_mp3info';
use MP4::Info 'get_mp4info';
use POSIX 'strftime';

##
## only have in files/ what you want to process for this round
## 
## each file needs to be in a directory in files/
##
## if any files haven't been listened to, then keep them there
##
##  perl 1process.pl >> index.xml
##

my $xml = <<EOX;
     <item>
         <title>%s</title>
         <pubDate>%s</pubDate>
         <enclosure url="https://joshlavin.com/feeds/%s" length="%s" type="audio/mpeg" />
     </item>
EOX

my @dirs = glob 'files/*';
my @out;

for my $dir (@dirs) {
warn $dir;
    my @files = glob "$dir/*";
    for my $file (@files) {
warn $file;
        `chmod 644 $file`;
        my $title = $file;
        $title =~ s/.*\/(.*)\.mp3/$1/;
        $title =~ s/[_\d]/ /g;
        $title =~ s/\s+$//;
        my $date = strftime('%c', localtime);
        my $info;
        if ($file =~ /\.mp3/) {
            $info = get_mp3info($file);
        }
        else {
            $info = get_mp4info($file);
        }
warn $title;
warn $date;
warn $info;
        if ($@) {
            die 'get info failed: ', $@;
        }
        push @out, sprintf $xml, $title, $date, $file, $info->{SIZE};
    }
}

# Read existing index.xml if it exists
my $index_file = 'index.xml';
my @existing_lines;
if (-e $index_file) {
    open my $fh, '<', $index_file or die "Cannot open $index_file: $!";
    @existing_lines = <$fh>;
    close $fh;
    
    # Remove closing tags from the end
    while (@existing_lines && $existing_lines[-1] =~ /^\s*<\/(channel|rss)>\s*$/) {
        pop @existing_lines;
    }
}

# Write back to index.xml with new items inserted before closing tags
if (@out) {
    open my $fh, '>', $index_file or die "Cannot write to $index_file: $!";
    print $fh @existing_lines;
    print $fh join "\n", @out;
    print $fh "\n</channel>\n</rss>\n";
    close $fh;
}
