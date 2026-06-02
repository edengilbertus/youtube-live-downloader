package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

const version = "0.1.0"

func main() {
	// Define flags
	outputFlag := flag.String("output", "%(title)s-%(id)s.%(ext)s", "Output file path template")
	listFormats := flag.Bool("list-formats", false, "List available formats")
	showVersion := flag.Bool("version", false, "Show version")
	poTokenFlag := flag.String("po-token", "", "YouTube Proof of Origin token (PO Token)")
	visitorDataFlag := flag.String("visitor-data", "", "YouTube Visitor Data header string")
	cookiesFlag := flag.String("cookies", "", "Path to Netscape cookies file (e.g. cookies.txt)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <URL>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Download YouTube live streams from the start.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s https://www.youtube.com/live/VIDEO_ID\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --cookies cookies.txt https://www.youtube.com/live/VIDEO_ID\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --po-token \"PO_TOKEN_HERE\" --visitor-data \"VISITOR_DATA_HERE\" URL\n", os.Args[0])
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

	// Parse cookies file if provided
	var cookies []*http.Cookie
	if *cookiesFlag != "" {
		var err error
		cookies, err = ParseCookiesFile(*cookiesFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to parse cookies file: %v\n", err)
		} else {
			fmt.Printf("Loaded %d YouTube cookies from %s\n", len(cookies), *cookiesFlag)
		}
	} else {
		// Attempt automatic extraction from Chrome on macOS
		var err error
		cookies, err = ExtractChromeCookies()
		if err == nil && len(cookies) > 0 {
			fmt.Printf("Automatically loaded %d YouTube cookies from Google Chrome\n", len(cookies))
		}
	}

	fmt.Printf("Video ID: %s\n", videoID)

	fmt.Println("Extracting video info...")
	info, err := ExtractVideoInfo(videoID, *poTokenFlag, *visitorDataFlag, cookies)
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

	manifest, err := ParseDASHManifest(dashURL, cookies)
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

	// Create cancelable context for downloads
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nStopping download gracefully... Muxing downloaded fragments...")
		cancel() // Trigger cancellation of download loops
	}()

	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	// Initialize video downloader
	videoOutput := fmt.Sprintf("%s_video.ts", videoID)
	videoBaseURL := BuildFragmentBaseURL(bestVideo)
	videoDownloader, err := NewFragmentDownloader(videoBaseURL, videoOutput, cookies)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating video downloader: %v\n", err)
		os.Exit(1)
	}
	defer videoDownloader.Close()

	// Start video download goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := videoDownloader.DownloadFromStart(ctx); err != nil {
			if err != context.Canceled {
				errChan <- fmt.Errorf("video download error: %w", err)
			}
		}
	}()

	// Initialize and start audio downloader if separate audio stream is present
	var audioDownloader *FragmentDownloader
	audioOutput := ""
	if bestAudio != nil {
		audioOutput = fmt.Sprintf("%s_audio.ts", videoID)
		audioBaseURL := BuildFragmentBaseURL(*bestAudio)
		audioDownloader, err = NewFragmentDownloader(audioBaseURL, audioOutput, cookies)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating audio downloader: %v\n", err)
			cancel()
			wg.Done() // decrement video group since we are exiting early
			os.Exit(1)
		}
		defer audioDownloader.Close()

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := audioDownloader.DownloadFromStart(ctx); err != nil {
				if err != context.Canceled {
					errChan <- fmt.Errorf("audio download error: %w", err)
				}
			}
		}()
	}

	// Monitor downloads completion
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Check for any download errors
	var downloadErr error
	for err := range errChan {
		if err != nil {
			downloadErr = err
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			cancel() // cancel the other downloader if one fails
		}
	}

	// Close file handles explicitly before starting muxing to prevent file locks
	videoDownloader.Close()
	if audioDownloader != nil {
		audioDownloader.Close()
	}

	// Check if video file contains downloaded content to mux
	if fileInfo, err := os.Stat(videoOutput); err == nil && fileInfo.Size() > 0 {
		outputPath := FormatOutputPath(*outputFlag, info.Title, videoID, "mp4")
		muxer := NewMuxer()

		fmt.Println("Muxing streams...")
		var muxErr error
		if audioOutput != "" {
			if audioInfo, err := os.Stat(audioOutput); err == nil && audioInfo.Size() > 0 {
				muxErr = muxer.MuxStreams(videoOutput, audioOutput, outputPath)
			} else {
				fmt.Println("Warning: Audio file is empty or missing. Remuxing video stream only.")
				muxErr = muxer.MuxSingleStream(videoOutput, outputPath)
			}
		} else {
			muxErr = muxer.MuxSingleStream(videoOutput, outputPath)
		}

		if muxErr != nil {
			fmt.Fprintf(os.Stderr, "Error muxing streams: %v\n", muxErr)
		} else {
			fmt.Printf("Muxing complete: %s\n", outputPath)
		}
	} else {
		fmt.Println("No segments were downloaded. Skipping muxing.")
	}

	// Clean up temporary files
	os.Remove(videoOutput)
	if audioOutput != "" {
		os.Remove(audioOutput)
	}

	if downloadErr != nil {
		os.Exit(1)
	}

	fmt.Println("Download process completed.")
}
