# Back_part 文件职责说明

本文按 GoFrame 后端分层说明 `Back_part/` 下各目录和主要文件的职责。当前仓库布局为：

```text
My_oncall/
├── Back_part/   # GoFrame + Eino ADK 后端模块
└── Front_page/  # React/Vite 前端模块，当前与 Back_part 同级
```

## GoFrame 分层总览

| 层级 | 路径 | 职责 |
| --- | --- | --- |
| 入口层 | `main.go` | 后端进程入口，只负责创建根上下文并调用 `internal/cmd`。 |
| 启动层 | `internal/cmd/` | 加载配置、初始化依赖、绑定路由、启动 HTTP Server。 |
| API 契约层 | `api/chat/v1/` | GoFrame 请求/响应结构体、路由 path、method、字段校验标签。 |
| 生成接口层 | `api/chat/chat.go` | GoFrame 生成的 Controller 接口定义，不手写业务逻辑。 |
| Controller 层 | `internal/controller/chat/` | 协议适配层，把 GoFrame 请求转发给 service，保持薄控制器。 |
| Service 接口层 | `internal/service/chat/` | 对外暴露业务能力接口，隔离 controller 与 logic 实现。 |
| Logic 层 | `internal/logic/` | 业务编排、Agent、AI 组件、会话、应用依赖初始化。 |
| Model 层 | `internal/model/` | 内部业务输入/输出模型，不直接承载 HTTP 协议。 |
| Utility 层 | `utility/` | 跨层复用的基础设施工具，如 HTTP middleware。 |
| 配置与部署 | `manifest/`, `deploy/`, `scripts/` | 本地配置、Docker Compose 中间件、开发启停脚本。 |

## 根目录文件

| 文件 | 作用 |
| --- | --- |
| `main.go` | 后端入口，调用 `cmd.Main.Run(gctx.New())`。 |
| `go.mod` | Go module 定义，当前模块名为 `go_agent`。 |
| `go.sum` | Go 依赖校验和。 |
| `.env` | 本地环境变量文件，保存模型、Embedding、搜索等运行配置；不得提交真实密钥。 |
| `.gitignore` | 后端模块忽略规则。注意不要误忽略需要版本管理的文档、脚手架配置或生成契约。 |
| `AGENTS.md` | 后端模块协作约束，说明结构、测试和安全要求。 |

## API 契约层

| 文件 | 作用 |
| --- | --- |
| `api/chat/v1/chat.go` | 可编辑接口契约。定义 `/api/v1/chat_stream`、`/api/v1/chat_resume_stream`、`/api/v1/upload` 的请求/响应结构。 |
| `api/chat/chat.go` | GoFrame CLI 生成的接口聚合文件。Controller 需要实现这里的 `IChatV1`。 |

## 启动与路由

| 文件 | 作用 |
| --- | --- |
| `internal/cmd/cmd.go` | 启动主流程：加载 `.env` 和配置、初始化 Redis memory、创建 Application、绑定 `/api/v1` 路由、启动 6872 端口。 |

## Controller 与 Service

| 文件 | 作用 |
| --- | --- |
| `internal/controller/chat/chat.go` | GoFrame CLI 初始化文件，保留 controller 包声明。 |
| `internal/controller/chat/chat_v1.go` | Chat V1 Controller。把流式对话、恢复、上传请求转发到 `internal/logic/chat`。 |
| `internal/service/chat/chat.go` | Chat service 接口定义，包括 `Stream`、`Resume`、`FileUpload`。 |
| `internal/model/chat.go` | 内部 chat 输入模型，解耦 HTTP 请求对象与业务调用参数。 |

## 应用初始化

| 文件 | 作用 |
| --- | --- |
| `internal/logic/app/app.go` | 应用装配入口。初始化日志、Redis、LLM、Embedding、Dialogue Agent、Knowledge Agent，并集中管理关闭逻辑。 |

## Chat 业务编排

| 文件 | 作用 |
| --- | --- |
| `internal/logic/chat/service.go` | Chat 主业务实现。处理 SSE 输出、checkpoint、resume、会话记忆、文件上传和 Knowledge Agent 调用。 |
| `internal/logic/chat/sse_test.go` | SSE 行为测试，保护流式事件输出语义。 |

## Dialogue Agent

| 文件 | 作用 |
| --- | --- |
| `internal/logic/agent/dialogue/agent.go` | 构建对话 Agent，注册意图分析、详情选择、知识检索等工具，并配置 summarization middleware。 |
| `internal/logic/agent/dialogue/tools/IntentAnalysisTool.go` | 意图分析工具，用于判断用户问题方向、置信度和缺失信息。 |
| `internal/logic/agent/dialogue/tools/detail_selection_tool.go` | 详情选择工具，用于在候选项有限时向用户请求结构化选择。 |
| `internal/logic/agent/dialogue/tools/KnowledgeRetrieveTool.go` | 知识库检索工具，从 Milvus 检索相关知识片段。 |
| `internal/logic/agent/dialogue/tools/WebSearchTool.go` | 网络搜索工具，用于外部资料或时效信息检索。 |
| `internal/logic/agent/dialogue/tools/BashApprovalTool.go` | 命令审批工具，归档/运维场景下用于高风险命令审批。 |

