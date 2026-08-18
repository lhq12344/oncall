package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}
