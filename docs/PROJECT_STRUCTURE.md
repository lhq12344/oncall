# OnCall 项目结构解析文档

## 项目概述

OnCall 是一个基于 Go + AI 的智能值班告警处理系统，使用 GoFrame 框架 + Cloudwego Eino (AI 编排框架) 构建。系统通过多 Agent 协作实现智能告警处理、根因分析、知识检索和自动化修复。

**技术栈:**
- **后端框架**: GoFrame + Cloudwego Eino
- **LLM**: DeepSeek V3 (Volcengine Ark API) + Claude
- **向量数据库**: Milvus
- **缓存**: Redis (会话记忆 + 多级缓存)
- **数据库**: MySQL + GORM
- **日志**: Elasticsearch + 腾讯云 CLS (MCP 协议)
- **监控**: Prometheus
- **前端**: Vanilla JS

**服务端口**: 6872

---

## 目录结构总览

```
oncall/
├── main.go                    # 项目入口
├── go.mod                     # Go 模块定义
├── CLAUDE.md                  # 项目说明
├── manifest/                  # 基础设施配置
│   ├── config/config.yaml     # 主配置文件
│   └── k8s/                  # K8s 部署配置
├── api/                       # API 定义
│   └── chat/
│       ├── chat.go            # 公共定义
│       └── v1/chat.go        # V1 API 结构
├── internal/                  # 内部模块
│   ├── bootstrap/             # 应用启动初始化
│   ├── controller/chat/       # HTTP 控制器
│   ├── logic/                 # 业务逻辑层
│   ├── agent/                 # Agent 核心系统
│   ├── ai/                    # AI 基础设施
│   ├── cache/                 # 缓存模块
│   ├── concurrent/            # 并发控制
│   └── consts/                # 常量定义
├── utility/                   # 工具库
│   ├── mem/                   # Redis 会话记忆
│   ├── mysql/                 # MySQL 连接池
│   ├── middleware/            # HTTP 中间件
│   ├── tokenizer/             # Token 计数器
│   ├── elasticsearch/         # ES 客户端
│   └── common/                # 公共定义
├── Front_page/                # 前端页面
└── docs/                      # 项目文档
```

---

## 核心模块详解

### 1. main.go - 项目入口

**作用**: GoFrame HTTP 服务器启动入口

**核心功能**:
- 初始化配置 (Redis, MySQL, Milvus, Elasticsearch)
- 初始化统一工作流 Agent（RCA → Ops → Execution → Strategy）及相关模块
- 注册 HTTP 路由 `/api/v1/*`
- 启动 SSE 服务

**端口**: 6872

**关键路由**:
| 路由 | 方法 | 功能 |
|------|------|------|
| `/api/v1/chat` | POST | 单轮对话 |
| `/api/v1/chat_stream` | POST | 流式对话 (SSE) |
| `/api/v1/chat_resume` | POST | 恢复中断对话（checkpoint + resume） |
| `/api/v1/chat_resume_stream` | POST | 流式恢复中断对话（SSE） |
| `/api/v1/upload` | POST | 文件上传到知识库 |
| `/api/v1/ai_ops` | POST | AI 运维操作 |
| `/api/v1/monitoring` | GET | 监控统计 |

---

### 2. manifest/config/config.yaml - 配置文件

**作用**: 运行时配置中心

**配置项**:
```yaml
llm:
  - name: "claude"
    api_key: "xxx"
    endpoint: "https://api.anthropic.com"
  - name: "doubao"
    api_key: "xxx"

redis:
  host: "localhost"
  port: 30379

mysql:
  host: "localhost"
  port: 30306
  database: "oncall"

milvus:
  host: "milvus.infra.svc"
  port: 19530

prometheus:
  url: "http://127.0.0.1:30090"

elasticsearch:
  addresses: ["http://localhost:9200"]

cache:
  enabled: true
  ttl: 300  # 秒

concurrent:
  max_parallel_agents: 5
  max_parallel_tools: 10

circuit_breaker:
  enabled: true
  failure_threshold: 5
```

---

### 3. api/ - API 定义

#### `api/chat/chat.go` - 公共定义
- 错误码常量
- 通用响应结构

#### `api/chat/v1/chat.go` - V1 API 结构
| 结构体 | 用途 |
|--------|------|
| `ChatReq` | 对话请求 (question, id, stream) |
| `ChatRes` | 对话响应（含 interrupted/checkpoint_id） |
| `ChatResumeReq` | 恢复请求（checkpoint_id + decision） |
| `ChatResumeRes` | 恢复响应（含新的中断上下文） |
| `UploadReq` | 文件上传请求 |
| `AIOpsReq` | AI 运维请求 |

