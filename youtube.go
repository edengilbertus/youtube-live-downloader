package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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

const tokenJS = `const { fetchVisitorData } = require('youtube-po-token-generator/lib/workflow');
const { url, userAgent } = require('youtube-po-token-generator/lib/consts');
const { JSDOM, VirtualConsole } = require('youtube-po-token-generator/node_modules/jsdom');
const fs = require('fs/promises');
const path = require('path');

function isValidToken(token) {
    if (!token || token.length < 50 || token.length > 500) {
        return false;
    }
    try {
        const decoded = Buffer.from(token, 'base64').toString('utf-8');
        if (decoded.includes('DFO:') || decoded.includes('Error') || decoded.includes('at ')) {
            return false;
        }
    } catch (e) {
        // ignore
    }
    return true;
}

async function main() {
    try {
        const visitorData = await fetchVisitorData();
        
        // Resolve vendor paths
        const pkgDir = path.dirname(require.resolve('youtube-po-token-generator/package.json'));
        const domContent = await fs.readFile(path.join(pkgDir, 'vendor', 'index.html'), 'utf-8');
        const baseContent = await fs.readFile(path.join(pkgDir, 'vendor', 'base.js'), 'utf-8');
        const baseAppendContent = await fs.readFile(path.join(pkgDir, 'lib', 'inject.js'), 'utf-8');
        
        const poToken = await new Promise((resolve, reject) => {
            const { window } = new JSDOM(domContent, {
                url,
                pretendToBeVisual: true,
                runScripts: 'dangerously',
                virtualConsole: new VirtualConsole(),
            });
            Object.defineProperty(window.navigator, 'userAgent', { value: userAgent, writable: false });
            window.visitorData = visitorData;
            
            let isResolved = false;
            window.onPoToken = (token) => {
                if (isResolved) return;
                isResolved = true;
                window.close();
                resolve(token);
            };
            
            // Timeout after 10 seconds for the JSDOM execution
            const timer = setTimeout(() => {
                if (isResolved) return;
                isResolved = true;
                window.close();
                reject(new Error('JSDOM execution timed out'));
            }, 10000);
            
            try {
                window.eval(baseContent.replace(/}\s*\)\(_yt_player\);\s*$/, (matched) => ';'+baseAppendContent+';'+matched));
            } catch (err) {
                clearTimeout(timer);
                if (isResolved) return;
                isResolved = true;
                window.close();
                reject(err);
            }
        });
        
        if (!isValidToken(poToken)) {
            throw new Error('Generated token is invalid (contains script errors or unexpected length)');
        }
        
        console.log(JSON.stringify({ visitorData, poToken }));
        process.exit(0);
    } catch (err) {
        console.error("Error generating token:", err.message || err);
        process.exit(1);
    }
}

main();`

type TokenResult struct {
	VisitorData string `json:"visitorData"`
	POToken     string `json:"poToken"`
}

// getCacheDir returns the path to the centralized ~/.yt-live cache directory
func getCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	cacheDir := filepath.Join(home, ".yt-live")
	_ = os.MkdirAll(cacheDir, 0755)
	return cacheDir
}

// getOrGenerateTokens retrieves provided tokens or generates them using Node.js helper
func getOrGenerateTokens(poToken, visitorData string) (string, string, error) {
	if poToken != "" && visitorData != "" {
		return poToken, visitorData, nil
	}

	fmt.Println("No tokens provided. Attempting to generate PO Token automatically in background...")

	cacheDir := getCacheDir()
	jsPath := filepath.Join(cacheDir, "get_token.js")

	// Write helper JS file to cache dir
	err := os.WriteFile(jsPath, []byte(tokenJS), 0644)
	if err != nil {
		return "", "", fmt.Errorf("failed to write get_token.js: %w", err)
	}

	// Prepare token generation command with a 15-second timeout to prevent infinite hangs
	runCmd := func() (string, string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "node", "get_token.js")
		cmd.Dir = cacheDir // Execute command inside the cache directory
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return "", "", fmt.Errorf("generation timed out after 15s")
			}
			return "", "", fmt.Errorf("node error: %w (stderr: %s)", err, stderr.String())
		}

		var res TokenResult
		if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
			return "", "", fmt.Errorf("failed to parse output: %w (stdout: %s)", err, stdout.String())
		}
		return res.POToken, res.VisitorData, nil
	}

	poToken, visitorData, err = runCmd()
	if err != nil {
		// If npm package is missing, install it in cacheDir and retry
		errMsg := err.Error()
		if strings.Contains(errMsg, "Cannot find module") {
			fmt.Println("youtube-po-token-generator package missing. Installing via npm in ~/.yt-live...")
			installCmd := exec.Command("npm", "install", "youtube-po-token-generator@latest")
			installCmd.Dir = cacheDir // Run npm install in cache directory
			if installErr := installCmd.Run(); installErr != nil {
				return "", "", fmt.Errorf("npm install failed: %w", installErr)
			}
			// Retry running the script
			return runCmd()
		}
		return "", "", err
	}

	fmt.Println("Successfully generated PO Token and Visitor Data!")
	return poToken, visitorData, nil
}

