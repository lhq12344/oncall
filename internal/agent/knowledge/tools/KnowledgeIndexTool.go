package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino-ext/components/indexer/milvus"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// KnowledgeIndexTool 知识索引工具
type KnowledgeIndexTool struct {
	indexer *milvus.Indexer
	logger  *zap.Logger
}

func NewKnowledgeIndexTool(idx interface{}, logger *zap.Logger) tool.BaseTool {
	var indexer *milvus.Indexer
	if idx != nil {
		if i, ok := idx.(*milvus.Indexer); ok {
			indexer = i
		}
	}

	return &KnowledgeIndexTool{
		indexer: indexer,
		logger:  logger,
	}
}

func (t *KnowledgeIndexTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "knowledge_index",
		Desc: "将新的故障案例索引到知识库。自动将长文档分片，支持智能向量化和存储。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"content": {
				Type:     schema.String,
				Desc:     "案例内容（包含问题描述、解决方案、结果）。长文档会自动分片。",
				Required: true,
			},
			"title": {
				Type:     schema.String,
				Desc:     "案例标题或文件名",
				Required: false,
			},
			"tags": {
				Type:     schema.String,
				Desc:     "案例标签（逗号分隔）",
				Required: false,
			},
			"chunk_size": {
				Type:     schema.Integer,
				Desc:     "分片大小（字符数），默认 1000。可根据文档类型调��。",
				Required: false,
			},
		}),
	}, nil
}

func (t *KnowledgeIndexTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// 解析参数
	var params struct {
		Content   string `json:"content"`
		Title     string `json:"title"`
		Tags      string `json:"tags"`
		ChunkSize int    `json:"chunk_size"`
	}

	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return "", fmt.Errorf("invalid parameters: %w", err)
	}

	// 参数验证
	if params.Content == "" {
		return "", fmt.Errorf("content is required")
	}

	// 默认分片大小
	if params.ChunkSize <= 0 {
		params.ChunkSize = 1000
	}

	// 如果 indexer 不可用，返回降级响应
	if t.indexer == nil {
		if t.logger != nil {
			t.logger.Warn("knowledge indexer not available, returning degraded response")
		}
		return `{"status":"degraded","indexed":false,"message":"知识索引功能暂时不可用（Milvus 未连接）"}`, nil
	}

	// 智能分片文档
	docs := t.splitDocument(params.Content, params.Title, params.Tags, params.ChunkSize)

	if t.logger != nil {
		t.logger.Info("document split into chunks",
			zap.String("title", params.Title),
			zap.Int("content_length", len(params.Content)),
			zap.Int("chunk_size", params.ChunkSize),
			zap.Int("total_chunks", len(docs)))
	}

	// 批量索引文档
	ids, err := t.indexer.Store(ctx, docs)
	if err != nil {
		if t.logger != nil {
			t.logger.Error("failed to index document", zap.Error(err))
		}
		return fmt.Sprintf(`{"status":"error","indexed":false,"message":"索引失败: %s"}`, err.Error()), nil
	}

	if t.logger != nil {
		t.logger.Info("document indexed successfully",
			zap.Int("content_length", len(params.Content)),
			zap.String("title", params.Title),
			zap.Int("chunks", len(docs)),
			zap.Strings("ids", ids))
	}

	return fmt.Sprintf(`{"status":"success","indexed":true,"message":"文档已成功索引到知识库","chunks":%d,"doc_ids":%s,"content_length":%d}`,
		len(docs), toJSONArray(ids), len(params.Content)), nil
}

// splitDocument 智能分片文档（支持 Markdown 一级标题分片）
func (t *KnowledgeIndexTool) splitDocument(content, title, tags string, chunkSize int) []*schema.Document {
	var docs []*schema.Document

	// 尝试按 Markdown 一级标题分片
	if markdownChunks := t.splitByMarkdownHeaders(content); len(markdownChunks) > 1 {
		if t.logger != nil {
			t.logger.Info("using markdown header-based splitting",
				zap.Int("chunks", len(markdownChunks)))
		}
		return t.createDocumentsFromChunks(markdownChunks, title, tags)
	}

	// 如果没有 Markdown 标题，使用固定大小分片
	if t.logger != nil {
		t.logger.Info("using fixed-size splitting",
			zap.Int("chunk_size", chunkSize))
	}

	contentRunes := []rune(content)
	totalChunks := (len(contentRunes) + chunkSize - 1) / chunkSize

	timestamp := time.Now().Unix()

	for i := 0; i < len(contentRunes); i += chunkSize {
		end := i + chunkSize
		if end > len(contentRunes) {
			end = len(contentRunes)
		}

		chunkContent := string(contentRunes[i:end])
		chunkNum := i/chunkSize + 1

		// 为每个分片添加上下文信息
		if title != "" {
			chunkContent = fmt.Sprintf("[文档: %s, 第 %d/%d 片]\n\n%s", title, chunkNum, totalChunks, chunkContent)
		}

		doc := &schema.Document{
			ID:      fmt.Sprintf("doc_%d_%d", timestamp, chunkNum),
			Content: chunkContent,
			MetaData: map[string]any{
				"title":        title,
				"tags":         tags,
				"chunk":        chunkNum,
				"total_chunks": totalChunks,
				"timestamp":    timestamp,
				"chunk_size":   len(chunkContent),
				"split_type":   "fixed_size",
			},
		}
		docs = append(docs, doc)
	}

	return docs
}

