package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// ParseCookiesFile reads a Netscape format cookies file and returns a slice of http.Cookies
func ParseCookiesFile(filePath string) ([]*http.Cookie, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open cookies file: %w", err)
	}
	defer file.Close()

	var cookies []*http.Cookie
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}

		domain := fields[0]
		// Filter for youtube.com related cookies to avoid sending unrelated cookies
		if !strings.Contains(domain, "youtube.com") {
			continue
		}

		name := fields[5]
		value := fields[6]

		cookies = append(cookies, &http.Cookie{
			Name:   name,
			Value:  value,
			Domain: domain,
			Path:   fields[2],
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading cookies file: %w", err)
	}

	return cookies, nil
}

// BuildCookieHeader builds a single string suitable for the "Cookie" header
func BuildCookieHeader(cookies []*http.Cookie) string {
	var parts []string
	for _, cookie := range cookies {
		parts = append(parts, fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
	}
	return strings.Join(parts, "; ")
}
