package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// pbkdf2HMACSHA1 derives a key using PBKDF2-HMAC-SHA1 (Go standard library implementation)
func pbkdf2HMACSHA1(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha1.New, password)
	hashLen := sha1.Size
	numBlocks := (keyLen + hashLen - 1) / hashLen

	var buf [4]byte
	dk := make([]byte, 0, numBlocks*hashLen)
	var U, T []byte

	for block := 1; block <= numBlocks; block++ {
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)

		prf.Reset()
		prf.Write(salt)
		prf.Write(buf[:])
		U = prf.Sum(nil)

		T = make([]byte, len(U))
		copy(T, U)

		for j := 2; j <= iter; j++ {
			prf.Reset()
			prf.Write(U)
			U = prf.Sum(nil)
			for k := 0; k < len(T); k++ {
				T[k] ^= U[k]
			}
		}
		dk = append(dk, T...)
	}

	return dk[:keyLen]
}

func getChromeSafeStoragePassword() (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-w", "-s", "Chrome Safe Storage")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("keychain error: %w (stderr: %s)", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func decryptChromeCookie(hexCiphertext string, key []byte) (string, error) {
	ciphertext, err := hex.DecodeString(hexCiphertext)
	if err != nil {
		return "", err
	}

	if len(ciphertext) < 3 {
		return "", fmt.Errorf("ciphertext too short")
	}
	// Strip v10/v11 prefix
	ciphertext = ciphertext[3:]

	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext block size")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	// IV is 16 space characters (0x20)
	iv := bytes.Repeat([]byte{0x20}, aes.BlockSize)

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove PKCS#7 padding
	if len(plaintext) == 0 {
		return "", fmt.Errorf("plaintext is empty")
	}
	paddingLen := int(plaintext[len(plaintext)-1])
	if paddingLen < 1 || paddingLen > aes.BlockSize {
		return "", fmt.Errorf("invalid padding length")
	}
	for _, b := range plaintext[len(plaintext)-paddingLen:] {
		if int(b) != paddingLen {
			return "", fmt.Errorf("invalid padding bytes")
		}
	}

	decrypted := plaintext[:len(plaintext)-paddingLen]

	// Strip the 32-byte signature prefix added in modern Chrome versions on macOS
	if len(decrypted) > 32 {
		decrypted = decrypted[32:]
	}

	return string(decrypted), nil
}

func findChromeCookiesFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	basePath := filepath.Join(home, "Library/Application Support/Google/Chrome")
	var paths []string

	defaultPath := filepath.Join(basePath, "Default/Cookies")
	if _, err := os.Stat(defaultPath); err == nil {
		paths = append(paths, defaultPath)
	}

	files, err := os.ReadDir(basePath)
	if err == nil {
		for _, file := range files {
			if file.IsDir() && strings.HasPrefix(file.Name(), "Profile ") {
				profilePath := filepath.Join(basePath, file.Name(), "Cookies")
				if _, err := os.Stat(profilePath); err == nil {
					paths = append(paths, profilePath)
				}
			}
		}
	}
	return paths
}

// ExtractChromeCookies automatically extracts and decrypts YouTube cookies from local Google Chrome installations on macOS
func ExtractChromeCookies() ([]*http.Cookie, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("automatic Chrome cookie extraction is only supported on macOS")
	}

	password, err := getChromeSafeStoragePassword()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve safe storage password: %w", err)
	}

	// Derive AES key
	salt := []byte("saltysalt")
	key := pbkdf2HMACSHA1([]byte(password), salt, 1003, 16)

	cookieFiles := findChromeCookiesFiles()
	if len(cookieFiles) == 0 {
		return nil, fmt.Errorf("no Google Chrome Cookie databases found")
	}

	var cookies []*http.Cookie

	// Extract cookies from all found profiles
	for _, dbPath := range cookieFiles {
		// Query sqlite3 for YouTube cookies
		query := "SELECT host_key, path, is_secure, expires_utc, name, hex(encrypted_value) FROM cookies WHERE host_key LIKE '%youtube.com';"
		cmd := exec.Command("sqlite3", "-separator", "|", dbPath, query)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			// Skip profiles that can't be read (e.g. locked or permission denied)
			continue
		}

		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			fields := strings.Split(line, "|")
			if len(fields) < 6 {
				continue
			}
			domain := fields[0]
			path := fields[1]
			name := fields[4]
			hexCiphertext := fields[5]

			decrypted, err := decryptChromeCookie(hexCiphertext, key)
			if err != nil {
				continue
			}

			cookies = append(cookies, &http.Cookie{
				Name:   name,
				Value:  decrypted,
				Domain: domain,
				Path:   path,
			})
		}
	}

	if len(cookies) == 0 {
		return nil, fmt.Errorf("no decrypted YouTube cookies found")
	}

	return cookies, nil
}
