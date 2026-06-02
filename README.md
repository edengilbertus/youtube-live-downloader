# yt-live

A YouTube live stream downloader written in Go. Downloads live streams from the start using DASH fragment generation.

## Installation

```bash
go build -o yt-live
```

## Requirements

- Go 1.26+
- ffmpeg (for muxing)

## Usage

```bash
# Download a live stream
./yt-live "https://www.youtube.com/live/VIDEO_ID"

# Custom output path
./yt-live --output "/path/to/%(title)s.%(ext)s" "URL"

# List available formats
./yt-live --list-formats "URL"
```

## How it works

1. Extracts stream URLs from YouTube's innertube API
2. Parses DASH manifest to get fragment base URLs
3. Downloads fragments using `sq/N` pattern from sequence 0
4. Muxes video and audio streams with ffmpeg

## Limitations

- Only supports YouTube live streams
- Requires DASH manifest (not all streams have one)
- YouTube may change their API without notice
