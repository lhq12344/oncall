// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package chat

import (
	"context"

	"go_agent/api/chat/v1"
)

type IChatV1 interface {
	Chat(ctx context.Context, req *v1.ChatReq) (res *v1.ChatRes, err error)
	ChatStream(ctx context.Context, req *v1.ChatStreamReq) (res *v1.ChatStreamRes, err error)
	ChatResume(ctx context.Context, req *v1.ChatResumeReq) (res *v1.ChatResumeRes, err error)
	ChatResumeStream(ctx context.Context, req *v1.ChatResumeStreamReq) (res *v1.ChatResumeStreamRes, err error)
	FileUpload(ctx context.Context, req *v1.FileUploadReq) (res *v1.FileUploadRes, err error)
	AIOps(ctx context.Context, req *v1.AIOpsReq) (res *v1.AIOpsRes, err error)
}
