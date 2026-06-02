# YouTube Live Stream Downloader - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI tool that downloads YouTube live streams from the start using fragment generation.

**Architecture:** Single binary that extracts YouTube stream URLs via innertube API, downloads fragments using `sq/N` pattern, and muxes with ffmpeg.

**Tech Stack:** Go 1.26+, standard library only, ffmpeg for muxing

---

## File Structure

```
Youtube Downloader/
├── main.go          # CLI entry point, flag parsing, orchestration
├── youtube.go       # YouTube innertube API client
├── fragment.go      # Fragment generator and downloader
├── muxer.go         # ffmpeg muxing wrapper
├── go.mod           # Module definition
└── README.md        # Usage documentation
```

---

### Task 1: Initialize Go Module

**Files:**
- Create: `go.mod`

- [ ] **Step 1: Initialize the Go module**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go mod init youtube-live`

- [ ] **Step 2: Verify module creation**

Run: `cat go.mod`
Expected output:
```
module youtube-live

go 1.26
```

- [ ] **Step 3: Commit**

```bash
cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader"
git init
git add go.mod
git commit -m "init: initialize go module"
```

---

### Task 2: YouTube API Client - Basic Structure

**Files:**
- Create: `youtube.go`

- [ ] **Step 1: Create youtube.go with API client struct**

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// YouTube client contexts for innertube API
var clientContexts = []map[string]interface{}{
	{
		"clientName":    "WEB_EMBEDDED_PLAYER",
		"clientVersion": "1.20240101.00.00",
		"hl":            "en",
		"gl":            "US",
	},
	{
		"clientName":    "ANDROID",
		"clientVersion": "19.09.37",
		"androidSdkVersion": 30,
		"hl":            "en",
		"gl":            "US",
	},
}

// VideoInfo contains extracted video metadata
type VideoInfo struct {
	ID             string
	Title          string
	IsLive         bool
	DashManifestURL string
	HLSManifestURL string
}

// ExtractVideoInfo fetches video metadata from YouTube
func ExtractVideoInfo(videoID string) (*VideoInfo, error) {
	for _, ctx := range clientContexts {
		info, err := tryExtractWithClient(videoID, ctx)
		if err != nil {
			continue
		}
		if info.DashManifestURL != "" || info.HLSManifestURL != "" {
			return info, nil
		}
	}
	return nil, fmt.Errorf("could not extract video info for %s", videoID)
}

func tryExtractWithClient(videoID string, clientCtx map[string]interface{}) (*VideoInfo, error) {
	requestBody := map[string]interface{}{
		"videoId": videoID,
		"context": map[string]interface{}{
			"client": clientCtx,
		},
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	apiKey := "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8"
	url := fmt.Sprintf("https://www.youtube.com/youtubei/v1/player?key=%s", apiKey)

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var playerResp map[string]interface{}
	if err := json.Unmarshal(respBody, &playerResp); err != nil {
		return nil, err
	}

	return parsePlayerResponse(videoID, playerResp)
}

func parsePlayerResponse(videoID string, resp map[string]interface{}) (*VideoInfo, error) {
	info := &VideoInfo{ID: videoID}

	videoDetails, _ := resp["videoDetails"].(map[string]interface{})
	if videoDetails != nil {
		info.Title, _ = videoDetails["title"].(string)
		info.IsLive, _ = videoDetails["isLive"].(bool)
	}

	streamingData, _ := resp["streamingData"].(map[string]interface{})
	if streamingData != nil {
		info.DashManifestURL, _ = streamingData["dashManifestUrl"].(string)
		info.HLSManifestURL, _ = streamingData["hlsManifestUrl"].(string)
	}

	return info, nil
}

// ExtractVideoID parses various YouTube URL formats and returns the video ID
func ExtractVideoID(url string) (string, error) {
	patterns := []string{
		`(?:youtube\.com/live/|youtu\.be/|youtube\.com/watch\?v=)([a-zA-Z0-9_-]{11})`,
		`^([a-zA-Z0-9_-]{11})$`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(url)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("invalid YouTube URL: %s", url)
}

// NormalizeDashURL changes LIVE to DVR for full stream history
func NormalizeDashURL(url string) string {
	url = strings.Replace(url, "/playlist_type/LIVE/", "/playlist_type/DVR/", 1)
	return url
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go build ./...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader"
git add youtube.go
git commit -m "feat: add YouTube API client for extracting stream URLs"
```

