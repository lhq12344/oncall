package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	mu            sync.Mutex
	cfg           Config
	hooks         []Hook
	notifications []Notification
	fired         map[string]bool
	httpClient    *http.Client
}

func NewEngine(configs ...Config) *Engine {
	cfg := Config{Enabled: true}
	if len(configs) > 0 {
		cfg = configs[0]
	}
	cfg = normalizeConfig(cfg)
	return &Engine{
		cfg:        cfg,
		hooks:      append([]Hook(nil), cfg.Hooks...),
		fired:      make(map[string]bool),
		httpClient: &http.Client{},
	}
}

func NewDisabledEngine() *Engine {
	return NewEngine(Config{Enabled: false})
}

func (e *Engine) LoadHooks(hooks []Hook) error {
	if e == nil {
		return nil
	}
	if err := Validate(hooks, e.cfg.WebhookAllowedHosts...); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hooks = append([]Hook(nil), hooks...)
	e.cfg.Hooks = append([]Hook(nil), hooks...)
	e.cfg.Enabled = true
	e.fired = make(map[string]bool)
	return nil
}

func (e *Engine) Enabled() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.Enabled
}

func (e *Engine) RuleCount() int {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.hooks)
}

func (e *Engine) Status() map[string]any {
	if e == nil {
		return map[string]any{"enabled": false, "rules": 0, "notifications": 0}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return map[string]any{
		"enabled":       e.cfg.Enabled,
		"rules":         len(e.hooks),
		"notifications": len(e.notifications),
	}
}

func (e *Engine) RunEvent(ctx context.Context, event EventName, hctx HookContext) []HookResult {
	hctx.EventName = event
	return e.RunHooks(ctx, hctx)
}

func (e *Engine) RunHooks(ctx context.Context, hctx HookContext) []HookResult {
	if e == nil {
		return nil
	}
	snapshot := e.snapshotHooks()
	if len(snapshot) == 0 {
		return nil
	}
	var results []HookResult
	for _, hook := range snapshot {
		if hook.Event != hctx.EventName || !hookEnabled(hook) || !evaluateCondition(hook.Condition, hctx) {
			continue
		}
		if hook.Once && e.markFired(hook.ID) {
			continue
		}
		if hook.Async {
			results = append(results, HookResult{HookID: hook.ID, Event: hctx.EventName, Output: "(async)", Success: true, Async: true})
			go func(h Hook, c HookContext) {
				result := e.executeAction(context.Background(), h, c)
				e.recordNotification(result, c)
			}(hook, hctx)
			continue
		}
		result := e.executeAction(ctx, hook, hctx)
		e.recordNotification(result, hctx)
		results = append(results, result)
	}
	return results
}

func (e *Engine) RunPreToolHooks(ctx context.Context, hctx HookContext) (bool, string) {
	if e == nil {
		return false, ""
	}
	hctx.EventName = EventToolPreUse
	results := e.RunHooks(ctx, hctx)
	for _, result := range results {
		if result.Reject {
			msg := strings.TrimSpace(result.Output)
			if msg == "" {
				msg = "blocked by hook " + result.HookID
			}
			return true, msg
		}
	}
	return false, ""
}

func (e *Engine) DrainNotifications(limit int) []Notification {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if limit <= 0 || limit > len(e.notifications) {
		limit = len(e.notifications)
	}
	out := append([]Notification(nil), e.notifications[:limit]...)
	e.notifications = append([]Notification(nil), e.notifications[limit:]...)
	return out
}

func (e *Engine) PeekNotifications(limit int) []Notification {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if limit <= 0 || limit > len(e.notifications) {
		limit = len(e.notifications)
	}
	return append([]Notification(nil), e.notifications[:limit]...)
}

func (e *Engine) snapshotHooks() []Hook {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.cfg.Enabled {
		return nil
	}
	return append([]Hook(nil), e.hooks...)
}

func (e *Engine) markFired(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fired[id] {
		return true
	}
	e.fired[id] = true
	return false
}

func (e *Engine) recordNotification(result HookResult, hctx HookContext) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	max := e.cfg.MaxNotifications
	if max <= 0 {
		max = defaultMaxNotifications
	}
	e.notifications = append(e.notifications, Notification{Result: result, Context: hctx, At: time.Now()})
	if len(e.notifications) > max {
		e.notifications = append([]Notification(nil), e.notifications[len(e.notifications)-max:]...)
	}
}

