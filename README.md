# yt-live

A lightweight, dependency-free (standard library only) Go binary designed to download active YouTube live streams **from the very beginning** (DVR mode) instead of just from the live edge.

## Features

- **Download From Start:** Normalizes the live DASH manifest into DVR mode to retrieve the stream's full history (up to 12 hours ago).
- **Concurrency:** Concurrently downloads video and audio tracks to prevent stream drift, optimize performance, and avoid rolling buffer expiration.
- **Graceful Interrupts:** Catching `Ctrl+C` triggers context-based cancellation, cleanly closing and merging all partially downloaded fragments into a final playable `.mp4` file and removing temporary files.
- **Bandwidth Optimization:** Sequence polling uses HTTP Range requests (`Range: bytes=0-0`) to fetch only the first byte of a segment rather than downloading whole segments to determine the stream's live edge.
- **Bot Check Mitigation:**
  - Support for Netscape browser cookie files (`--cookies cookies.txt`) to authenticate requests.
  - Safe, automated PO Token and Visitor Data background generation with crash/loop guards.
  - Command-line flags (`--po-token` and `--visitor-data`) to manually supply browser tokens when needed.

## Requirements

- **Go 1.22+** (Standard library only)
- **FFmpeg** (For merging audio and video streams. The binary will automatically look for a local `./ffmpeg` executable in its directory before falling back to system `ffmpeg`.)

## Installation

Compile the binary from the source folder:
```bash
go build -o yt-live
```

---

## Usage

### 1. Download a Live Stream (Default)
Attempts to fetch the stream. If YouTube challenges the request for bot verification, the program will fail gracefully and provide instructions:
```bash
./yt-live "https://www.youtube.com/live/VIDEO_ID"
```

### 2. Bypass Bot Check using Cookies (Recommended & Automated)
Export cookies from your active browser session as a Netscape cookie file (e.g. `cookies.txt`) and pass it to the downloader:
```bash
./yt-live --cookies cookies.txt "https://www.youtube.com/live/VIDEO_ID"
```

### 3. Bypass Bot Check using Manual Browser Tokens
Provide the Proof of Origin Token and Visitor Data directly from your browser's Developer Tools (Network tab -> `/v1/player` request):
```bash
./yt-live --po-token "YOUR_PO_TOKEN" --visitor-data "YOUR_VISITOR_DATA" "https://www.youtube.com/live/VIDEO_ID"
```

### Options

```
Options:
  -cookies string
    	Path to Netscape cookies file (e.g. cookies.txt)
  -list-formats
    	List available formats
  -output string
    	Output file path template (default "%(title)s-%(id)s.%(ext)s")
  -po-token string
    	YouTube Proof of Origin token (PO Token)
  -version
    	Show version
  -visitor-data string
    	YouTube Visitor Data header string
```

---

## How it Works

1. **Extraction:** Emulates InnerTube client contexts (`ANDROID`, `WEB_EMBEDDED_PLAYER`) to extract stream metadata.
2. **Normalisation:** Substitutes `/playlist_type/LIVE/` with `/playlist_type/DVR/` in the DASH manifest URL.
3. **Parsing:** Parses the DASH XML schema to identify available representation timelines.
4. **Parallel Downloading:** Spin up concurrent goroutines to download video and audio fragments simultaneously using the `sq/N` pattern starting from sequence `0`.
5. **Muxing:** Spawns an `ffmpeg` subprocess to merge the downloaded tracks into a single `.mp4` container.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