// splitByMarkdownHeaders 按 Markdown 一级标题（# ）分片
func (t *KnowledgeIndexTool) splitByMarkdownHeaders(content string) []string {
	lines := splitLines(content)
	var chunks []string
	var currentChunk []string
	foundHeader := false

	for _, line := range lines {
		// 检测一级标题（# 开头，后面跟空格）
		trimmedLine := trimSpace(line)
		isHeader := len(trimmedLine) >= 2 && trimmedLine[0] == '#' && trimmedLine[1] == ' '

		if isHeader {
			// 如果当前有内容，保存为一个 chunk
			if len(currentChunk) > 0 {
				chunks = append(chunks, joinLines(currentChunk))
				currentChunk = nil
			}
			foundHeader = true
		}

		// 将当前行加入 chunk（包括标题行）
		currentChunk = append(currentChunk, line)
	}

	// 保存最后一个 chunk
	if len(currentChunk) > 0 {
		chunks = append(chunks, joinLines(currentChunk))
	}

	// 如果没有找到任何标题，返回空数组（触发固定大小分片）
	if !foundHeader {
		if t.logger != nil {
			t.logger.Info("no markdown headers found, will use fixed-size splitting")
		}
		return []string{}
	}

	if t.logger != nil {
		t.logger.Info("markdown headers found",
			zap.Int("chunks", len(chunks)),
			zap.Bool("has_headers", foundHeader))
	}

	return chunks
}

// createDocumentsFromChunks 从分片创建文档
func (t *KnowledgeIndexTool) createDocumentsFromChunks(chunks []string, title, tags string) []*schema.Document {
	var docs []*schema.Document
	timestamp := time.Now().Unix()
	totalChunks := len(chunks)

	for i, chunk := range chunks {
		chunkNum := i + 1

		// 提取分片的标题（第一行如果是 # 标题）
		chunkTitle := extractMarkdownTitle(chunk)
		if chunkTitle == "" {
			chunkTitle = fmt.Sprintf("第 %d 部分", chunkNum)
		}

		// 添加上下文信息
		chunkContent := chunk
		if title != "" {
			chunkContent = fmt.Sprintf("[文档: %s, %s, 第 %d/%d 部分]\n\n%s",
				title, chunkTitle, chunkNum, totalChunks, chunk)
		}

		doc := &schema.Document{
			ID:      fmt.Sprintf("doc_%d_%d", timestamp, chunkNum),
			Content: chunkContent,
			MetaData: map[string]any{
				"title":        title,
				"tags":         tags,
				"chunk":        chunkNum,
				"chunk_title":  chunkTitle,
				"total_chunks": totalChunks,
				"timestamp":    timestamp,
				"chunk_size":   len(chunkContent),
				"split_type":   "markdown_header",
			},
		}
		docs = append(docs, doc)
	}

	return docs
}

// extractMarkdownTitle 提取 Markdown 标题
func extractMarkdownTitle(content string) string {
	lines := splitLines(content)
	for _, line := range lines {
		if len(line) >= 2 && line[0] == '#' && line[1] == ' ' {
			// 移除 # 和空格，返回标题文本
			return trimSpace(line[2:])
		}
	}
	return ""
}

// splitLines 分割行
func splitLines(s string) []string {
	var lines []string
	var line []rune
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, string(line))
			line = nil
		} else {
			line = append(line, r)
		}
	}
	if len(line) > 0 {
		lines = append(lines, string(line))
	}
	return lines
}

// joinLines 合并行
func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

// trimSpace 去除首尾空格
func trimSpace(s string) string {
	start := 0
	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}

	return s[start:end]
}

// toJSONArray 将字符串数组转换为 JSON 数组字符串
func toJSONArray(strs []string) string {
	if len(strs) == 0 {
		return "[]"
	}
	result := "["
	for i, s := range strs {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf(`"%s"`, s)
	}
	result += "]"
	return result
}
