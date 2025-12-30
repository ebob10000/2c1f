package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		version  string
		expected [3]int
	}{
		{"1.2.3", [3]int{1, 2, 3}},
		{"0.0.1", [3]int{0, 0, 1}},
		{"10.20.30", [3]int{10, 20, 30}},
		{"1.2", [3]int{1, 2, 0}},
		{"1", [3]int{1, 0, 0}},
		{"invalid", [3]int{0, 0, 0}},
	}

	for _, tt := range tests {
		result := parseVersion(tt.version)
		if result != tt.expected {
			t.Errorf("parseVersion(%q) = %v, want %v", tt.version, result, tt.expected)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current  string
		latest   string
		expected bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.1.0", true},
		{"1.0.0", "2.0.0", true},
		{"1.0.1", "1.0.0", false},
		{"1.1.0", "1.0.0", false},
		{"2.0.0", "1.0.0", false},
		{"1.0.0", "1.0.0", false},
		{"1.2.3", "1.2.4", true},
	}

	for _, tt := range tests {
		result := isNewerVersion(tt.current, tt.latest)
		if result != tt.expected {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tt.current, tt.latest, result, tt.expected)
		}
	}
}

func TestDownloadUpdate_ChecksumVerification(t *testing.T) {
	// Create test file content
	content := []byte("test file content for update")
	hash := sha256.Sum256(content)
	correctChecksum := hex.EncodeToString(hash[:])
	incorrectChecksum := "0000000000000000000000000000000000000000000000000000000000000000"

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	// Test with correct checksum
	t.Run("CorrectChecksum", func(t *testing.T) {
		asset := &Asset{
			Name:               "test-file.bin",
			BrowserDownloadURL: server.URL,
			Size:               int64(len(content)),
			Checksum:           correctChecksum,
		}

		tmpFile, err := DownloadUpdate(asset, nil)
		if err != nil {
			t.Fatalf("DownloadUpdate failed: %v", err)
		}
		defer os.Remove(tmpFile)

		// Verify file was created
		if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
			t.Errorf("Downloaded file does not exist")
		}
	})

	// Test with incorrect checksum
	t.Run("IncorrectChecksum", func(t *testing.T) {
		asset := &Asset{
			Name:               "test-file.bin",
			BrowserDownloadURL: server.URL,
			Size:               int64(len(content)),
			Checksum:           incorrectChecksum,
		}

		tmpFile, err := DownloadUpdate(asset, nil)
		if err == nil {
			os.Remove(tmpFile)
			t.Fatalf("DownloadUpdate should have failed with checksum mismatch")
		}

		// Verify temp file was cleaned up
		if tmpFile != "" {
			if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
				os.Remove(tmpFile)
				t.Errorf("Temp file was not cleaned up after checksum failure")
			}
		}
	})

	// Test without checksum (should still work)
	t.Run("NoChecksum", func(t *testing.T) {
		asset := &Asset{
			Name:               "test-file.bin",
			BrowserDownloadURL: server.URL,
			Size:               int64(len(content)),
			Checksum:           "",
		}

		tmpFile, err := DownloadUpdate(asset, nil)
		if err != nil {
			t.Fatalf("DownloadUpdate failed: %v", err)
		}
		defer os.Remove(tmpFile)
	})
}

func TestDownloadUpdate_SecureTempFile(t *testing.T) {
	content := []byte("test content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	asset := &Asset{
		Name:               "test.exe",
		BrowserDownloadURL: server.URL,
		Size:               int64(len(content)),
	}

	tmpFile, err := DownloadUpdate(asset, nil)
	if err != nil {
		t.Fatalf("DownloadUpdate failed: %v", err)
	}
	defer os.Remove(tmpFile)

	// Verify temp file has random component (not predictable)
	filename := filepath.Base(tmpFile)
	if filename == "2c1f-update.exe" {
		t.Errorf("Temp file name is predictable: %s", filename)
	}

	// Verify it contains the pattern
	matched, err := filepath.Match("2c1f-update-*.exe", filename)
	if err != nil {
		t.Fatalf("filepath.Match failed: %v", err)
	}
	if !matched {
		t.Errorf("Temp file name doesn't match expected pattern: %s", filename)
	}
}

func TestDownloadUpdate_SizeVerification(t *testing.T) {
	content := []byte("test content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	// Test with incorrect size
	asset := &Asset{
		Name:               "test.bin",
		BrowserDownloadURL: server.URL,
		Size:               int64(len(content) + 100), // Wrong size
	}

	tmpFile, err := DownloadUpdate(asset, nil)
	if err == nil {
		os.Remove(tmpFile)
		t.Fatalf("DownloadUpdate should have failed with size mismatch")
	}

	// Verify temp file was cleaned up
	if tmpFile != "" {
		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			os.Remove(tmpFile)
			t.Errorf("Temp file was not cleaned up after size verification failure")
		}
	}
}
