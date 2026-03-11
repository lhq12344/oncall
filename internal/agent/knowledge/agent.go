package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Knowledge Agent 配置
type Config struct {
	Indexer      interface{}
	Logger       *zap.Logger
	DefaultChunk int
}

type uploadInput struct {
	Content string         `json:"content"`
	Title   string         `json:"title,omitempty"`
	Tags    string         `json:"tags,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type uploadResult struct {
	IDs []string
}

type KnowledgeUploadAgent struct {
	name        string
	description string
	runnable    compose.Runnable[*uploadInput, *uploadResult]
	logger      *zap.Logger
}

// NewKnowledgeAgent 创建 Knowledge Agent（上传专用 Eino Chain）
func NewKnowledgeAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	runnable, err := BuildKnowledgeUploadChain(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build knowledge upload chain: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("knowledge upload agent created")
	}

	return &KnowledgeUploadAgent{
		name:        "knowledge_agent",
		description: "知识库上传代理，仅负责将文本分片并索引到向量库",
		runnable:    runnable,
		logger:      cfg.Logger,
	}, nil
}

func (a *KnowledgeUploadAgent) Name(ctx context.Context) string {
	return a.name
}

func (a *KnowledgeUploadAgent) Description(ctx context.Context) string {
	return a.description
}

func (a *KnowledgeUploadAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		uploadReq, err := parseUploadInput(input)
		if err != nil {
			gen.Send(&adk.AgentEvent{AgentName: a.name, Err: err})
			return
		}

		result, err := a.runnable.Invoke(ctx, uploadReq)
		if err != nil {
			gen.Send(&adk.AgentEvent{AgentName: a.name, Err: err})
			return
		}

		if a.logger != nil {
			a.logger.Info("knowledge uploaded",
				zap.Int("chunks", len(result.IDs)),
				zap.String("title", uploadReq.Title))
		}

		msg := schema.AssistantMessage(
			fmt.Sprintf("知识上传完成，已入库 %d 个分片。", len(result.IDs)),
			nil,
		)
		event := adk.EventFromMessage(msg, nil, schema.Assistant, "")
		event.AgentName = a.name
		event.Output.CustomizedOutput = map[string]any{
			"indexed": true,
			"ids":     result.IDs,
			"title":   uploadReq.Title,
			"tags":    uploadReq.Tags,
		}
		gen.Send(event)
	}()

	return iter
}

func parseUploadInput(input *adk.AgentInput) (*uploadInput, error) {
	if input == nil || len(input.Messages) == 0 {
		return nil, fmt.Errorf("upload content is required")
	}

	content := latestUserContent(input.Messages)
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("upload content is required")
	}

	var req uploadInput
	if strings.HasPrefix(content, "{") {
		if err := json.Unmarshal([]byte(content), &req); err == nil {
			if strings.TrimSpace(req.Content) != "" {
				return &req, nil
			}
		}
	}

	return &uploadInput{
		Content: content,
		Title:   inferTitle(content),
	}, nil
}

func latestUserContent(messages []adk.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] == nil {
			continue
		}
		if messages[i].Role == schema.User && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}

	return ""
}

func inferTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimSpace(line)
		runes := []rune(line)
		if len(runes) > 48 {
			return string(runes[:48])
		}
		return line
	}
	return "knowledge_upload"
}

func toDocuments(in *uploadInput) []*schema.Document {
	now := time.Now().Unix()
	doc := &schema.Document{
		ID:      fmt.Sprintf("knowledge_%d_1", now),
		Content: strings.TrimSpace(in.Content),
		MetaData: map[string]any{
			"title":      strings.TrimSpace(in.Title),
			"tags":       strings.TrimSpace(in.Tags),
			"upload_at":  time.Now().Format(time.RFC3339),
			"split_type": "raw",
		},
	}

	if in.Meta != nil {
		for k, v := range in.Meta {
			doc.MetaData[k] = v
		}
	}

	return []*schema.Document{doc}
}
