package transfer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidatePath(t *testing.T) {
	// Create a temp directory for testing
	tmpDir, err := os.MkdirTemp("", "receiver-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name      string
		path      string
		baseDir   string
		wantError bool
	}{
		{
			name:      "Valid path within base",
			path:      filepath.Join(tmpDir, "subdir", "file.txt"),
			baseDir:   tmpDir,
			wantError: false,
		},
		{
			name:      "Path traversal with ..",
			path:      filepath.Join(tmpDir, "..", "outside.txt"),
			baseDir:   tmpDir,
			wantError: true,
		},
		{
			name:      "Path exactly at base",
			path:      tmpDir,
			baseDir:   tmpDir,
			wantError: false, // Path can be at the base directory
		},
		{
			name:      "Deeply nested valid path",
			path:      filepath.Join(tmpDir, "a", "b", "c", "d", "file.txt"),
			baseDir:   tmpDir,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path, tt.baseDir)
			if (err != nil) != tt.wantError {
				t.Errorf("validatePath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidatePath_SymlinkAttack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink test on Windows (requires admin privileges)")
	}

	// Create temp directories
	tmpDir, err := os.MkdirTemp("", "receiver-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	baseDir := filepath.Join(tmpDir, "base")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("Failed to create base dir: %v", err)
	}

	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("Failed to create outside dir: %v", err)
	}

	// Create a symlink from inside base to outside
	symlinkPath := filepath.Join(baseDir, "link")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Try to access a file through the symlink
	maliciousPath := filepath.Join(symlinkPath, "evil.txt")

	err = validatePath(maliciousPath, baseDir)
	if err == nil {
		t.Errorf("validatePath() should have detected symlink attack")
	}
}

func TestValidatePath_NonExistentPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "receiver-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with non-existent file in valid directory
	nonExistentPath := filepath.Join(tmpDir, "subdir", "newfile.txt")

	err = validatePath(nonExistentPath, tmpDir)
	if err != nil {
		t.Errorf("validatePath() should allow non-existent paths within base: %v", err)
	}
}

func TestValidatePath_AbsolutePaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "receiver-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with absolute paths
	validPath := filepath.Join(tmpDir, "file.txt")

	err = validatePath(validPath, tmpDir)
	if err != nil {
		t.Errorf("validatePath() failed with absolute paths: %v", err)
	}
}

func TestValidatePath_EdgeCases(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "receiver-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name      string
		path      string
		wantError bool
	}{
		{
			name:      "Empty path component",
			path:      filepath.Join(tmpDir, "", "file.txt"),
			wantError: false, // filepath.Join handles this
		},
		{
			name:      "Dot component",
			path:      filepath.Join(tmpDir, ".", "file.txt"),
			wantError: false,
		},
		{
			name:      "Multiple slashes",
			path:      filepath.Join(tmpDir, "subdir", "", "", "file.txt"),
			wantError: false, // filepath.Join normalizes this
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path, tmpDir)
			if (err != nil) != tt.wantError {
				t.Errorf("validatePath() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
