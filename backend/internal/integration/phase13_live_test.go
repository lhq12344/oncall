//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPhaseThirteenLiveSSEEndpoint(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("ONCALL_LIVE_BASE_URL"), "/")
	if baseURL == "" {
		t.Skip("set ONCALL_LIVE_BASE_URL to run live SSE cutover validation")
	}

	if _, err := assertLiveSSEStream(baseURL, "phase13-live-sse", "phase13 live SSE validation: reply briefly", ""); err != nil {
		t.Fatal(err)
	}
}

func TestPhaseThirteenLiveSSEPressureAndReconnect(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("ONCALL_LIVE_BASE_URL"), "/")
	if baseURL == "" {
		t.Skip("set ONCALL_LIVE_BASE_URL to run live SSE pressure validation")
	}
	if !envBool("ONCALL_LIVE_SSE_PRESSURE") {
		t.Skip("set ONCALL_LIVE_SSE_PRESSURE=1 to run live SSE pressure/reconnect validation")
	}

	clients := envInt("ONCALL_LIVE_SSE_CLIENTS", 4)
	reconnects := envInt("ONCALL_LIVE_SSE_RECONNECTS", 2)
	if clients < 1 {
		clients = 1
	}
	if clients > 20 {
		clients = 20
	}
	if reconnects < 1 {
		reconnects = 1
	}
	if reconnects > 10 {
		reconnects = 10
	}

	var failures atomic.Int64
	var wg sync.WaitGroup
	for clientID := 0; clientID < clients; clientID++ {
		clientID := clientID
		wg.Add(1)
		go func() {
			defer wg.Done()
			lastEventID := ""
			for attempt := 0; attempt < reconnects; attempt++ {
				sessionID := fmt.Sprintf("phase13-pressure-%02d", clientID)
				question := fmt.Sprintf("phase13 SSE pressure client %d reconnect %d: reply briefly", clientID, attempt)
				nextEventID, err := assertLiveSSEStream(baseURL, sessionID, question, lastEventID)
				if err != nil {
					failures.Add(1)
					t.Logf("client=%d reconnect=%d: %v", clientID, attempt, err)
					return
				}
				lastEventID = nextEventID
			}
		}()
	}
	wg.Wait()
	if failures.Load() > 0 {
		t.Fatalf("SSE pressure/reconnect validation had %d failed client(s)", failures.Load())
	}
}

func assertLiveSSEStream(baseURL, sessionID, question, lastEventID string) (string, error) {
	body := bytes.NewBufferString(fmt.Sprintf(`{"id":%q,"question":%q}`, sessionID, question))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+liveChatStreamPath(), body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(lastEventID) != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("connect live SSE endpoint: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("live SSE endpoint returned %s", res.Status)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		return "", fmt.Errorf("live SSE content-type=%q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(res.Body)
	seenEventID := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id:") {
			seenEventID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		}
		if isLegacySSEFrame(line) {
			return "", fmt.Errorf("live SSE emitted legacy frame: %q", line)
		}
		if strings.HasPrefix(line, "data:") && strings.Contains(line, "oncall.event/v1") {
			if seenEventID == "" {
				return "", fmt.Errorf("live SSE emitted versioned event without id")
			}
			var event struct {
				Version string         `json:"version"`
				Type    string         `json:"type"`
				Payload map[string]any `json:"payload"`
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return "", fmt.Errorf("decode live SSE RunEvent: %w", err)
			}
			switch event.Type {
			case "error", "run.failed":
				return "", fmt.Errorf("live SSE emitted failure event: %s payload=%v", event.Type, event.Payload)
			case "run.completed", "run.finished":
				return seenEventID, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read live SSE endpoint: %w", err)
	}
	return "", fmt.Errorf("live SSE endpoint did not emit a successful terminal oncall.event/v1 before closing")
}

func isLegacySSEFrame(line string) bool {
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	return data == "["+"DONE"+"]" ||
		data == "["+"ERROR"+"]" ||
		strings.HasPrefix(data, "map"+"[command")
}

func TestLegacySSEFrameDetectionIgnoresQuotedRunEventPayload(t *testing.T) {
	line := `data: {"version":"oncall.event/v1","type":"message.token","payload":{"content":"no legacy [DONE] or [ERROR] sentinel here"}}`
	if isLegacySSEFrame(line) {
		t.Fatalf("quoted content inside versioned JSON must not be treated as a legacy SSE frame")
	}

	for _, legacy := range []string{"data: [" + "DONE" + "]", "data: [" + "ERROR" + "]", "data: map" + "[command:kubectl]"} {
		if !isLegacySSEFrame(legacy) {
			t.Fatalf("legacy frame %q was not detected", legacy)
		}
	}
}

func TestPhaseThirteenLiveOptionalDependencyEndpoints(t *testing.T) {
	for _, target := range []struct {
		name string
		env  string
	}{
		{name: "redis", env: "ONCALL_LIVE_REDIS_ADDR"},
		{name: "elasticsearch", env: "ONCALL_LIVE_ELASTICSEARCH_ADDR"},
		{name: "milvus", env: "ONCALL_LIVE_MILVUS_ADDR"},
		{name: "kubernetes", env: "ONCALL_LIVE_KUBERNETES_ADDR"},
		{name: "cozeloop", env: "ONCALL_LIVE_COZELOOP_ADDR"},
	} {
		t.Run(target.name, func(t *testing.T) {
			addr := os.Getenv(target.env)
			if strings.TrimSpace(addr) == "" {
				t.Skipf("set %s to run live %s fault-injection connectivity validation", target.env, target.name)
			}
			if target.name == "cozeloop" {
				assertCozeLoopHealth(t, addr)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var d net.Dialer
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				t.Fatalf("%s endpoint %s is unreachable: %v", target.name, addr, err)
			}
			_ = conn.Close()
		})
	}
}

func assertCozeLoopHealth(t *testing.T, addr string) {
	t.Helper()
	base := strings.TrimSpace(os.Getenv("ONCALL_LIVE_COZELOOP_URL"))
	if base == "" {
		base = strings.TrimSpace(addr)
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			base = "http://" + base
		}
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		t.Fatalf("invalid CozeLoop URL/address %q: %v", base, err)
	}
	if isOfficialCozeLoopHost(parsed.Hostname()) {
		assertTCPReachable(t, "cozeloop", parsed.Host)
		return
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/ping"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		t.Fatalf("build CozeLoop ping request: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CozeLoop ping %s failed: %v", parsed.String(), err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		t.Fatalf("CozeLoop ping %s returned %s: %s", parsed.String(), res.Status, strings.TrimSpace(string(data)))
	}
	body := strings.ToLower(string(data))
	if !strings.Contains(body, "pong") && !strings.Contains(body, "coze loop") {
		t.Fatalf("CozeLoop ping %s returned unexpected body: %s", parsed.String(), strings.TrimSpace(string(data)))
	}
}