func (e *Engine) executeAction(ctx context.Context, hook Hook, hctx HookContext) HookResult {
	result := HookResult{HookID: hook.ID, Event: hctx.EventName, Success: true, Reject: hook.Reject}
	switch hook.Action.Type {
	case ActionLog, ActionAudit, ActionMessage:
		result.Output = hookOutput(hook, hctx)
	case ActionWebhook:
		result = e.runWebhook(ctx, hook, hctx)
	default:
		result.Success = false
		result.Output = fmt.Sprintf("unsupported hook action %q", hook.Action.Type)
	}
	if hook.Reject && hctx.EventName == EventToolPreUse {
		if strings.TrimSpace(hook.RejectReason) != "" {
			result.Output = hook.RejectReason
		} else if strings.TrimSpace(result.Output) == "" {
			result.Output = "blocked by hook " + hook.ID
		}
		result.Reject = true
	}
	if !result.Success && hook.OnError == "ignore" {
		result.Success = true
	}
	if !result.Success && hook.OnError == "reject" && hctx.EventName == EventToolPreUse {
		result.Reject = true
	}
	return result
}

func (e *Engine) runWebhook(ctx context.Context, hook Hook, hctx HookContext) HookResult {
	result := HookResult{HookID: hook.ID, Event: hctx.EventName, Reject: hook.Reject}
	if !e.webhookAllowed(hook.Action.URL) {
		result.Output = "webhook host is not allowlisted"
		return result
	}
	timeout := actionTimeout(hook.Action, time.Duration(e.cfg.DefaultTimeoutMS)*time.Millisecond)
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body := hook.Action.Body
	if body == "" {
		payload := map[string]any{"event": hctx.EventName, "hook_id": hook.ID, "context": redactContext(hctx)}
		data, _ := json.Marshal(payload)
		body = string(data)
	}
	maxBytes := hook.Action.MaxBytes
	if maxBytes <= 0 {
		maxBytes = e.cfg.MaxPayloadBytes
	}
	if maxBytes > 0 && len([]byte(body)) > maxBytes {
		result.Output = fmt.Sprintf("webhook payload exceeds %d bytes", maxBytes)
		return result
	}
	method := strings.ToUpper(strings.TrimSpace(hook.Action.Method))
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(cctx, method, hook.Action.URL, bytes.NewBufferString(body))
	if err != nil {
		result.Output = err.Error()
		return result
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range hook.Action.Headers {
		req.Header.Set(name, value)
	}
	client := e.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		result.Output = err.Error()
		return result
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	result.Output = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	return result
}

func (e *Engine) webhookAllowed(raw string) bool {
	if e == nil {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, allowed := range e.cfg.WebhookAllowedHosts {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return true
		}
	}
	return false
}

func hookEnabled(h Hook) bool {
	return h.Enabled == nil || *h.Enabled
}

func hookOutput(h Hook, ctx HookContext) string {
	if strings.TrimSpace(h.Action.Message) != "" {
		return h.Action.Message
	}
	switch h.Action.Type {
	case ActionLog:
		return fmt.Sprintf("hook %s observed %s", h.ID, ctx.EventName)
	case ActionAudit:
		return fmt.Sprintf("audit %s observed %s", h.ID, ctx.EventName)
	default:
		return ""
	}
}

func redactContext(ctx HookContext) HookContext {
	copied := ctx
	if copied.ToolArgs != nil {
		copied.ToolArgs = redactMap(copied.ToolArgs)
	}
	if copied.Metadata != nil {
		copied.Metadata = redactMap(copied.Metadata)
	}
	return copied
}

func redactMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "authorization") {
			out[key] = "[REDACTED]"
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			out[key] = redactMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}
