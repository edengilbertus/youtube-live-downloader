package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// FragmentDownloader handles downloading fragments from a live stream
type FragmentDownloader struct {
	BaseURL      string
	OutputFile   *os.File
	HTTPClient   *http.Client
	LastSeq      int
	PollInterval time.Duration
	Cookies      []*http.Cookie
}

// NewFragmentDownloader creates a new fragment downloader
func NewFragmentDownloader(baseURL, outputPath string, cookies []*http.Cookie) (*FragmentDownloader, error) {
	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}

	return &FragmentDownloader{
		BaseURL:      baseURL,
		OutputFile:   f,
		HTTPClient:   createBrowserHTTPClient(30 * time.Second),
		LastSeq:      -1,
		PollInterval: 5 * time.Second,
		Cookies:      cookies,
	}, nil
}

// Close closes the output file
func (fd *FragmentDownloader) Close() error {
	return fd.OutputFile.Close()
}

// DownloadFromStart downloads all fragments from sequence 0 to current position
func (fd *FragmentDownloader) DownloadFromStart(ctx context.Context) error {
	fmt.Println("Downloading live stream from the start...")

	// First, get the current live position
	currentSeq, err := fd.getCurrentSequence(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current position: %w", err)
	}

	fmt.Printf("Current live position: segment %d\n", currentSeq)
	fmt.Println("Downloading segments from 0...")

	// Download from 0 to current position
	for seq := 0; seq <= currentSeq; seq++ {
		select {
		case <-ctx.Done():
			fmt.Println("Download interrupted.")
			return ctx.Err()
		default:
		}

		if err := fd.downloadFragment(ctx, seq); err != nil {
			fmt.Printf("Warning: failed to download segment %d: %v\n", seq, err)
			continue
		}
		if seq%10 == 0 {
			fmt.Printf("Downloaded segment %d/%d\n", seq, currentSeq)
		}
	}

	fmt.Println("Initial download complete. Now polling for new segments...")

	// Poll for new segments
	return fd.pollForNewSegments(ctx, currentSeq+1)
}

// getCurrentSequence gets the current live position from X-Head-Seqnum header
func (fd *FragmentDownloader) getCurrentSequence(ctx context.Context) (int, error) {
	url := fd.buildFragmentURL(0)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0") // Optimization: request only the first byte to save bandwidth
	if len(fd.Cookies) > 0 {
		req.Header.Set("Cookie", BuildCookieHeader(fd.Cookies))
	}

	resp, err := fd.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Read and discard the partial response body
	io.CopyN(io.Discard, resp.Body, 1)

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
func (fd *FragmentDownloader) downloadFragment(ctx context.Context, seq int) error {
	url := fd.buildFragmentURL(seq)

	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return err
		}
		if len(fd.Cookies) > 0 {
			req.Header.Set("Cookie", BuildCookieHeader(fd.Cookies))
		}

		resp, err := fd.HTTPClient.Do(req)
		if err != nil {
			if attempt < 2 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(attempt+1) * time.Second):
				}
				continue
			}
			return err
		}

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
			_, err := io.Copy(fd.OutputFile, resp.Body)
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return nil
		}

		if attempt < 2 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
	}

	return fmt.Errorf("failed to download fragment %d after 3 attempts", seq)
}

// pollForNewSegments continuously polls for new segments
func (fd *FragmentDownloader) pollForNewSegments(ctx context.Context, startSeq int) error {
	currentSeq := startSeq
	consecutiveEmpty := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		newSeq, err := fd.getCurrentSequence(ctx)
		if err != nil {
			consecutiveEmpty++
			if consecutiveEmpty > 10 {
				fmt.Println("Stream appears to have ended.")
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(fd.PollInterval):
			}
			continue
		}

		if newSeq >= currentSeq {
			consecutiveEmpty = 0
			for seq := currentSeq; seq <= newSeq; seq++ {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				if err := fd.downloadFragment(ctx, seq); err != nil {
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

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(fd.PollInterval):
		}
	}
}

// buildFragmentURL constructs the fragment URL from base URL and sequence number
func (fd *FragmentDownloader) buildFragmentURL(seq int) string {
	u, err := url.Parse(fd.BaseURL)
	if err != nil {
		// Fallback to simple append if parsing fails
		base := strings.TrimRight(fd.BaseURL, "/")
		return fmt.Sprintf("%s/sq/%d", base, seq)
	}

	// Safely append to the path, preserving query parameters
	u.Path = strings.TrimRight(u.Path, "/") + fmt.Sprintf("/sq/%d", seq)
	return u.String()
}
