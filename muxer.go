package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Muxer handles merging video and audio streams using ffmpeg
type Muxer struct {
	FFmpegPath string
}

// NewMuxer creates a new muxer instance, checking first for a local ffmpeg binary
func NewMuxer() *Muxer {
	ffmpegPath := "ffmpeg"
	
	// Check if ffmpeg exists in the current directory or adjacent to the binary
	if _, err := os.Stat("./ffmpeg"); err == nil {
		ffmpegPath = "./ffmpeg"
	} else if exePath, err := os.Executable(); err == nil {
		localPath := filepath.Join(filepath.Dir(exePath), "ffmpeg")
		if _, err := os.Stat(localPath); err == nil {
			ffmpegPath = localPath
		}
	}
	
	return &Muxer{
		FFmpegPath: ffmpegPath,
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
