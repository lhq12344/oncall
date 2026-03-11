// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package chat

import (
	"context"

	v1 "go_agent/api/chat/v1"
)

type IChatV1 interface {
	ChatStream(ctx context.Context, req *v1.ChatStreamReq) (res *v1.ChatStreamRes, err error)
	ChatResumeStream(ctx context.Context, req *v1.ChatResumeStreamReq) (res *v1.ChatResumeStreamRes, err error)
	FileUpload(ctx context.Context, req *v1.FileUploadReq) (res *v1.FileUploadRes, err error)
	AIOpsStream(ctx context.Context, req *v1.AIOpsStreamReq) (res *v1.AIOpsStreamRes, err error)
	AIOpsResumeStream(ctx context.Context, req *v1.AIOpsResumeStreamReq) (res *v1.AIOpsResumeStreamRes, err error)
	Monitoring(ctx context.Context, req *v1.MonitoringReq) (res *v1.MonitoringRes, err error)
}
