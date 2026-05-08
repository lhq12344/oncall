package session

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultTraceTTL = 24 * time.Hour

// OrchestrationTraceEvent records internal graph events outside visible chat memory.
type OrchestrationTraceEvent struct {
	SessionID      string            `json:"session_id,omitempty"`
	TurnID         string            `json:"turn_id,omitempty"`
	CheckpointID   string            `json:"checkpoint_id,omitempty"`
	Source         string            `json:"source,omitempty"`
	Node           string            `json:"node,omitempty"`
	EventType      string            `json:"event_type,omitempty"`
	AgentOrTool    string            `json:"agent_or_tool,omitempty"`
	ToolCallID     string            `json:"tool_call_id,omitempty"`
	Timestamp      string            `json:"timestamp,omitempty"`
	Status         string            `json:"status,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
	CompactPayload string            `json:"compact_payload,omitempty"`
	ErrorSummary   string            `json:"error_summary,omitempty"`
}

// OrchestrationTraceRecorder persists internal graph events for audit/debugging.
type OrchestrationTraceRecorder interface {
	RecordEvent(ctx context.Context, event OrchestrationTraceEvent) error
}

type NoopOrchestrationTraceRecorder struct{}

func (NoopOrchestrationTraceRecorder) RecordEvent(ctx context.Context, event OrchestrationTraceEvent) error {
	return nil
}

type RedisOrchestrationTraceRecorder struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

func NewRedisOrchestrationTraceRecorder(client *redis.Client, prefix string, ttl time.Duration) OrchestrationTraceRecorder {
	if client == nil {
		return NoopOrchestrationTraceRecorder{}
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "oncall"
	}
	if ttl <= 0 {
		ttl = defaultTraceTTL
	}
	return &RedisOrchestrationTraceRecorder{client: client, prefix: prefix, ttl: ttl}
}

func (r *RedisOrchestrationTraceRecorder) RecordEvent(ctx context.Context, event OrchestrationTraceEvent) error {
	if r == nil || r.client == nil {
		return nil
	}
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.TurnID = strings.TrimSpace(event.TurnID)
	if event.SessionID == "" || event.TurnID == "" {
		return nil
	}
	if strings.TrimSpace(event.Timestamp) == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	event.CompactPayload = ClipTraceText(event.CompactPayload)
	event.ErrorSummary = ClipTraceText(event.ErrorSummary)
	event.Tags = normalizeTraceTags(event.Tags)

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	metaKey := r.traceMetaKey(event.SessionID, event.TurnID)
	eventsKey := r.traceEventsKey(event.SessionID, event.TurnID)
	metaFields := traceMetaFields(event)
	_, err = r.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, metaKey, metaFields)
		pipe.RPush(ctx, eventsKey, string(payload))
		pipe.Expire(ctx, metaKey, r.ttl)
		pipe.Expire(ctx, eventsKey, r.ttl)
		return nil
	})
	return err
}

func (r *RedisOrchestrationTraceRecorder) traceMetaKey(sessionID, turnID string) string {
	return r.prefix + ":trace:" + sessionID + ":" + turnID + ":meta"
}

func (r *RedisOrchestrationTraceRecorder) traceEventsKey(sessionID, turnID string) string {
	return r.prefix + ":trace:" + sessionID + ":" + turnID + ":events"
}

func normalizeTraceTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(tags))
	for key, value := range tags {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		normalized[key] = value
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func traceMetaFields(event OrchestrationTraceEvent) map[string]any {
	fields := map[string]any{
		"session_id":    event.SessionID,
		"turn_id":       event.TurnID,
		"checkpoint_id": event.CheckpointID,
		"source":        event.Source,
		"updated_at":    event.Timestamp,
	}
	tags := normalizeTraceTags(event.Tags)
	if len(tags) == 0 {
		return fields
	}
	if tagsJSON, err := json.Marshal(tags); err == nil {
		fields["tags"] = string(tagsJSON)
	}
	if userLanguage := strings.TrimSpace(tags["user_language"]); userLanguage != "" {
		fields["user_language"] = userLanguage
	}
	return fields
}

func ClipTraceText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")
	value = replacer.Replace(value)
	runes := []rune(value)
	const maxTraceRunes = 1200
	if len(runes) <= maxTraceRunes {
		return value
	}
	return string(runes[:maxTraceRunes]) + "..."
}
