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
