package v1

import (
	"github.com/gogf/gf/v2/frame/g"
)

type ChatReq struct {
	g.Meta   `path:"/chat" method:"post" summary:"对话"`
	Id       string
	Question string
}

type ChatRes struct {
	Answer string `json:"answer"`
}

type ChatStreamReq struct {
	g.Meta   `path:"/chat_stream" method:"post" summary:"流式对话"`
	Id       string
	Question string
}

type ChatStreamRes struct {
}

type FileUploadReq struct {
	g.Meta `path:"/upload" method:"post" mime:"multipart/form-data" summary:"文件上传"`
}

type FileUploadRes struct {
	FileName string `json:"fileName" dc:"保存的文件名"`
	FilePath string `json:"filePath" dc:"文件保存路径"`
	FileSize int64  `json:"fileSize" dc:"文件大小(字节)"`
}

type AIOpsReq struct {
	g.Meta `path:"/ai_ops" method:"post" summary:"AI运维"`
}

type AIOpsRes struct {
	Result string   `json:"result"`
	Detail []string `json:"detail"`
}

type MonitoringReq struct {
	g.Meta `path:"/monitoring" method:"get" summary:"监控统计"`
}

type MonitoringRes struct {
	CacheHitRate     float64                    `json:"cache_hit_rate" dc:"缓存命中率"`
	CacheHits        int64                      `json:"cache_hits" dc:"缓存命中次数"`
	CacheMisses      int64                      `json:"cache_misses" dc:"缓存未命中次数"`
	CircuitBreakers  []CircuitBreakerStatus     `json:"circuit_breakers" dc:"熔断器状态"`
}

type CircuitBreakerStatus struct {
	Name      string `json:"name" dc:"熔断器名称"`
	State     string `json:"state" dc:"状态: closed/open/half_open"`
	Requests  uint32 `json:"requests" dc:"总请求数"`
	Successes uint32 `json:"successes" dc:"成功次数"`
	Failures  uint32 `json:"failures" dc:"失败次数"`
}

// HealingTriggerReq 触发自愈请求
type HealingTriggerReq struct {
	g.Meta      `path:"/healing/trigger" method:"post" summary:"触发自愈"`
	IncidentID  string `json:"incident_id" v:"required" dc:"故障ID"`
	Type        string `json:"type" v:"required" dc:"故障类型"`
	Severity    string `json:"severity" v:"required" dc:"严重程度"`
	Title       string `json:"title" dc:"故障标题"`
	Description string `json:"description" v:"required" dc:"故障描述"`
}

type HealingTriggerRes struct {
	SessionID string `json:"session_id" dc:"自愈会话ID"`
	Message   string `json:"message" dc:"提示信息"`
}

// HealingStatusReq 查询自愈状态请求
type HealingStatusReq struct {
	g.Meta    `path:"/healing/status" method:"get" summary:"查询自愈状态"`
	SessionID string `json:"session_id" dc:"会话ID（可选，不传则返回所有活跃会话）"`
}

type HealingStatusRes struct {
	Sessions []HealingSessionInfo `json:"sessions" dc:"会话列表"`
}

type HealingSessionInfo struct {
	SessionID   string `json:"session_id" dc:"会话ID"`
	State       string `json:"state" dc:"当前状态"`
	IncidentID  string `json:"incident_id" dc:"故障ID"`
	IncidentType string `json:"incident_type" dc:"故障类型"`
	Severity    string `json:"severity" dc:"严重程度"`
	StartTime   string `json:"start_time" dc:"开始时间"`
	RetryCount  int    `json:"retry_count" dc:"重试次数"`
	RootCause   string `json:"root_cause,omitempty" dc:"根因（如果已诊断）"`
	Strategy    string `json:"strategy,omitempty" dc:"修复策略（如果已决策）"`
}

// HealingHistoryReq 查询历史案例请求
type HealingHistoryReq struct {
	g.Meta       `path:"/healing/history" method:"get" summary:"查询历史案例"`
	IncidentType string `json:"incident_type" dc:"故障类型（可选）"`
	Success      *bool  `json:"success" dc:"是否成功（可选）"`
	Limit        int    `json:"limit" dc:"返回数量限制"`
	Offset       int    `json:"offset" dc:"偏移量"`
}
