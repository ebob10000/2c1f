package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// UpdateInfo contains information about an available update
type UpdateInfo struct {
	Version  string `json:"version"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
}

// CheckForUpdates checks if a newer version is available on GitHub
func CheckForUpdates(repo, currentVersion string) (*UpdateInfo, error) {
	release, err := FetchLatestRelease(repo)
	if err != nil {
		return nil, err
	}

	// Compare versions
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if !isNewerVersion(currentVersion, latestVersion) {
		return nil, nil // No update available
	}

	// Find asset for current platform
	asset, err := GetAssetForPlatform(release, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}

	return &UpdateInfo{
		Version:  latestVersion,
		URL:      asset.BrowserDownloadURL,
		Size:     asset.Size,
		Checksum: asset.Checksum,
	}, nil
}

// isNewerVersion compares two semantic version strings
// Returns true if latest > current
func isNewerVersion(current, latest string) bool {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}

	return false // versions are equal
}

// parseVersion parses a version string like "2.2.0" into [major, minor, patch]
func parseVersion(version string) [3]int {
	parts := strings.Split(version, ".")
	var result [3]int

	for i := 0; i < 3 && i < len(parts); i++ {
		num, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err == nil {
			result[i] = num
		}
	}

	return result
}

// DownloadUpdate downloads the update to a temporary file
// progressCallback is called periodically with (downloaded, total) bytes
func DownloadUpdate(asset *Asset, progressCallback func(int64, int64)) (string, error) {
	// Create secure temp file with random name
	tmpDir := os.TempDir()
	out, err := os.CreateTemp(tmpDir, "2c1f-update-*"+filepath.Ext(asset.Name))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpFile := out.Name()
	defer out.Close() // Safe cleanup if early return

	// Download file
	resp, err := http.Get(asset.BrowserDownloadURL)
	if err != nil {
		os.Remove(tmpFile)
		return "", fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		os.Remove(tmpFile)
		return "", fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Hash while downloading
	hasher := sha256.New()
	multiWriter := io.MultiWriter(out, hasher)

	// Copy with progress tracking
	var downloaded int64
	total := asset.Size
	buf := make([]byte, 32*1024) // 32KB buffer

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, writeErr := multiWriter.Write(buf[:n])
			if writeErr != nil {
				os.Remove(tmpFile)
				return "", fmt.Errorf("failed to write file: %w", writeErr)
			}

			downloaded += int64(n)
			if progressCallback != nil {
				progressCallback(downloaded, total)
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			os.Remove(tmpFile)
			return "", fmt.Errorf("failed to read response: %w", err)
		}
	}

	// Verify size
	if downloaded != total {
		os.Remove(tmpFile)
		return "", fmt.Errorf("download incomplete: got %d bytes, expected %d", downloaded, total)
	}

	// Verify checksum if provided
	if asset.Checksum != "" {
		actualHash := hex.EncodeToString(hasher.Sum(nil))
		if actualHash != asset.Checksum {
			os.Remove(tmpFile)
			return "", fmt.Errorf("checksum mismatch: expected %s, got %s", asset.Checksum, actualHash)
		}
	}

	// Ensure file is flushed to disk
	if err := out.Close(); err != nil {
		os.Remove(tmpFile)
		return "", fmt.Errorf("failed to close file: %w", err)
	}

	return tmpFile, nil
}

// ReplaceAndRestart replaces the current executable with the update and restarts
func ReplaceAndRestart(updatePath, currentPath string) error {
	switch runtime.GOOS {
	case "windows":
		return replaceAndRestartWindows(updatePath, currentPath)
	case "darwin", "linux":
		return replaceAndRestartUnix(updatePath, currentPath)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// replaceAndRestartWindows uses a batch script to replace the executable on Windows
func replaceAndRestartWindows(updatePath, currentPath string) error {
	// Create secure temp script with random name
	scriptFile, err := os.CreateTemp(os.TempDir(), "2c1f-update-*.bat")
	if err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}
	scriptPath := scriptFile.Name()

	script := fmt.Sprintf(`@echo off
timeout /t 2 /nobreak > nul
move /y "%s" "%s"
start "" "%s"
del "%%~f0"
`, updatePath, currentPath, currentPath)

	if _, err := scriptFile.WriteString(script); err != nil {
		scriptFile.Close()
		os.Remove(scriptPath)
		return fmt.Errorf("failed to write update script: %w", err)
	}
	scriptFile.Close()

	// Launch script in detached process
	cmd := exec.Command("cmd.exe", "/C", scriptPath)
	cmd.SysProcAttr = getSysProcAttr()

	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to launch update script: %w", err)
	}

	// Exit application immediately
	os.Exit(0)
	return nil
}

// replaceAndRestartUnix uses a shell script to replace the executable on macOS/Linux
func replaceAndRestartUnix(updatePath, currentPath string) error {
	// Create secure temp script with random name
	scriptFile, err := os.CreateTemp(os.TempDir(), "2c1f-update-*.sh")
	if err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}
	scriptPath := scriptFile.Name()

	script := fmt.Sprintf(`#!/bin/bash
sleep 2
mv -f "%s" "%s"
chmod +x "%s"
nohup "%s" > /dev/null 2>&1 &
rm -f "$0"
`, updatePath, currentPath, currentPath, currentPath)

	if _, err := scriptFile.WriteString(script); err != nil {
		scriptFile.Close()
		os.Remove(scriptPath)
		return fmt.Errorf("failed to write update script: %w", err)
	}

	scriptFile.Close()

	// Make script executable
	if err := os.Chmod(scriptPath, 0700); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to make script executable: %w", err)
	}

	// Launch script in background
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.SysProcAttr = getSysProcAttr()

	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to launch update script: %w", err)
	}

	// Exit application immediately
	os.Exit(0)
	return nil
}
