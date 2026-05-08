package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

type ChatStreamReq struct {
	g.Meta   `path:"/chat_stream" method:"post" summary:"流式对话"`
	Id       string `json:"id" v:"required" dc:"会话id"`
	Question string `json:"question" dc:"用户问题"`
}

type ChatStreamRes struct{}

type InterruptContext struct {
	ID          string `json:"id" dc:"中断点ID，用于resume target"`
	Address     string `json:"address" dc:"中断点地址"`
	Info        string `json:"info" dc:"中断信息"`
	IsRootCause bool   `json:"is_root_cause" dc:"是否根因中断点"`
}

type ChatResumeStreamReq struct {
	g.Meta         `path:"/chat_resume_stream" method:"post" summary:"流式恢复中断对话"`
	Id             string   `json:"id" v:"required"`
	CheckpointID   string   `json:"checkpoint_id" v:"required"`
	InterruptIDs   []string `json:"interrupt_ids,omitempty"`
	Approved       *bool    `json:"approved,omitempty"`
	Resolved       *bool    `json:"resolved,omitempty"`
	Comment        string   `json:"comment,omitempty"`
	SelectionValue string   `json:"selection_value,omitempty"`
}

type ChatResumeStreamRes struct{}

type FileUploadReq struct {
	g.Meta `path:"/upload" method:"post" mime:"multipart/form-data" summary:"文件上传"`
}

type FileUploadRes struct {
	FileName string `json:"fileName" dc:"保存的文件名"`
	FilePath string `json:"filePath" dc:"文件保存路径"`
	FileSize int64  `json:"fileSize" dc:"文件大小(字节)"`
}
