package version

import (
	"testing"
)

func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Errorf("Version should not be empty")
	}
}

func TestVersionFormat(t *testing.T) {
	// Version should be in format like "1.2.3" or "dev"
	// Just check it's a non-empty string for now
	if len(Version) == 0 {
		t.Errorf("Version length is 0")
	}
}