---

### 4. internal/bootstrap/app.go - 应用初始化

**作用**: 核心初始化逻辑

**核心函数**:
| 函数 | 功能 |
|------|------|
| `NewApplication()` | 创建应用实例 |
| `InitRedis()` | 初始化 Redis 客户端 |
| `InitMySQL()` | 初始化 MySQL 连接池 |
| `InitElasticsearch()` | 初始化 ES 客户端 |
| `InitChatModel()` | 初始化 LLM 客户端 |
| `InitAgents()` | 初始化所有 Agent |

---

### 5. internal/controller/chat/ - HTTP 控制器

#### `chat_v1.go` - 核心控制器

**结构体** `ControllerV1`:
```go
type ControllerV1 struct {
    chatAgent        adk.ResumableAgent   // 会话根 Agent
    chatRunner       *adk.Runner          // checkpoint runner
    chatStreamRunner *adk.Runner          // streaming checkpoint runner
    cacheManager    *cache.Manager        // 缓存管理
    llmCache        *cache.LLMCache       // LLM 响应缓存
    cbManager       *concurrent.CircuitBreakerManager  // 熔断器
    opsExecutor     *ops.IntegratedOpsExecutor
}
```

**核心方法**:
| 方法 | 流程 |
|------|------|
| `Chat()` | 参数验证 → 获取历史 → LLM缓存查询 → Runner执行(with checkpoint) → 中断/结果返回 |
| `ChatStream()` | SSE设置 → Runner流式执行(with checkpoint) → 中断SSE事件/文本输出 |
| `ChatResume()` | 读取checkpoint与interrupt ids → ResumeWithParams 原地恢复 |
| `ChatResumeStream()` | SSE恢复执行，支持再次中断并返回新的interrupt上下文 |
| `FileUpload()` | 获取文件 → 验证类型 → 分片处理 → Milvus索引 → 返回 |
| `AIOps()` | Plan-Execute-Replan 模式执行运维任务 |
| `Monitoring()` | 返回熔断器统计和缓存命中率 |

---

### 6. internal/logic/ - 业务逻辑层

#### `sse/sse.go` - SSE 流式服务

**核心结构**:
```go
type Client struct {
    Id          string
    Request     *ghttp.Request
    messageChan chan string
}

type Service struct {
    clients *gmap.StrAnyMap
}
```

**核心函数**:
| 函数 | 功能 |
|------|------|
| `New()` | 创建 SSE 服务 |
| `Create()` | 建立 SSE 连接，设置 event-stream 响应头 |
| `SendToClient()` | 向客户端发送消息 |

**SSE 消息格式**:
```
id: {timestamp}
event: {eventType}
data: {data}

```

---

### 7. internal/agent/ - Agent 核心系统 (重点)

这是项目的核心模块，包含多类专业 Agent，由统一工作流进行编排。

#### 7.1 Incident Workflow Agent - 统一工作流 Agent
**文件**: `agent/ops/incident_workflow.go`

**职责**: 以顺序+循环方式编排 RCA、Ops、Execution、Strategy 完成故障处置

**调用流程**:
```
用户请求 → RCA → Ops(规划) → Validator(校验) → Execution(执行)
                                       ↘ 失败/未解决 ↗
                               最终进入 Strategy 复盘
```

---

#### 7.2 Dialogue Agent - 对话 Agent
**文件**: `agent/dialogue/agent.go`

**职责**: 用户意图分析和对话引导

**工具**:
| 工具 | 文件 | 功能 |
|------|------|------|
| `intent_analysis` | tools/IntentAnalysisTool.go | 分析意图类型 (monitor/diagnose/knowledge/execute/general) |
| `question_prediction` | tools/QuestionPredictionTool.go | 预测用户下一步问题 |

**意图分类**:
- `monitor` - 监控查询
- `diagnose` - 故障诊断
- `knowledge` - 知识检索
- `execute` - 执行操作
- `general` - 通用对话

---

#### 7.3 Knowledge Agent - 知识库 Agent
**文件**: `agent/knowledge/agent.go`

**职责**: RAG 知识检索和文档索引

**工具**:
| 工具 | 文件 | 功能 |
|------|------|------|
| `vector_search` | tools/VectorSearchTool.go | Milvus 向量检索 |
| `knowledge_index` | tools/KnowledgeIndexTool.go | 文档索引到知识库 |

**特性**:
- 自动文档分片 (默认 1000 字符/片)
- Markdown 标题感知切分
- 相似度排序返回

---

#### 7.4 Ops Agent - 运维 Agent
**文件**: `agent/ops/agent.go`

