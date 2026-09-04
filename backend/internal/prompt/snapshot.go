package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type PromptSnapshot struct {
	Version      string          `json:"version"`
	Role         Role            `json:"role"`
	MaxTokens    int             `json:"max_tokens"`
	Sections     []PromptSection `json:"sections"`
	Rendered     string          `json:"rendered"`
	Hash         string          `json:"hash"`
	OmittedCount int             `json:"omitted_count"`
}

func (s PromptSnapshot) StableBytes() ([]byte, error) {
	clone := s
	clone.Hash = ""
	return json.Marshal(clone)
}

func (s PromptSnapshot) ComputeHash() (string, error) {
	b, err := s.StableBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
