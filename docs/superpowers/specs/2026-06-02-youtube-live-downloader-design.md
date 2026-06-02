# YouTube Live Stream Downloader - Design Spec

## Overview

A CLI tool written in Go that downloads YouTube live streams from the start. Uses YouTube's innertube API to extract stream URLs, then downloads fragments using the `sq/N` pattern to capture from the beginning of the stream.

## Goals

- Download YouTube live streams from the start (not just the live edge)
- Single binary, no external dependencies except ffmpeg for muxing
- Simple CLI interface
- Handle retries, errors, and network issues gracefully

## Non-Goals

- Regular YouTube video downloads (live streams only)
- Playlist downloads
- GUI or web interface
- Multi-platform support (YouTube only)

## Usage

```bash
# Download a live stream
yt-live download "https://www.youtube.com/live/VIDEO_ID"

# Custom output path
yt-live download --output "/path/to/%(title)s.%(ext)s" "URL"

# List available formats
yt-live list-formats "URL"
```

## Architecture

### Components

1. **YouTube API Client** (`youtube.go`)
   - Makes requests to YouTube's innertube API
   - Extracts player response with stream URLs
   - Handles client rotation to find DASH manifests
   - Returns manifest URLs + metadata

2. **Fragment Generator** (`fragment.go`)
   - Constructs fragment URLs: `{base_url}/sq/{N}`
   - Uses `X-Head-Seqnum` header to find current live position
   - Downloads fragments sequentially from 0 to current position
   - Polls for new fragments every few seconds
   - Handles retries and skip-on-error

3. **Muxer** (`muxer.go`)
   - Concatenates video + audio fragments into a single file
   - Uses ffmpeg to merge separate streams
   - Handles output filename templating

4. **CLI** (`main.go`)
   - Parses flags: `--output`, `--format`, `--no-check-certificate`
   - Orchestrates the pipeline: extract → download → mux
   - Progress display

### Data Flow

```
URL → YouTube API → Stream URLs (video+audio) → Fragment Generator → .ts/.m4s files → ffmpeg mux → .mp4
```

## Implementation Details

### YouTube API

- Endpoint: `https://www.youtube.com/youtubei/v1/player`
- Client contexts to try:
  - `WEB_EMBEDDED_PLAYER`
  - `ANDROID`
  - `IOS`
- Request body includes video ID, client context, and API key
- Response contains `streamingData.dashManifestUrl` and `streamingData.hlsManifestUrl`
- Modify DASH URL: `playlist_type/LIVE` → `playlist_type/DVR`

### Fragment Download

1. Parse DASH manifest to get segment template info
2. Fragment URL pattern: `{base_url}/sq/{sequence_number}`
3. Use `X-Head-Seqnum` header from last fragment request to know current position
4. Download from `sq/0` up to `X-Head-Seqnum`
5. Sleep `segment_duration` seconds, then check for new fragments
6. Stop when stream ends (no new fragments for multiple polls)

### Error Handling

- Retry failed fragment downloads (up to 3 attempts)
- Skip unavailable fragments if `--skip-errors` is set
- Graceful shutdown on Ctrl+C (finalize current file)
- Clear error messages for common issues (private stream, geo-blocked, etc.)

## Dependencies

- Go 1.26+
- ffmpeg (external, for muxing video+audio streams)
- No Go library dependencies (standard library only for MVP)

## File Structure

```
Youtube Downloader/
├── main.go          # CLI entry point
├── youtube.go       # YouTube API client
├── fragment.go      # Fragment generator
├── muxer.go         # ffmpeg muxing
├── go.mod
├── go.sum
├── README.md
└── docs/
    └── superpowers/
        └── specs/
            └── 2026-06-02-youtube-live-downloader-design.md
```