---

### Task 3: Fragment Generator

**Files:**
- Create: `fragment.go`

- [ ] **Step 1: Create fragment.go with download logic**

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// FragmentDownloader handles downloading fragments from a live stream
type FragmentDownloader struct {
	BaseURL     string
	OutputFile  *os.File
	HTTPClient  *http.Client
	LastSeq     int
	PollInterval time.Duration
}

// NewFragmentDownloader creates a new fragment downloader
func NewFragmentDownloader(baseURL, outputPath string) (*FragmentDownloader, error) {
	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}

	return &FragmentDownloader{
		BaseURL:     baseURL,
		OutputFile:  f,
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
		LastSeq:     -1,
		PollInterval: 5 * time.Second,
	}, nil
}

// Close closes the output file
func (fd *FragmentDownloader) Close() error {
	return fd.OutputFile.Close()
}

// DownloadFromStart downloads all fragments from sequence 0 to current position
func (fd *FragmentDownloader) DownloadFromStart() error {
	fmt.Println("Downloading live stream from the start...")

	// First, get the current live position
	currentSeq, err := fd.getCurrentSequence()
	if err != nil {
		return fmt.Errorf("failed to get current position: %w", err)
	}

	fmt.Printf("Current live position: segment %d\n", currentSeq)
	fmt.Println("Downloading segments from 0...")

	// Download from 0 to current position
	for seq := 0; seq <= currentSeq; seq++ {
		if err := fd.downloadFragment(seq); err != nil {
			fmt.Printf("Warning: failed to download segment %d: %v\n", seq, err)
			continue
		}
		if seq%10 == 0 {
			fmt.Printf("Downloaded segment %d/%d\n", seq, currentSeq)
		}
	}

	fmt.Printf("Initial download complete. Now polling for new segments...\n")

	// Poll for new segments
	return fd.pollForNewSegments(currentSeq + 1)
}

// getCurrentSequence gets the current live position from X-Head-Seqnum header
func (fd *FragmentDownloader) getCurrentSequence() (int, error) {
	// Try to get from a test request
	url := fd.buildFragmentURL(0)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := fd.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Read and discard the body
	io.Copy(io.Discard, resp.Body)

	// Check X-Head-Seqnum header
	seqnumStr := resp.Header.Get("X-Head-Seqnum")
	if seqnumStr == "" {
		return 0, fmt.Errorf("X-Head-Seqnum header not found")
	}

	seqnum, err := strconv.Atoi(seqnumStr)
	if err != nil {
		return 0, fmt.Errorf("invalid X-Head-Seqnum value: %s", seqnumStr)
	}

	return seqnum, nil
}

// downloadFragment downloads a single fragment by sequence number
func (fd *FragmentDownloader) downloadFragment(seq int) error {
	url := fd.buildFragmentURL(seq)

	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}

		resp, err := fd.HTTPClient.Do(req)
		if err != nil {
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			return err
		}

		if resp.StatusCode == http.StatusOK {
			_, err := io.Copy(fd.OutputFile, resp.Body)
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			// Segment doesn't exist yet, skip
			return nil
		}

		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}

	return fmt.Errorf("failed to download fragment %d after 3 attempts", seq)
}