## Knowledge Agent

| 文件 | 作用 |
| --- | --- |
| `internal/logic/agent/knowledge/agent.go` | 知识上传 Agent，解析上传内容并触发文档切分和入库。 |
| `internal/logic/agent/knowledge/orchestration.go` | Knowledge Graph/Chain 编排：上传文本落临时文件、加载、切分、Milvus 索引。 |
| `internal/logic/agent/knowledge/loader.go` | 文档加载器封装。 |
| `internal/logic/agent/knowledge/transformer.go` | Markdown/text 切分器封装。 |
| `internal/logic/agent/knowledge/indexer.go` | Knowledge 层 indexer 适配器，调用 AI Milvus indexer。 |
| `internal/logic/agent/knowledge/embedding.go` | Knowledge 侧 Embedding 封装。 |
| `internal/logic/agent/knowledge/ranker.go` | 检索结果排序/重排逻辑。 |

## AI 基础组件

| 文件 | 作用 |
| --- | --- |
| `internal/logic/ai/models/open_ai.go` | OpenAI 兼容 ChatModel 初始化，读取模型 API Key、Base URL、模型名等配置。 |
| `internal/logic/ai/models/open_ai_test.go` | 模型配置解析测试。 |
| `internal/logic/ai/embedder/embedder.go` | 阿里云百炼 DashScope Embedding 模型初始化。 |
| `internal/logic/ai/client/client.go` | Milvus 客户端初始化，负责数据库、collection、索引创建和启动期重试。 |
| `internal/logic/ai/client/client_test.go` | Milvus 启动期可重试错误判断测试。 |
| `internal/logic/ai/common/common.go` | AI 公共常量，如默认数据库、collection、文件目录。 |
| `internal/logic/ai/common/milvus_config.go` | Milvus 配置读取，优先配置文件，再回退环境变量和默认值。 |
| `internal/logic/ai/common/milvus_config_test.go` | Milvus 配置解析测试。 |
| `internal/logic/ai/indexer/indexer.go` | Milvus Indexer 构造，连接 Embedding 与 Milvus 写入。 |
| `internal/logic/ai/retriever/retriever.go` | Milvus Retriever 构造，负责 collection load、输出字段选择和检索配置。 |
| `internal/logic/ai/tokenizer/tokenizer.go` | Token 估算与上下文裁剪辅助逻辑。 |

## Session 与记忆

| 文件 | 作用 |
| --- | --- |
| `internal/logic/session/types.go` | 会话、消息、checkpoint 等领域类型定义。 |
| `internal/logic/session/manager.go` | 会话管理器，负责 session 生命周期和消息保存。 |
| `internal/logic/session/manager_test.go` | 会话管理行为测试。 |
| `internal/logic/session/storage.go` | 会话存储接口定义。 |
| `internal/logic/session/redis_storage.go` | Redis 会话存储实现。 |
| `internal/logic/session/checkpoint_store.go` | Eino checkpoint store 适配实现。 |
| `internal/logic/session/session_memory.go` | Chat 对话上下文构建与历史保存。 |
| `internal/logic/session/mem/mem.go` | Redis memory 工具，负责长上下文裁剪、摘要、TTL 和 token 预算。 |
| `internal/logic/session/mem/mem_test.go` | Redis memory 工具测试。 |

## 中间件、配置与脚本

| 文件 | 作用 |
| --- | --- |
| `utility/middleware/middleware.go` | HTTP 中间件，包括 CORS 和响应包装；需要保留 SSE 不被二次包装。 |
| `manifest/config/config.yaml` | GoFrame 运行配置。当前包含 server、logger、file_dir、Redis、Milvus 配置；模型密钥建议放 `.env`。 |
| `manifest/k8s/README.md` | Kubernetes 部署说明。 |
| `manifest/k8s/deploy.sh` | Kubernetes 部署脚本入口。 |
| `deploy/docker-compose.middleware.yml` | 本地 Redis、etcd、MinIO、Milvus 中间件定义。 |
| `scripts/dev.sh` | 本地开发启停脚本，负责启动中间件、后端和前端。当前需要注意前端实际在 `../Front_page`。 |

## Hack 与构建辅助

| 文件 | 作用 |
| --- | --- |
| `hack/hack.mk` | GoFrame 项目构建/生成 Make 目标。 |
| `hack/hack-cli.mk` | GoFrame CLI 相关 Make 目标。 |
| `hack/config.yaml` | GoFrame CLI 配置，如 dao 生成和 docker build 参数。 |

## 日志与运行产物

| 路径 | 作用 |
| --- | --- |
| `.run/` | 本地 dev 脚本 PID、日志、二进制输出目录。应保持忽略。 |

## 当前结构建议

1. `Back_part/` 可以作为独立 GoFrame 后端模块运行，验证命令应在 `Back_part/` 内执行。
2. `Front_page/` 当前与 `Back_part/` 同级，后端脚本如果要统一启动前端，应显式使用 `../Front_page`。
3. `api/chat/v1/chat.go` 是接口契约权威来源；`api/chat/chat.go` 是生成文件，不应手改业务逻辑。
4. `internal/controller` 保持薄转发，核心 SSE、resume、上传逻辑应继续放在 `internal/logic/chat`。
