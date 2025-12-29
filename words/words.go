package words

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
)

// Generate creates a random 6-digit code (e.g., "123-456")
func Generate() (string, error) {
	for {
		n, err := rand.Int(rand.Reader, big.NewInt(1000000))
		if err != nil {
			return "", fmt.Errorf("failed to generate random number: %w", err)
		}
		if n.Int64() == 0 {
			continue
		}
		// Format as 123-456 (padding with zeros if needed)
		return fmt.Sprintf("%03d-%03d", n.Int64()/1000, n.Int64()%1000), nil
	}
}

// Validate checks if a code has the correct format (###-###)
func Validate(code string) bool {
	matched, _ := regexp.MatchString(`^\d{3}-\d{3}$`, code)
	return matched
}
