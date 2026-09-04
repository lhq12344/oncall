package context

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/schema"
)

type TranscriptStore interface {
	BuildMessages(ctx context.Context, sessionID string, userMsg *schema.Message, reserveToolsTokens int) ([]*schema.Message, error)
	SaveTurn(ctx context.Context, sessionID string, userMsg *schema.Message, assistantMsg *schema.Message, promptMessages []*schema.Message, promptTokens int, completionTokens int) error
	CompactHistory(ctx context.Context, sessionID string, maxRecentTurns int, summarizeAfterTurns int, summaryMaxRunes int) error
}

type MemoryTranscriptStore struct {
	mu       sync.Mutex
	messages map[string][]*schema.Message
}

func NewMemoryTranscriptStore() *MemoryTranscriptStore {
	return &MemoryTranscriptStore{messages: make(map[string][]*schema.Message)}
}

func (s *MemoryTranscriptStore) BuildMessages(ctx context.Context, sessionID string, userMsg *schema.Message, reserveToolsTokens int) ([]*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	stored := s.messages[sessionID]
	out := make([]*schema.Message, 0, len(stored)+1)
	for _, msg := range stored {
		out = append(out, cloneSchemaMessage(msg))
	}
	if userMsg != nil {
		out = append(out, cloneSchemaMessage(userMsg))
	}
	return out, nil
}

func (s *MemoryTranscriptStore) SaveTurn(ctx context.Context, sessionID string, userMsg *schema.Message, assistantMsg *schema.Message, promptMessages []*schema.Message, promptTokens int, completionTokens int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if userMsg == nil || assistantMsg == nil {
		return fmt.Errorf("user and assistant messages are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[sessionID] = append(s.messages[sessionID], cloneSchemaMessage(userMsg), cloneSchemaMessage(assistantMsg))
	return nil
}

func (s *MemoryTranscriptStore) CompactHistory(ctx context.Context, sessionID string, maxRecentTurns int, summarizeAfterTurns int, summaryMaxRunes int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if maxRecentTurns <= 0 {
		return nil
	}
	maxMessages := maxRecentTurns * 2
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.messages[sessionID]
	if len(stored) > maxMessages {
		s.messages[sessionID] = append([]*schema.Message(nil), stored[len(stored)-maxMessages:]...)
	}
	return nil
}

func cloneSchemaMessage(msg *schema.Message) *schema.Message {
	if msg == nil {
		return nil
	}
	cloned := *msg
	return &cloned
}
