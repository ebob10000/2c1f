package words

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
)

// Generate creates a random 9-digit code (e.g., "123-456-789")
// This provides ~30 bits of entropy, making brute-force attacks significantly harder
func Generate() (string, error) {
	for {
		// Generate number from 1 to 999,999,999 (~30 bits entropy)
		n, err := rand.Int(rand.Reader, big.NewInt(1000000000))
		if err != nil {
			return "", fmt.Errorf("failed to generate random number: %w", err)
		}
		if n.Int64() == 0 {
			continue
		}
		// Format as 123-456-789 (padding with zeros if needed)
		num := n.Int64()
		part1 := num / 1000000
		part2 := (num / 1000) % 1000
		part3 := num % 1000
		return fmt.Sprintf("%03d-%03d-%03d", part1, part2, part3), nil
	}
}

// Validate checks if a code has the correct format (###-###-###)
func Validate(code string) bool {
	matched, _ := regexp.MatchString(`^\d{3}-\d{3}-\d{3}$`, code)
	return matched
}
