package chat

import (
	"context"

	v1 "go_agent/api/chat/v1"
	"go_agent/internal/model"
)

// Service defines the chat module behavior exposed to GoFrame controllers.
type Service interface {
	Stream(ctx context.Context, input model.ChatStreamInput) error
	Resume(ctx context.Context, input model.ChatResumeInput) error
	FileUpload(ctx context.Context, req *v1.FileUploadReq) (*v1.FileUploadRes, error)
}
