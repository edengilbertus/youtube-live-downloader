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
	MimeType        string           `xml:"mimeType,attr"`
	SegmentTemplate *SegmentTemplate `xml:"SegmentTemplate"`
	Representations []Representation `xml:"Representation"`
}

type SegmentTemplate struct {
	Media           string           `xml:"media,attr"`
	StartNumber     int              `xml:"startNumber,attr"`
	Timescale       int              `xml:"timescale,attr"`
	Duration        int64            `xml:"duration,attr"`
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
	ID              string           `xml:"id,attr"`
	Bandwidth       int              `xml:"bandwidth,attr"`
	Width           int              `xml:"width,attr"`
	Height          int              `xml:"height,attr"`
	BaseURL         string           `xml:"BaseURL"`
	SegmentTemplate *SegmentTemplate `xml:"SegmentTemplate"`
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

// ParseDASHManifest fetches and parses a DASH manifest with cookies support
func ParseDASHManifest(url string, cookies []*http.Cookie) (*DASHManifest, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	if len(cookies) > 0 {
		req.Header.Set("Cookie", BuildCookieHeader(cookies))
	}

	resp, err := http.DefaultClient.Do(req)
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

// resolveTemplate helper resolves the correct SegmentTemplate for a representation (handling overrides)
func resolveTemplate(as *AdaptationSet, rep *Representation) *SegmentTemplate {
	if rep.SegmentTemplate != nil {
		return rep.SegmentTemplate
	}
	return as.SegmentTemplate
}

// getSegmentDuration calculates segment duration handling SegmentTimeline and static duration attributes
func getSegmentDuration(template *SegmentTemplate) time.Duration {
	if template == nil {
		return 0
	}
	timescale := template.Timescale
	if timescale <= 0 {
		timescale = 1 // Prevent division by zero
	}

	// 1. Try SegmentTimeline
	if template.SegmentTimeline != nil && len(template.SegmentTimeline.Segments) > 0 {
		firstSeg := template.SegmentTimeline.Segments[0]
		return time.Duration(firstSeg.D) * time.Second / time.Duration(timescale)
	}

	// 2. Try static Duration attribute
	if template.Duration > 0 {
		return time.Duration(template.Duration) * time.Second / time.Duration(timescale)
	}

	return 0
}

// GetVideoStreams extracts video stream info from the manifest
func (m *DASHManifest) GetVideoStreams() []DashStreamInfo {
	var streams []DashStreamInfo

	for _, period := range m.Periods {
		for _, as := range period.AdaptationSets {
			if !strings.HasPrefix(as.MimeType, "video/") {
				continue
			}

			for _, rep := range as.Representations {
				template := resolveTemplate(&as, &rep)
				if template == nil {
					continue
				}

				startNumber := template.StartNumber
				if startNumber == 0 {
					startNumber = 1
				}

				stream := DashStreamInfo{
					RepresentationID: rep.ID,
					Bandwidth:         rep.Bandwidth,
					Width:             rep.Width,
					Height:            rep.Height,
					BaseURL:           rep.BaseURL,
					StartNumber:       startNumber,
					SegmentDuration:   getSegmentDuration(template),
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

			for _, rep := range as.Representations {
				template := resolveTemplate(&as, &rep)
				if template == nil {
					continue
				}

				startNumber := template.StartNumber
				if startNumber == 0 {
					startNumber = 1
				}

				stream := DashStreamInfo{
					RepresentationID: rep.ID,
					Bandwidth:         rep.Bandwidth,
					BaseURL:           rep.BaseURL,
					StartNumber:       startNumber,
					SegmentDuration:   getSegmentDuration(template),
				}

				streams = append(streams, stream)
			}
		}
	}

	return streams
}

// BuildFragmentBaseURL constructs the base URL for fragment downloads
func BuildFragmentBaseURL(rep DashStreamInfo) string {
	return rep.BaseURL
}

// ParseSequenceFromHeader extracts sequence number from X-Head-Seqnum header
func ParseSequenceFromHeader(header string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(header))
}