**职责**: 监控系统状态、采集指标、分析日志

**工具**:
| 工具 | 文件 | 功能 |
|------|------|------|
| `k8s_monitor` | tools/k8s_monitor.go | K8s 资源监控 (Pod/Node/Deployment) |
| `metrics_collector` | tools/metrics_collector.go | Prometheus 指标采集 |
| `es_log_query` | tools/es_log_query.go | Elasticsearch 日志查询 |
| `log_analyzer` | tools/log_analyzer.go | 日志模式分析 |

**架构**: Plan-Execute-Replan 模式

---

#### 7.5 Execution Agent - 执行 Agent
**文件**: `agent/execution/agent.go`

**职责**: 安全执行运维命令

**工具**:
| 工具 | 文件 | 功能 |
|------|------|------|
| `generate_plan` | tools/generate_plan.go | 生成可执行步骤序列 |
| `execute_step` | tools/execute_step.go | 沙盒环境执行 |
| `validate_result` | tools/validate_result.go | 验证执行结果 |
| `rollback` | tools/rollback.go | 回滚操作 |

**安全机制**:
- 命令白名单 (kubectl, systemctl, docker, curl)
- 危险模式检测
- 风险等级评估 (low/medium/high)

---

#### 7.6 RCA Agent - 根因分析 Agent
**文件**: `agent/rca/agent.go`

**职责**: 故障根因分析和影响范围评估

**工具**:
| 工具 | 文件 | 功能 |
|------|------|------|
| `build_dependency_graph` | tools/build_dependency_graph.go | 构建服务依赖图 |
| `correlate_signals` | tools/correlate_signals.go | 关联告警/日志/指标 |
| `infer_root_cause` | tools/infer_root_cause.go | 推理根本原因 |
| `analyze_impact` | tools/analyze_impact.go | 分析影响范围 |

---

#### 7.7 Strategy Agent - 策略 Agent
**文件**: `agent/strategy/agent.go`

**职责**: 评估和优化执行策略

**工具**:
| 工具 | 文件 | 功能 |
|------|------|------|
| `evaluate_strategy` | tools/evaluate_strategy.go | 评估策略质量 |
| `optimize_strategy` | tools/optimize_strategy.go | 优化执行策略 |
| `update_knowledge` | tools/update_knowledge.go | 更新知识库 |
| `prune_knowledge` | tools/prune_knowledge.go | 清理低质量案例 |

---

### 8. internal/ai/ - AI 基础设施

#### 8.1 models/ - LLM 模型客户端
**文件**: `ai/models/chat_model.go`

**功能**: DeepSeek V3 和 Claude API 客户端初始化

#### 8.2 embedder/ - 向量嵌入模型
**文件**: `ai/embedder/doubao_embedder.go`

**功能**: Doubao embedding 模型初始化

#### 8.3 retriever/ - 向量检索
**文件**: `ai/retriever/milvus_retriever.go`

**功能**: Milvus 向量数据库检索

#### 8.4 indexer/ - 向量索引
**文件**: `ai/indexer/milvus_indexer.go`

**功能**: Milvus 向量数据库索引存储

#### 8.5 loader/ - 文档加载
**文件**: `ai/loader/file_loader.go`

**功能**: 从文件加载文档内容

#### 8.6 tools/ - Agent 工具集
**文件**: `ai/tools/`

| 文件 | 工具名 | 功能 |
|------|--------|------|
| `web_search.go` | `web_search` | 联网搜索 (Bing API) |
| `query_internal_docs.go` | `query_internal_docs` | RAG 知识库检索 |
| `query_log.go` | `query_log` | 腾讯云 CLS 日志查询 (MCP) |
| `get_current_time.go` | `get_current_time` | 获取当前时间 |
| `mysql_crud.go` | `mysql_crud` | MySQL CRUD 操作 |
| `query_metrics_alerts.go` | `query_prometheus_alerts` | Prometheus 告警查询 |

---

### 9. internal/cache/ - 缓存模块

| 文件 | 功能 |
|------|------|
| `cache.go` | 缓存管理器基础框架 |
| `llm_cache.go` | LLM 响应缓存 |
| `vector_cache.go` | Milvus 向量检索结果缓存 |
| `monitoring_cache.go` | 监控数据缓存 (K8s/Prometheus/ES) |
| `eviction.go` | 缓存失效策略 (TTL, LRU) |

**缓存键格式**: `prefix:type:agentID:sessionID:key`

**默认 TTL**:
- LLM 缓存: 30 分钟
- 向量缓存: 1 小时
- 监控缓存: 5 分钟
- 通用缓存: 15 分钟

---

