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
	ID        string `xml:"id,attr"`
	Bandwidth int    `xml:"bandwidth,attr"`
	Width     int    `xml:"width,attr"`
	Height    int    `xml:"height,attr"`
	BaseURL   string `xml:"BaseURL"`
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
	return rep.BaseURL
}

// ParseSequenceFromHeader extracts sequence number from X-Head-Seqnum header
func ParseSequenceFromHeader(header string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(header))
}