func assertTCPReachable(t *testing.T, name, addr string) {
	t.Helper()
	if _, _, err := net.SplitHostPort(addr); err != nil {
		if strings.Contains(err.Error(), "missing port") {
			addr = net.JoinHostPort(addr, "443")
		} else {
			t.Fatalf("invalid %s endpoint %q: %v", name, addr, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("%s endpoint %s is unreachable: %v", name, addr, err)
	}
	_ = conn.Close()
}

func isOfficialCozeLoopHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "api.coze.cn" || strings.HasSuffix(host, ".coze.cn")
}

func Example() {
	fmt.Println("ONCALL_LIVE_BASE_URL=http://127.0.0.1:6872")
	fmt.Println("ONCALL_LIVE_CHAT_STREAM=/api/v1/chat_stream")
	fmt.Println("ONCALL_LIVE_SSE_PRESSURE=1")
	fmt.Println("ONCALL_LIVE_SSE_CLIENTS=4")
	fmt.Println("ONCALL_LIVE_SSE_RECONNECTS=2")
	fmt.Println("ONCALL_LIVE_REDIS_ADDR=127.0.0.1:6379")
	fmt.Println("ONCALL_LIVE_ELASTICSEARCH_ADDR=127.0.0.1:9200")
	fmt.Println("ONCALL_LIVE_MILVUS_ADDR=127.0.0.1:19530")
	fmt.Println("ONCALL_LIVE_KUBERNETES_ADDR=127.0.0.1:6443")
	fmt.Println("ONCALL_LIVE_COZELOOP_ADDR=127.0.0.1:18082")
	// Output:
	// ONCALL_LIVE_BASE_URL=http://127.0.0.1:6872
	// ONCALL_LIVE_CHAT_STREAM=/api/v1/chat_stream
	// ONCALL_LIVE_SSE_PRESSURE=1
	// ONCALL_LIVE_SSE_CLIENTS=4
	// ONCALL_LIVE_SSE_RECONNECTS=2
	// ONCALL_LIVE_REDIS_ADDR=127.0.0.1:6379
	// ONCALL_LIVE_ELASTICSEARCH_ADDR=127.0.0.1:9200
	// ONCALL_LIVE_MILVUS_ADDR=127.0.0.1:19530
	// ONCALL_LIVE_KUBERNETES_ADDR=127.0.0.1:6443
	// ONCALL_LIVE_COZELOOP_ADDR=127.0.0.1:18082
}

func liveChatStreamPath() string {
	path := strings.TrimSpace(os.Getenv("ONCALL_LIVE_CHAT_STREAM"))
	if path == "" {
		return "/api/v1/chat_stream"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	return value
}
