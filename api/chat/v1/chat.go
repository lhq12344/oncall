package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

type ChatStreamReq struct {
	g.Meta   `path:"/chat_stream" method:"post" summary:"流式对话"`
	Id       string
	Question string
}

type ChatStreamRes struct{}

type InterruptContext struct {
	ID          string `json:"id" dc:"中断点ID，用于resume target"`
	Address     string `json:"address" dc:"中断点地址"`
	Info        string `json:"info" dc:"中断信息"`
	IsRootCause bool   `json:"is_root_cause" dc:"是否根因中断点"`
}

type ChatResumeStreamReq struct {
	g.Meta       `path:"/chat_resume_stream" method:"post" summary:"流式恢复中断对话"`
	Id           string   `json:"id" v:"required"`
	CheckpointID string   `json:"checkpoint_id" v:"required"`
	InterruptIDs []string `json:"interrupt_ids,omitempty"`
	Approved     *bool    `json:"approved,omitempty"`
	Resolved     *bool    `json:"resolved,omitempty"`
	Comment      string   `json:"comment,omitempty"`
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

type AIOpsStreamReq struct {
	g.Meta `path:"/ai_ops_stream" method:"post" summary:"AI运维流式"`
}

type AIOpsStreamRes struct{}

type AIOpsResumeStreamReq struct {
	g.Meta       `path:"/ai_ops_resume_stream" method:"post" summary:"AI运维流式恢复中断"`
	CheckpointID string   `json:"checkpoint_id" v:"required"`
	InterruptIDs []string `json:"interrupt_ids,omitempty"`
	Approved     *bool    `json:"approved,omitempty"`
	Resolved     *bool    `json:"resolved,omitempty"`
	Comment      string   `json:"comment,omitempty"`
}

type AIOpsResumeStreamRes struct{}

type MonitoringReq struct {
	g.Meta `path:"/monitoring" method:"get" summary:"监控统计"`
}

type MonitoringRes struct {
	CacheHitRate    float64                `json:"cache_hit_rate" dc:"缓存命中率"`
	CacheHits       int64                  `json:"cache_hits" dc:"缓存命中次数"`
	CacheMisses     int64                  `json:"cache_misses" dc:"缓存未命中次数"`
	CircuitBreakers []CircuitBreakerStatus `json:"circuit_breakers" dc:"熔断器状态"`
}

type CircuitBreakerStatus struct {
	Name      string `json:"name" dc:"熔断器名称"`
	State     string `json:"state" dc:"状态: closed/open/half_open"`
	Requests  uint32 `json:"requests" dc:"总请求数"`
	Successes uint32 `json:"successes" dc:"成功次数"`
	Failures  uint32 `json:"failures" dc:"失败次数"`
}
