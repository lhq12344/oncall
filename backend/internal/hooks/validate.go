package hooks

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultMaxPayloadBytes  = 64 * 1024
	defaultMaxNotifications = 100
	defaultTimeout          = 5 * time.Second
	maxTimeout              = 30 * time.Second
)

var validEvents = map[EventName]bool{
	EventSessionStart:    true,
	EventSessionEnd:      true,
	EventTurnStart:       true,
	EventTurnEnd:         true,
	EventAgentStart:      true,
	EventAgentEnd:        true,
	EventAgentError:      true,
	EventToolPreUse:      true,
	EventToolPostUse:     true,
	EventToolError:       true,
	EventApprovalRequest: true,
	EventResumeRequest:   true,
}

func Validate(hooks []Hook, allowedHosts ...string) error {
	var errs []error
	hostSet := make(map[string]bool, len(allowedHosts))
	for _, host := range allowedHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			hostSet[host] = true
		}
	}
	ids := map[string]bool{}
	for i, hook := range hooks {
		prefix := fmt.Sprintf("hook[%d]", i)
		if strings.TrimSpace(hook.ID) == "" {
			errs = append(errs, fmt.Errorf("%s id is required", prefix))
		} else if ids[hook.ID] {
			errs = append(errs, fmt.Errorf("%s duplicate id %q", prefix, hook.ID))
		}
		ids[hook.ID] = true
		if hook.Event == "" {
			errs = append(errs, fmt.Errorf("%s event is required", prefix))
		} else if !validEvents[hook.Event] {
			errs = append(errs, fmt.Errorf("%s unknown event %q", prefix, hook.Event))
		}
		if hook.Action.Type == "" {
			errs = append(errs, fmt.Errorf("%s action.type is required", prefix))
		}
		switch hook.Action.Type {
		case ActionLog, ActionAudit:
		case ActionMessage:
			if strings.TrimSpace(hook.Action.Message) == "" {
				errs = append(errs, fmt.Errorf("%s action.message must be non-empty", prefix))
			}
		case ActionWebhook:
			if strings.TrimSpace(hook.Action.URL) == "" {
				errs = append(errs, fmt.Errorf("%s action.url must be non-empty", prefix))
				break
			}
			parsed, err := url.Parse(hook.Action.URL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
				errs = append(errs, fmt.Errorf("%s action.url must be a valid http(s) URL", prefix))
				break
			}
			if parsed.Scheme != "https" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" {
				errs = append(errs, fmt.Errorf("%s action.url must use https unless targeting localhost", prefix))
			}
			if len(hostSet) == 0 || !hostSet[strings.ToLower(parsed.Hostname())] {
				errs = append(errs, fmt.Errorf("%s action.url host %q is not allowlisted", prefix, parsed.Hostname()))
			}
			for name := range hook.Action.Headers {
				lower := strings.ToLower(strings.TrimSpace(name))
				if lower == "authorization" || lower == "cookie" || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
					errs = append(errs, fmt.Errorf("%s action.headers contains sensitive header %q", prefix, name))
				}
			}
		case ActionCommand:
			errs = append(errs, fmt.Errorf("%s action.type command is disabled by default", prefix))
		default:
			if hook.Action.Type != "" {
				errs = append(errs, fmt.Errorf("%s unknown action.type %q", prefix, hook.Action.Type))
			}
		}
		if timeout := actionTimeout(hook.Action, defaultTimeout); timeout < 0 || timeout > maxTimeout {
			errs = append(errs, fmt.Errorf("%s action.timeout must be between 0 and %s", prefix, maxTimeout))
		}
		switch strings.TrimSpace(hook.OnError) {
		case "", "fail", "ignore", "reject":
		default:
			errs = append(errs, fmt.Errorf("%s on_error must be fail, ignore, or reject", prefix))
		}
		if hook.Reject && hook.Event != EventToolPreUse {
			errs = append(errs, fmt.Errorf("%s reject is only supported for %s", prefix, EventToolPreUse))
		}
	}
	return errors.Join(errs...)
}

func normalizeConfig(cfg Config) Config {
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = defaultMaxPayloadBytes
	}
	if cfg.MaxNotifications <= 0 {
		cfg.MaxNotifications = defaultMaxNotifications
	}
	if cfg.DefaultTimeoutMS <= 0 {
		cfg.DefaultTimeoutMS = int(defaultTimeout / time.Millisecond)
	}
	return cfg
}

func actionTimeout(action Action, fallback time.Duration) time.Duration {
	timeout := action.Timeout
	if action.TimeoutMS > 0 {
		timeout = time.Duration(action.TimeoutMS) * time.Millisecond
	}
	if timeout == 0 {
		return fallback
	}
	return timeout
}