// ExtractVideoInfo fetches video metadata from YouTube, supporting optional PO-Token validation and browser cookies
func ExtractVideoInfo(videoID string, poToken string, visitorData string, cookies []*http.Cookie) (*VideoInfo, error) {
	var err error
	poToken, visitorData, err = getOrGenerateTokens(poToken, visitorData)
	if err != nil {
		fmt.Printf("\n[!] Warning: Automated PO Token generation failed/timed out: %v\n", err)
		fmt.Println("    To bypass YouTube's bot checks manually, please run the tool with browser tokens:")
		fmt.Println("    1. Open Chrome/Firefox Developer Tools (F12) and go to the Network tab.")
		fmt.Println("    2. Navigate to any YouTube video and play it.")
		fmt.Println("    3. Search for the \"/v1/player\" request in the Network log.")
		fmt.Println("    4. Copy 'poToken' (from serviceIntegrityDimensions) and 'visitorData' (from client context).")
		fmt.Println("    5. Run the tool with the flags:")
		fmt.Printf("       ./yt-live --po-token \"COPIED_PO_TOKEN\" --visitor-data \"COPIED_VISITOR_DATA\" https://www.youtube.com/live/%s\n\n", videoID)
		fmt.Println("Attempting download without token...")
	}

	var errs []string
	for _, ctx := range clientContexts {
		info, err := tryExtractWithClient(videoID, ctx, poToken, visitorData, cookies)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", ctx["clientName"], err))
			continue
		}
		if info.DashManifestURL != "" || info.HLSManifestURL != "" {
			return info, nil
		}
		errs = append(errs, fmt.Sprintf("%s: no streaming data URLs in response", ctx["clientName"]))
	}
	return nil, fmt.Errorf("could not extract video info for %s (details: %s)", videoID, strings.Join(errs, "; "))
}

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

func tryExtractWithClient(videoID string, clientCtx map[string]interface{}, poToken string, visitorData string, cookies []*http.Cookie) (*VideoInfo, error) {
	// Deep copy clientCtx to avoid mutating global slice across calls
	ctxCopy := make(map[string]interface{})
	for k, v := range clientCtx {
		ctxCopy[k] = v
	}
	if visitorData != "" {
		ctxCopy["visitorData"] = visitorData
	}

	requestBody := map[string]interface{}{
		"videoId": videoID,
		"context": map[string]interface{}{
			"client": ctxCopy,
		},
	}

	if poToken != "" {
		ctxObj := requestBody["context"].(map[string]interface{})
		ctxObj["serviceIntegrityDimensions"] = map[string]interface{}{
			"poToken": poToken,
		}
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	apiKey := "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8"
	url := fmt.Sprintf("https://www.youtube.com/youtubei/v1/player?key=%s", apiKey)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	if len(cookies) > 0 {
		req.Header.Set("Cookie", BuildCookieHeader(cookies))
	}

	resp, err := httpClient.Do(req)
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

	if playabilityStatus, ok := resp["playabilityStatus"].(map[string]interface{}); ok {
		status, _ := playabilityStatus["status"].(string)
		if status != "OK" {
			reason, _ := playabilityStatus["reason"].(string)
			return nil, fmt.Errorf("playability status: %s (reason: %s)", status, reason)
		}
	} else {
		return nil, fmt.Errorf("missing playabilityStatus in player response")
	}

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
