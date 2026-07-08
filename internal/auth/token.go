package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const tokenPrefix = "obt_" // Omni-BugTracker token

// GenerateAPIToken returns a display token (shown once) and its SHA-256 hash for storage.
func GenerateAPIToken() (plaintext string, hash []byte, err error) {
	raw := make([]byte, 24)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("read random: %w", err)
	}
	plaintext = tokenPrefix + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, sum[:], nil
}

// HashToken hashes a presented token for constant-work lookup by hash.
func HashToken(plaintext string) []byte {
	sum := sha256.Sum256([]byte(plaintext))
	return sum[:]
}

// LooksLikeAPIToken avoids JWKS work for obvious API tokens.
func LooksLikeAPIToken(s string) bool {
	return len(s) > len(tokenPrefix) && s[:len(tokenPrefix)] == tokenPrefix
}
