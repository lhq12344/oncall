# Project Structure

当前后端按 GoFrame 分层整理为对话、知识库和网络搜索应用。

```text
.
├── main.go                         # 极简入口，仅调用 internal/cmd
├── api/chat/v1/                    # GoFrame 对外接口定义
├── internal/cmd/                   # 启动、配置加载、路由绑定、HTTP server
├── internal/controller/chat/        # GoFrame controller，薄转发到 service
├── internal/service/chat/           # 内部服务接口
├── internal/model/                  # 内部请求/响应模型
├── internal/logic/app/              # 应用依赖初始化
├── internal/logic/chat/             # Chat stream/resume/upload 编排
├── internal/logic/agent/            # dialogue 与 knowledge agent
├── internal/logic/ai/               # model、embedding、retriever、indexer、tokenizer、Milvus client
├── internal/logic/session/          # checkpoint、session memory、Redis memory
├── internal/legacy/opsworkflow/     # 已断开启动路径的旧运维工作流归档
├── utility/middleware/              # HTTP middleware
├── manifest/config/                 # 本地运行配置
├── deploy/                          # Docker Compose 中间件
├── scripts/                         # 本地开发启停脚本
└── Front_page/                      # 前端，当前未迁移
```

## Runtime Flow

1. `main.go` 调用 `internal/cmd.Main.Run(ctx)`。
2. `internal/cmd` 读取配置、初始化 Redis memory、创建 app，并绑定 `/api/v1` 路由。
3. `internal/controller/chat` 只做 GoFrame request/response 适配。
4. `internal/logic/chat` 负责 SSE、checkpoint、resume、session memory 和知识上传编排。
5. `internal/logic/agent/dialogue` 提供对话 agent，默认工具为：
   - `intent_analysis`
   - `request_detail_selection`
   - `knowledge_retrieve`
   - `web_search`
6. `internal/logic/agent/knowledge` 负责文档切分与 Milvus 入库。

## Public Routes

| Route | Method | Purpose |
| --- | --- | --- |
| `/api/v1/chat_stream` | POST | SSE 流式对话 |
| `/api/v1/chat_resume_stream` | POST | SSE 中断恢复 |
| `/api/v1/upload` | POST | 上传 `.txt/.md/.markdown` 到知识库 |

旧 AIOps 路由不再绑定。

## Legacy Area

`internal/legacy/opsworkflow/` 保留旧运维多 Agent 工作流代码，便于后续审计或阶段 2 删除。该目录不属于当前启动路径，不应被新对话/知识/搜索功能引用。

## Validation

后端变更后至少运行：

```bash
GOCACHE=/tmp/go-build-cache go build ./...
GOCACHE=/tmp/go-build-cache go test ./...
```
