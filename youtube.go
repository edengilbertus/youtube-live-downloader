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
		"clientName":       "ANDROID",
		"clientVersion":    "19.09.37",
		"androidSdkVersion": 30,
		"hl":               "en",
		"gl":               "US",
	},
}

// VideoInfo contains extracted video metadata
type VideoInfo struct {
	ID              string
	Title           string
	IsLive          bool
	DashManifestURL string
	HLSManifestURL  string
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
