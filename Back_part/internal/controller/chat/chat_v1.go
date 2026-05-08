package chat

import (
	"context"

	v1 "go_agent/api/chat/v1"
	logicchat "go_agent/internal/logic/chat"
	"go_agent/internal/model"
	chatservice "go_agent/internal/service/chat"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ControllerV1 binds GoFrame chat routes to the chat service.
type ControllerV1 struct {
	chat chatservice.Service
}

// NewV1 creates the V1 chat controller.
func NewV1(
	orchGraph compose.Runnable[[]*schema.Message, *schema.Message],
	logger *zap.Logger,
	redisClient *redis.Client,
	knowledgeAgent adk.Agent,
) *ControllerV1 {
	return &ControllerV1{
		chat: logicchat.NewService(orchGraph, logger, redisClient, knowledgeAgent),
	}
}

func (c *ControllerV1) ChatStream(ctx context.Context, req *v1.ChatStreamReq) (*v1.ChatStreamRes, error) {
	if err := c.chat.Stream(ctx, model.ChatStreamInput{
		SessionID: req.Id,
		Question:  req.Question,
	}); err != nil {
		return nil, err
	}
	return &v1.ChatStreamRes{}, nil
}

func (c *ControllerV1) ChatResumeStream(ctx context.Context, req *v1.ChatResumeStreamReq) (*v1.ChatResumeStreamRes, error) {
	if err := c.chat.Resume(ctx, model.ChatResumeInput{
		SessionID:      req.Id,
		CheckpointID:   req.CheckpointID,
		InterruptIDs:   req.InterruptIDs,
		Approved:       req.Approved,
		Resolved:       req.Resolved,
		Comment:        req.Comment,
		SelectionValue: req.SelectionValue,
	}); err != nil {
		return nil, err
	}
	return &v1.ChatResumeStreamRes{}, nil
}

func (c *ControllerV1) FileUpload(ctx context.Context, req *v1.FileUploadReq) (*v1.FileUploadRes, error) {
	return c.chat.FileUpload(ctx, req)
}
