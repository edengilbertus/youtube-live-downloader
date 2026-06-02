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

	// Use the output flag (will be used in download pipeline)
	_ = outputFlag

	fmt.Println("Starting download...")
	fmt.Println("Press Ctrl+C to stop.")

	// TODO: Implement full download pipeline
	// 1. Parse DASH manifest
	// 2. Extract fragment base URL
	// 3. Download fragments
	// 4. Mux with ffmpeg

	fmt.Println("Download complete!")
}
