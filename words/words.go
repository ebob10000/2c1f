package words

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// Word list for generating memorable codes (subset of BIP39)
var wordList = []string{
	"apple", "banana", "bucket", "castle", "diamond", "eagle", "falcon", "garden",
	"hammer", "island", "jungle", "kitchen", "lemon", "mountain", "needle", "ocean",
	"planet", "queen", "river", "sunset", "tiger", "umbrella", "village", "winter",
	"yellow", "zebra", "anchor", "bridge", "cloud", "dragon", "engine", "forest",
	"galaxy", "harbor", "ivory", "jacket", "kingdom", "lantern", "marble", "noble",
	"orange", "palace", "quartz", "rainbow", "silver", "temple", "unique", "velvet",
	"wizard", "xenon", "yogurt", "zephyr", "arrow", "beacon", "canyon", "desert",
	"emerald", "flame", "glacier", "hero", "inferno", "jasper", "knight", "lotus",
}

// Generate creates a random 3-word phrase
func Generate() (string, error) {
	words := make([]string, 3)
	for i := 0; i < 3; i++ {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(wordList))))
		if err != nil {
			return "", fmt.Errorf("failed to generate random word: %w", err)
		}
		words[i] = wordList[idx.Int64()]
	}
	return strings.Join(words, "-"), nil
}

// Validate checks if a code has the correct format
func Validate(code string) bool {
	parts := strings.Split(code, "-")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		found := false
		for _, word := range wordList {
			if word == part {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