// pollForNewSegments continuously polls for new segments
func (fd *FragmentDownloader) pollForNewSegments(startSeq int) error {
	currentSeq := startSeq
	consecutiveEmpty := 0

	for {
		// Get current live position
		newSeq, err := fd.getCurrentSequence()
		if err != nil {
			consecutiveEmpty++
			if consecutiveEmpty > 10 {
				fmt.Println("Stream appears to have ended.")
				return nil
			}
			time.Sleep(fd.PollInterval)
			continue
		}

		if newSeq >= currentSeq {
			consecutiveEmpty = 0
			// Download new segments
			for seq := currentSeq; seq <= newSeq; seq++ {
				if err := fd.downloadFragment(seq); err != nil {
					fmt.Printf("Warning: failed to download segment %d: %v\n", seq, err)
					continue
				}
			}
			if newSeq > currentSeq {
				fmt.Printf("Downloaded segments %d-%d\n", currentSeq, newSeq)
			}
			currentSeq = newSeq + 1
		} else {
			consecutiveEmpty++
			if consecutiveEmpty > 10 {
				fmt.Println("Stream appears to have ended.")
				return nil
			}
		}

		time.Sleep(fd.PollInterval)
	}
}

// buildFragmentURL constructs the fragment URL from base URL and sequence number
func (fd *FragmentDownloader) buildFragmentURL(seq int) string {
	base := strings.TrimRight(fd.BaseURL, "/")
	return fmt.Sprintf("%s/sq/%d", base, seq)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go build ./...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader"
git add fragment.go
git commit -m "feat: add fragment generator for live stream downloading"
```

---

### Task 4: Muxer - ffmpeg Wrapper

**Files:**
- Create: `muxer.go`

- [ ] **Step 1: Create muxer.go with ffmpeg wrapper**

```go
package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Muxer handles merging video and audio streams using ffmpeg
type Muxer struct {
	FFmpegPath string
}

// NewMuxer creates a new muxer instance
func NewMuxer() *Muxer {
	return &Muxer{
		FFmpegPath: "ffmpeg",
	}
}

// MuxStreams merges separate video and audio files into a single MP4
func (m *Muxer) MuxStreams(videoPath, audioPath, outputPath string) error {
	cmd := exec.Command(m.FFmpegPath,
		"-i", videoPath,
		"-i", audioPath,
		"-c:v", "copy",
		"-c:a", "copy",
		"-y",
		outputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// MuxSingleStream remuxes a single stream (when video+audio are combined)
func (m *Muxer) MuxSingleStream(inputPath, outputPath string) error {
	cmd := exec.Command(m.FFmpegPath,
		"-i", inputPath,
		"-c:v", "copy",
		"-c:a", "copy",
		"-y",
		outputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// FormatOutputPath replaces template variables in the output path
func FormatOutputPath(template, title, videoID, ext string) string {
	result := template
	result = strings.Replace(result, "%(title)s", title, -1)
	result = strings.Replace(result, "%(id)s", videoID, -1)
	result = strings.Replace(result, "%(ext)s", ext, -1)
	return result
}

// GetOutputDir returns the directory portion of an output path
func GetOutputDir(outputPath string) string {
	dir := filepath.Dir(outputPath)
	if dir == "" {
		return "."
	}
	return dir
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go build ./...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader"
git add muxer.go
git commit -m "feat: add ffmpeg muxer wrapper for stream merging"
```

---

### Task 5: Main CLI Entry Point

**Files:**
- Create: `main.go`

- [ ] **Step 1: Create main.go with CLI logic**

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const version = "0.1.0"

func main() {
	// Define flags
	outputFlag := flag.String("output", "%(title)s-%(id)s.%(ext)s", "Output file path template")
	listFormats := flag.Bool("list-formats", false, "List available formats")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <URL>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Download YouTube live streams from the start.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s https://www.youtube.com/live/VIDEO_ID\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --output \"%%(title)s.%%(ext)s\" URL\n", os.Args[0])
	}

	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Printf("yt-live version %s\n", version)
		os.Exit(0)
	}

	// Get URL from args
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	url := args[0]

	// Extract video ID
	videoID, err := ExtractVideoID(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Video ID: %s\n", videoID)

	// Extract video info
	fmt.Println("Extracting video info...")
	info, err := ExtractVideoInfo(videoID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting video info: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Title: %s\n", info.Title)
	fmt.Printf("Is Live: %v\n", info.IsLive)

	if !info.IsLive {
		fmt.Println("Warning: This does not appear to be a live stream.")
	}

	// List formats mode
	if *listFormats {
		if info.DashManifestURL != "" {
			fmt.Printf("DASH: %s\n", info.DashManifestURL)
		}
		if info.HLSManifestURL != "" {
			fmt.Printf("HLS: %s\n", info.HLSManifestURL)
		}
		os.Exit(0)
	}

	// Need DASH manifest for live-from-start
	if info.DashManifestURL == "" {
		fmt.Fprintf(os.Stderr, "Error: No DASH manifest found. Cannot download from start.\n")
		fmt.Fprintf(os.Stderr, "This stream may only have HLS, which doesn't support live-from-start.\n")
		os.Exit(1)
	}

	// Normalize URL for DVR
	dashURL := NormalizeDashURL(info.DashManifestURL)

	// TODO: Parse DASH manifest to get fragment base URL
	// For now, use a placeholder
	fmt.Printf("DASH URL: %s\n", dashURL[:100]+"...")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("Starting download...")
	fmt.Println("Press Ctrl+C to stop.")

	// TODO: Implement full download pipeline
	// 1. Parse DASH manifest
	// 2. Extract fragment base URL
	// 3. Download fragments
	// 4. Mux with ffmpeg

	fmt.Println("Download complete!")
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go build ./...`
Expected: No errors

- [ ] **Step 3: Test basic CLI functionality**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go run . --version`
Expected: `yt-live version 0.1.0`

- [ ] **Step 4: Commit**

```bash
cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader"
git add main.go
git commit -m "feat: add CLI entry point with flag parsing"
```

---

### Task 6: DASH Manifest Parser

**Files:**
- Create: `dash.go`

- [ ] **Step 1: Create dash.go with manifest parsing**

```go
package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DASHManifest represents a parsed DASH MPD manifest
type DASHManifest struct {
	Periods []Period `xml:"Period"`
}

type Period struct {
	AdaptationSets []AdaptationSet `xml:"AdaptationSet"`
}

type AdaptationSet struct {
	MimeType    string          `xml:"mimeType,attr"`
	SegmentTemplate *SegmentTemplate `xml:"SegmentTemplate"`
	Representations []Representation `xml:"Representation"`
}

type SegmentTemplate struct {
	Media       string `xml:"media,attr"`
	StartNumber int    `xml:"startNumber,attr"`
	Timescale   int    `xml:"timescale,attr"`
	SegmentTimeline *SegmentTimeline `xml:"SegmentTimeline"`
}

type SegmentTimeline struct {
	Segments []Segment `xml:"S"`
}

type Segment struct {
	T int64 `xml:"t,attr"`
	D int64 `xml:"d,attr"`
}

type Representation struct {
	ID            string `xml:"id,attr"`
	Bandwidth     int    `xml:"bandwidth,attr"`
	Width         int    `xml:"width,attr"`
	Height        int    `xml:"height,attr"`
	BaseURL       string `xml:"BaseURL"`
}

// DashStreamInfo contains info for a single DASH stream
type DashStreamInfo struct {
	RepresentationID string
	Bandwidth         int
	Width             int
	Height            int
	BaseURL           string
	SegmentDuration   time.Duration
	StartNumber       int
}

// ParseDASHManifest fetches and parses a DASH manifest
func ParseDASHManifest(url string) (*DASHManifest, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest DASHManifest
	if err := xml.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// GetVideoStreams extracts video stream info from the manifest
func (m *DASHManifest) GetVideoStreams() []DashStreamInfo {
	var streams []DashStreamInfo

	for _, period := range m.Periods {
		for _, as := range period.AdaptationSets {
			if !strings.HasPrefix(as.MimeType, "video/") {
				continue
			}
			if as.SegmentTemplate == nil {
				continue
			}

			for _, rep := range as.Representations {
				stream := DashStreamInfo{
					RepresentationID: rep.ID,
					Bandwidth:         rep.Bandwidth,
					Width:             rep.Width,
					Height:            rep.Height,
					BaseURL:           rep.BaseURL,
					StartNumber:       as.SegmentTemplate.StartNumber,
				}

				// Calculate segment duration from timeline
				if as.SegmentTemplate.SegmentTimeline != nil && len(as.SegmentTemplate.SegmentTimeline.Segments) > 0 {
					if as.SegmentTemplate.Timescale > 0 {
						firstSeg := as.SegmentTemplate.SegmentTimeline.Segments[0]
						stream.SegmentDuration = time.Duration(firstSeg.D) * time.Second / time.Duration(as.SegmentTemplate.Timescale)
					}
				}

				streams = append(streams, stream)
			}
		}
	}

	return streams
}

// GetAudioStreams extracts audio stream info from the manifest
func (m *DASHManifest) GetAudioStreams() []DashStreamInfo {
	var streams []DashStreamInfo

	for _, period := range m.Periods {
		for _, as := range period.AdaptationSets {
			if !strings.HasPrefix(as.MimeType, "audio/") {
				continue
			}
			if as.SegmentTemplate == nil {
				continue
			}

			for _, rep := range as.Representations {
				stream := DashStreamInfo{
					RepresentationID: rep.ID,
					Bandwidth:         rep.Bandwidth,
					BaseURL:           rep.BaseURL,
					StartNumber:       as.SegmentTemplate.StartNumber,
				}

				if as.SegmentTemplate.SegmentTimeline != nil && len(as.SegmentTemplate.SegmentTimeline.Segments) > 0 {
					if as.SegmentTemplate.Timescale > 0 {
						firstSeg := as.SegmentTemplate.SegmentTimeline.Segments[0]
						stream.SegmentDuration = time.Duration(firstSeg.D) * time.Second / time.Duration(as.SegmentTemplate.Timescale)
					}
				}

				streams = append(streams, stream)
			}
		}
	}

	return streams
}

// BuildFragmentBaseURL constructs the base URL for fragment downloads
func BuildFragmentBaseURL(rep DashStreamInfo) string {
	// YouTube's DASH base URL typically ends with the representation ID
	// We need to append /sq/{N} for fragment downloads
	return rep.BaseURL
}

// ParseSequenceFromHeader extracts sequence number from X-Head-Seqnum header
func ParseSequenceFromHeader(header string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(header))
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go build ./...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader"
git add dash.go
git commit -m "feat: add DASH manifest parser for stream extraction"
```

---

### Task 7: Integrate All Components

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Update main.go to use all components**

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const version = "0.1.0"

func main() {
	// Define flags
	outputFlag := flag.String("output", "%(title)s-%(id)s.%(ext)s", "Output file path template")
	listFormats := flag.Bool("list-formats", false, "List available formats")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <URL>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Download YouTube live streams from the start.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s https://www.youtube.com/live/VIDEO_ID\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --output \"%%(title)s.%%(ext)s\" URL\n", os.Args[0])
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("yt-live version %s\n", version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	url := args[0]

	videoID, err := ExtractVideoID(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Video ID: %s\n", videoID)

	fmt.Println("Extracting video info...")
	info, err := ExtractVideoInfo(videoID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting video info: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Title: %s\n", info.Title)
	fmt.Printf("Is Live: %v\n", info.IsLive)

	if !info.IsLive {
		fmt.Println("Warning: This does not appear to be a live stream.")
	}

	if *listFormats {
		if info.DashManifestURL != "" {
			fmt.Printf("DASH: %s\n", info.DashManifestURL)
		}
		if info.HLSManifestURL != "" {
			fmt.Printf("HLS: %s\n", info.HLSManifestURL)
		}
		os.Exit(0)
	}

	if info.DashManifestURL == "" {
		fmt.Fprintf(os.Stderr, "Error: No DASH manifest found. Cannot download from start.\n")
		os.Exit(1)
	}

	dashURL := NormalizeDashURL(info.DashManifestURL)
	fmt.Println("Parsing DASH manifest...")

	manifest, err := ParseDASHManifest(dashURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing DASH manifest: %v\n", err)
		os.Exit(1)
	}

	videoStreams := manifest.GetVideoStreams()
	audioStreams := manifest.GetAudioStreams()

	if len(videoStreams) == 0 {
		fmt.Fprintf(os.Stderr, "Error: No video streams found in manifest\n")
		os.Exit(1)
	}

	// Select best video stream (highest bandwidth)
	bestVideo := videoStreams[0]
	for _, s := range videoStreams {
		if s.Bandwidth > bestVideo.Bandwidth {
			bestVideo = s
		}
	}

	fmt.Printf("Selected video: %dx%d (%d kbps)\n", bestVideo.Width, bestVideo.Height, bestVideo.Bandwidth/1000)

	// Select best audio stream
	var bestAudio *DashStreamInfo
	if len(audioStreams) > 0 {
		bestAudio = &audioStreams[0]
		for _, s := range audioStreams {
			if s.Bandwidth > bestAudio.Bandwidth {
				bestAudio = &s
			}
		}
		fmt.Printf("Selected audio: %d kbps\n", bestAudio.Bandwidth/1000)
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nStopping download...")
		os.Exit(0)
	}()

	// Download video
	videoOutput := fmt.Sprintf("%s_video.ts", videoID)
	videoBaseURL := BuildFragmentBaseURL(bestVideo)

	fmt.Println("Downloading video fragments...")
	videoDownloader, err := NewFragmentDownloader(videoBaseURL, videoOutput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating video downloader: %v\n", err)
		os.Exit(1)
	}
	defer videoDownloader.Close()

	if err := videoDownloader.DownloadFromStart(); err != nil {
		fmt.Fprintf(os.Stderr, "Error downloading video: %v\n", err)
		os.Exit(1)
	}

	// Download audio if separate
	audioOutput := ""
	if bestAudio != nil {
		audioOutput = fmt.Sprintf("%s_audio.ts", videoID)
		audioBaseURL := BuildFragmentBaseURL(*bestAudio)

		fmt.Println("Downloading audio fragments...")
		audioDownloader, err := NewFragmentDownloader(audioBaseURL, audioOutput)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating audio downloader: %v\n", err)
			os.Exit(1)
		}
		defer audioDownloader.Close()

		if err := audioDownloader.DownloadFromStart(); err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading audio: %v\n", err)
			os.Exit(1)
		}
	}

	// Mux streams
	outputPath := FormatOutputPath(*outputFlag, info.Title, videoID, "mp4")
	muxer := NewMuxer()

	fmt.Println("Muxing streams...")
	if audioOutput != "" {
		err = muxer.MuxStreams(videoOutput, audioOutput, outputPath)
	} else {
		err = muxer.MuxSingleStream(videoOutput, outputPath)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error muxing streams: %v\n", err)
		os.Exit(1)
	}

	// Clean up temp files
	os.Remove(videoOutput)
	if audioOutput != "" {
		os.Remove(audioOutput)
	}

	fmt.Printf("Download complete: %s\n", outputPath)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go build ./...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader"
git add main.go
git commit -m "feat: integrate all components into full download pipeline"
```

---

### Task 8: Build and Test

**Files:**
- Create: `README.md`

- [ ] **Step 1: Create README.md**

```markdown
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
```

- [ ] **Step 2: Build the binary**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go build -o yt-live`
Expected: Binary `yt-live` created

- [ ] **Step 3: Test help output**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && ./yt-live --help`
Expected: Shows usage information

- [ ] **Step 4: Commit**

```bash
cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader"
git add README.md
git commit -m "docs: add README with usage instructions"
```

---

### Task 9: Final Build and Verify

- [ ] **Step 1: Build release binary**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && go build -o yt-live -ldflags="-s -w" .`

- [ ] **Step 2: Verify binary works**

Run: `cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader" && ./yt-live --version`
Expected: `yt-live version 0.1.0`

- [ ] **Step 3: Final commit**

```bash
cd "/Users/edengilbert/Desktop/DEV/Youtube Downloader"
git add -A
git commit -m "release: v0.1.0 - YouTube live stream downloader"
```