### 10. internal/concurrent/ - 并发控制

| 文件 | 功能 |
|------|------|
| `executor.go` | 通用并发执行器 (信号量控制) |
| `circuit_breaker.go` | 熔断器 (三态: Closed/Open/HalfOpen) |
| `agent_executor.go` | Agent 并行执行器 |
| `tool_executor.go` | 工具并行执行器 |

**熔断器配置**:
- 失败次数阈值: 5
- 失败率阈值: 0.5
- 打开持续时间: 60s
- 半开最大请求: 1

---

### 11. utility/ - 工具库

| 目录/文件 | 功能 |
|-----------|------|
| `mem/mem.go` | Redis 会话记忆，Token 预算控制 (96k 输入/8k 输出) |
| `mysql/mysql.go` | MySQL 连接池管理 |
| `middleware/middleware.go` | CORS 和响应封装中间件 |
| `tokenizer/tokenizer.go` | 精确 Token 计数 (调用 LLM API) |
| `elasticsearch/elasticsearch.go` | ES 客户端初始化 |
| `client/client.go` | Milvus 客户端初始化 |
| `common/common.go` | 公共常量 |

---

## 推荐的阅读顺序

### 路线一: 快速理解架构 (30 分钟)

1. **main.go** - 了解项目入口和启动流程
2. **manifest/config/config.yaml** - 了解配置结构
3. **internal/controller/chat/chat_v1.go** - 了解请求入口
4. **internal/agent/ops/incident_workflow.go** - 了解统一工作流编排
5. **api/chat/v1/chat.go** - 了解 API 数据结构

### 路线二: 深入 Agent 系统 (2 小时)

1. **main.go** - 项目入口
2. **internal/bootstrap/app.go** - 初始化流程
3. **internal/agent/ops/incident_workflow.go** - 统一工作流
4. **internal/agent/dialogue/** - 对话 Agent
5. **internal/agent/knowledge/** - 知识库 Agent
6. **internal/agent/ops/** - 运维 Agent
7. **internal/agent/execution/** - 执行 Agent
8. **internal/agent/rca/** - 根因分析
9. **internal/ai/tools/** - 所有工具实现

### 路线三: 深入基础设施 (1.5 小时)

1. **utility/mem/mem.go** - Redis 会话管理
2. **utility/mysql/mysql.go** - MySQL 连接池
3. **internal/cache/** - 缓存机制
4. **internal/concurrent/** - 并发控制和熔断
5. **utility/elasticsearch/** - ES 客户端
6. **utility/client/client.go** - Milvus 客户端

### 路线四: 理解请求处理流程

1. HTTP 请求到达 `main.go`
2. 路由到 `controller/chat/chat_v1.go`
3. 业务逻辑在 `logic/sse/sse.go`
4. Agent 执行在 `agent/ops/incident_workflow.go`
5. 工具执行在 `ai/tools/`
6. 结果返回并缓存

---

## 关键文件索引

| 分类 | 文件路径 |
|------|----------|
| 入口 | `main.go` |
| 配置 | `manifest/config/config.yaml` |
| API | `api/chat/v1/chat.go` |
| 控制器 | `internal/controller/chat/chat_v1.go` |
| 逻辑 | `internal/logic/sse/sse.go` |
| 工作流 Agent | `internal/agent/ops/incident_workflow.go` |
| 知识库 | `internal/agent/knowledge/` |
| 运维 | `internal/agent/ops/` |
| 执行 | `internal/agent/execution/` |
| 工具 | `internal/ai/tools/*.go` |
| 缓存 | `internal/cache/cache.go` |
| 熔断 | `internal/concurrent/circuit_breaker.go` |
| 会话 | `utility/mem/mem.go` |

---

## Agent 协作关系图

```
           +--------------------------------------+
           |     Incident Workflow (Resumable)    |
           +----------------+---------------------+
                            |
                    +-------v-------+
                    |  RCA Agent    |
                    +-------+-------+
                            |
                    +-------v-------+
                    |  Ops Agent    |
                    +-------+-------+
                            |
                    +-------v-------+
                    | Execution     |
                    | Agent         |
                    +-------+-------+
                            |
                    +-------v-------+
                    | Strategy      |
                    | Agent         |
                    +---------------+
```

---

## 附录: 项目命令

```bash
# 运行服务 (端口 6872)
go run main.go

# 构建二进制
make build

# 代码生成
make ctrl      # 从 api/ 生成控制器
make dao       # 从数据库生成 DAO
make service   # 生成服务接口

# 启动基础设施
cd manifest/docker && docker-compose up -d

# 启动前端
cd Front_page && ./start.sh
```
