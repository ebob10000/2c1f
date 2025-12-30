package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a release asset (downloadable file)
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Checksum           string `json:"-"` // Populated separately from checksums file
}

// FetchLatestRelease fetches the latest release from GitHub
func FetchLatestRelease(repo string) (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set User-Agent to avoid GitHub API rate limiting issues
	req.Header.Set("User-Agent", "2c1f-updater")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("GitHub API rate limit exceeded")
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release JSON: %w", err)
	}

	return &release, nil
}

// GetAssetForPlatform finds the correct asset for the given OS and architecture
func GetAssetForPlatform(release *GitHubRelease, goos, goarch string) (*Asset, error) {
	// Map platform/arch to asset naming patterns
	// Expected asset names:
	// - Windows: 2c1f-windows-amd64.exe
	// - macOS Intel: 2c1f-darwin-amd64
	// - macOS Apple Silicon: 2c1f-darwin-arm64
	// - Linux: 2c1f-linux-amd64

	var pattern string
	switch goos {
	case "windows":
		pattern = fmt.Sprintf("2c1f-windows-%s.exe", goarch)
	case "darwin":
		pattern = fmt.Sprintf("2c1f-darwin-%s", goarch)
	case "linux":
		pattern = fmt.Sprintf("2c1f-linux-%s", goarch)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", goos)
	}

	// Find matching asset
	var matchedAsset *Asset
	for i := range release.Assets {
		if strings.Contains(release.Assets[i].Name, pattern) || release.Assets[i].Name == pattern {
			matchedAsset = &release.Assets[i]
			break
		}
	}

	if matchedAsset == nil {
		return nil, fmt.Errorf("no matching asset found for %s/%s (looking for pattern: %s)", goos, goarch, pattern)
	}

	// Try to fetch checksums and populate checksum field
	checksums, err := FetchChecksums(release)
	if err == nil && checksums != nil {
		if checksum, ok := checksums[matchedAsset.Name]; ok {
			matchedAsset.Checksum = checksum
		}
	}

	return matchedAsset, nil
}

// FetchChecksums attempts to download and parse the checksums file from the release
// Returns a map of filename -> checksum (SHA256)
func FetchChecksums(release *GitHubRelease) (map[string]string, error) {
	// Look for common checksum file names
	checksumFiles := []string{"SHA256SUMS", "checksums.txt", "CHECKSUMS", "sha256sums.txt"}

	var checksumAsset *Asset
	for _, checksumFileName := range checksumFiles {
		for i := range release.Assets {
			if release.Assets[i].Name == checksumFileName {
				checksumAsset = &release.Assets[i]
				break
			}
		}
		if checksumAsset != nil {
			break
		}
	}

	if checksumAsset == nil {
		// No checksums file found, not necessarily an error
		return nil, nil
	}

	// Download checksums file
	resp, err := http.Get(checksumAsset.BrowserDownloadURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download checksums file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("checksums download failed with status: %d", resp.StatusCode)
	}

	// Parse checksums file (format: "hash  filename" or "hash filename")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read checksums file: %w", err)
	}

	checksums := make(map[string]string)
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse "hash  filename" or "hash filename"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hash := parts[0]
			filename := parts[1]
			// Remove potential leading asterisk from BSD-style checksums
			filename = strings.TrimPrefix(filename, "*")
			checksums[filename] = hash
		}
	}

	return checksums, nil
}
