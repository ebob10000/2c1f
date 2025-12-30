package words

import (
	"testing"
)

func TestGenerate(t *testing.T) {
	code, err := Generate()
	if err != nil {
		t.Fatalf("Generate() failed: %v", err)
	}

	// Check format: should be "###-###-###" (e.g., "123-456-789")
	if !Validate(code) {
		t.Errorf("Generate() returned invalid code format: %s", code)
	}

	// Check length: should be 11 characters (3 digits + hyphen + 3 digits + hyphen + 3 digits)
	if len(code) != 11 {
		t.Errorf("Generate() returned code with length %d, want 11: %s", len(code), code)
	}
}

func TestGenerate_Uniqueness(t *testing.T) {
	// Generate many codes and check they're not all the same
	codes := make(map[string]bool)
	iterations := 100

	for i := 0; i < iterations; i++ {
		code, err := Generate()
		if err != nil {
			t.Fatalf("Generate() failed: %v", err)
		}
		codes[code] = true
	}

	// We should have at least some variety (not all the same)
	// With 1,000,000 possible codes, collisions in 100 iterations are rare
	if len(codes) < iterations/2 {
		t.Errorf("Generate() generated only %d unique codes out of %d iterations", len(codes), iterations)
	}
}

func TestGenerate_NonZero(t *testing.T) {
	// The code skips 000-000-000, so let's generate many and ensure we never get it
	for i := 0; i < 100; i++ {
		code, err := Generate()
		if err != nil {
			t.Fatalf("Generate() failed: %v", err)
		}
		if code == "000-000-000" {
			t.Errorf("Generate() returned 000-000-000 which should be skipped")
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		code  string
		valid bool
	}{
		{"123-456-789", true},
		{"000-000-001", true},
		{"999-999-999", true},
		{"001-002-003", true},
		{"12-456-789", false},    // too few digits in first part
		{"1234-456-789", false},  // too many digits in first part
		{"123-45-789", false},    // too few digits in second part
		{"123-4567-789", false},  // too many digits in second part
		{"123-456-78", false},    // too few digits in third part
		{"123-456-7890", false},  // too many digits in third part
		{"123456789", false},     // no hyphens
		{"123-456", false},       // old format (only 2 parts)
		{"abc-def-ghi", false},   // not digits
		{"123_456_789", false},   // wrong separator
		{"", false},              // empty
		{"-", false},             // just hyphen
		{"--", false},            // just hyphens
	}

	for _, tt := range tests {
		result := Validate(tt.code)
		if result != tt.valid {
			t.Errorf("Validate(%q) = %v, want %v", tt.code, result, tt.valid)
		}
	}
}

func BenchmarkGenerate(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = Generate()
	}
}
