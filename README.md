# 智能运维 OnCall Agent 项目实现细节

## 一、项目概述

基于 Go 语言的智能运维 Agent 系统，核心能力是通过 RAG（检索增强生成）+ ReAct Agent + 多工具调用，实现对运维告警的智能分析与处理。支持流式对话、知识库管理、自动化告警诊断。

技术栈：GoFrame + Eino（字节跳动 Cloudwego AI 编排框架）+ Milvus 向量数据库 + Redis + MySQL + DeepSeek V3 大模型

---

## 二、整体架构

```
用户请求 (HTTP/SSE)
    │
    ▼
GoFrame HTTP Server (端口 6872)
    │  CORS中间件 → 统一响应中间件
    ▼
Controller 层 (internal/controller/chat/)
    │
    ▼
Logic 层 (internal/logic/)
    │
    ├──→ Chat Pipeline (RAG + ReAct Agent)
    │       │
    │       ├── Milvus 向量检索（知识库）
    │       ├── DeepSeek V3 大模型推理
    │       └── 工具调用（日志/告警/数据库/文档）
    │
    ├──→ Knowledge Index Pipeline（文档入库）
    │       │
    │       └── 文件加载 → Markdown分割 → 向量化 → Milvus存储
    │
    └──→ Plan-Execute-Replan Pipeline（复杂任务）
            │
            └── 规划Agent → 执行Agent → 重规划Agent（循环）

外部依赖：
  - Milvus (向量数据库, 端口19530)
  - Redis (会话记忆, 端口30379)
  - MySQL (结构化数据, 端口30306)
  - Volcengine Ark API (DeepSeek V3 + 豆包Embedding)
  - 腾讯云CLS (日志查询, MCP协议)
  - Prometheus (告警查询, 端口9090)
```

---

## 三、核心模块实现细节

### 3.1 Chat Pipeline（RAG 对话管线）

基于 Eino 框架的 DAG 图编排，实现 RAG + ReAct Agent 的对话流程。

#### 图结构
```
START
  ├─→ InputToRag ─→ MilvusRetriever ─┐
  │                                    ├─→ MergeInputs ─→ ChatTemplate ─→ ReactAgent ─→ END
  └─→ InputToChat ────────────────────┘
```

#### 各节点职责

| 节点 | 类型 | 功能 |
|------|------|------|
| InputToRag | Lambda | 从 UserMessage 提取 query 字符串，传给向量检索 |
| InputToChat | Lambda | 构造 map：content(用户问题)、history(历史消息)、date(当前时间) |
| MilvusRetriever | Retriever | 向量相似度检索，TopK=3，COSINE相似度，阈值0.8 |
| MergeInputs | Lambda | 合并 RAG 检索结果与对话上下文 |
| ChatTemplate | ChatTemplate | 用 FString 模板格式化系统提示词 + 历史消息 + 用户消息 |
| ReactAgent | Lambda | ReAct 推理Agent，最大25步工具调用 |

#### 关键实现

**图编译方式：**
```go
g := compose.NewGraph[*UserMessage, *schema.Message]()
// 添加节点
g.AddLambdaNode("InputToRag", inputToRagLambda)
g.AddLambdaNode("InputToChat", inputToChatLambda)
g.AddRetrieverNode("MilvusRetriever", retriever, compose.WithOutputKey("documents"))
g.AddLambdaNode("MergeInputs", mergeLambda)
g.AddChatTemplateNode("ChatTemplate", chatTemplate)
g.AddLambdaNode("ReactAgent", agentLambda)
// 添加边
g.AddEdge(compose.START, "InputToRag")
g.AddEdge(compose.START, "InputToChat")
g.AddEdge("InputToRag", "MilvusRetriever")
g.AddEdge("MilvusRetriever", "MergeInputs")
g.AddEdge("InputToChat", "MergeInputs")
// ... 直到 END
g.Compile(ctx, compose.WithGraphName("ChatAgent"))
```

**并行分支：** START 同时触发 InputToRag 和 InputToChat，MergeInputs 使用 `AllPredecessor` 触发模式等待两个分支都完成后再执行。

**ReAct Agent 配置：**
- 模型：DeepSeek V3 Quick（快速推理）
- 最大步数：25（防止无限循环）
- 注册工具：MCP日志查询、Prometheus告警、MySQL CRUD、当前时间、知识库检索

**系统提示词要点：**
- 角色定义为"对话小助手"
- 明确输出要求：纯文本，不使用 Markdown
- 注入日志 topic 信息（ap-guangzhou 区域，指定 topic ID）
- 动态注入 RAG 检索到的相关文档作为上下文
- 使用 FString 模板，变量包括 `{date}`、`{documents}`、`{content}`

---

### 3.2 Token 预算管理与会话记忆（Redis）

这是系统的核心难点之一，实现了基于 Redis + Lua 脚本的原子化滑动窗口记忆管理。

#### 预算参数
| 参数 | 值 | 说明 |
|------|-----|------|
| MaxInputTokens | 96,000 | 模型输入上限 |
| ReserveOutputTokens | 8,192 | 预留给输出的 token |
| ReserveToolsDefault | 20,000 | 工具描述预留 |
| ReserveUserTokens | 4,000 | 当前用户消息预留（固定） |
| SafetyTokens | 2,048 | 协议开销安全余量 |
| TTL | 2小时 | 滑动过期时间 |

#### 数据结构

Redis 中存储三个 key：
- `aiagent:ctx:{id}:sys` — 系统消息（带 token 计数）
- `aiagent:ctx:{id}:turns` — 对话轮次列表（JSON 数组）
- `aiagent:ctx:{id}:meta` — 元数据（系统 token 数、校准因子等）

每个轮次结构：
```go
type storedTurn struct {
    T    int               // 该轮次总 token 数
    TS   int64             // 时间戳
    Msgs []*schema.Message // 该轮次的消息列表
}
```

#### Token 估算算法

对消息构造 JSON "footprint"，按字符类型估算：
- ASCII 字符：0.30 tokens/char
- CJK 汉字：0.60 tokens/char
- 其他 Unicode：1.00 tokens/char
- 每条消息固定开销：8 tokens
- 最终结果：`ceil(overhead + estimated_text_tokens)`

#### 写入路径（SetMessages）

1. 用户消息 token：先估算，再用 API 返回的 `promptTokens` 校准
2. 校准因子：`promptTokens / estimatePromptMsgs()`，限制在 0.6~1.6 范围
3. 助手消息 token：优先使用 `completionTokens`，回退到估算
4. 通过 Lua 脚本原子性追加到 turns 列表

#### 读取路径（GetMessagesForRequest）

动态预算计算：
```
turnsBudget = MaxInputTokens - ReserveOutputTokens - reserveToolsTokens
              - userTokensReserve - SafetyTokens - sysTokens
```

Lua 脚本从最旧的轮次开始裁剪，直到总 token 数在预算内。每次读取刷新 TTL（滑动过期）。

#### 为什么用 Lua 脚本

- 保证裁剪操作的原子性，避免并发读写导致的数据不一致
- 减少 Redis 往返次数，一次 EVAL 完成读取+裁剪+更新元数据

---

### 3.3 知识库索引管线（Knowledge Index Pipeline）

用于将上传的文档向量化并存入 Milvus，供 RAG 检索使用。

#### 图结构
```
START → FileLoader → MarkdownSplitter → MilvusIndexer → END
```

#### 各节点实现

**FileLoader：** 自定义文件加载器，从磁盘读取文档源。

**MarkdownSplitter：** 基于 Markdown 标题的文档分割
- 按一级标题 `#` 分割，标题内容作为 `title` 元数据
- 每个分块生成唯一 UUID 作为 ID
- `TrimHeaders: false`，保留标题文本

**MilvusIndexer：** 向量化并存入 Milvus
- 使用豆包 Embedding 模型生成 2048 维向量
- 存入 `biz` 集合，字段：id、vector、content、metadata

#### 文件上传流程
1. 保存上传文件到 `./docs/` 目录
2. 查询 Milvus 中同 `_source` 的旧记录并删除（避免重复）
3. 调用知识库索引管线处理新文件
4. 返回文件信息（名称、路径、大小）

---

### 3.4 Plan-Execute-Replan 管线（复杂任务处理）

三阶段 Agent 架构，用于处理需要多步推理的复杂运维任务（如 AIOps 告警分析）。

#### 三个 Agent 组件

| Agent | 模型 | 职责 |
|-------|------|------|
| Planner | DeepSeek V3.1 Think（推理模型） | 制定执行计划 |
| Executor | DeepSeek V3 Quick（快速模型） | 执行计划中的每一步，可调用工具 |
| Replanner | DeepSeek V3.1 Think | 根据执行结果调整计划 |

#### 执行流程
```
用户问题 → Planner(制定计划) → Executor(执行) → Replanner(评估/调整)
                                    ↑                    │
                                    └────────────────────┘
                                    （最多循环20次）
```

**Executor 可用工具：** MCP日志查询、Prometheus告警、知识库检索、当前时间

**AIOps 预定义任务：**
1. 从 Prometheus 获取所有活跃告警
2. 对每个告警名称查询内部知识库
3. 汇总分析结果
4. 生成告警分析报告（活跃告警列表、根因分析、处理步骤、结论）

---

### 3.5 SSE 流式传输实现

#### 服务端（Go）

```go
type Client struct {
    Id          string
    Request     *ghttp.Request
    messageChan chan string // 缓冲区大小100
}
```

**HTTP 头设置：**
- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `Connection: keep-alive`
- `Access-Control-Allow-Origin: *`

**消息格式：**
```
id: {unix_nano_timestamp}
event: {eventType}
data: {data}

```

**事件类型：** `connected`（连接确认）、`message`（内容块）、`error`（错误）、`done`（完成）

#### 流式对话完整流程

1. 创建 SSE 客户端连接
2. 从 Redis 加载历史消息（带 token 预算裁剪）
3. 构建 Chat Pipeline 并以流式模式调用
4. 逐块聚合响应：
   - 文本内容：拼接
   - 工具调用：合并（简单覆盖策略）
   - Usage：从最后一个 chunk 获取 `promptTokens` 和 `completionTokens`
5. 流结束（EOF）后，将用户消息 + 完整助手响应 + 工具调用 + 实际 token 用量写回 Redis

#### 客户端（JavaScript）

- 使用 `fetch()` + `response.body.getReader()` 读取 SSE 流
- `TextDecoder` 解码，缓冲不完整行
- 实时更新 DOM 中的助手消息内容
- 流结束后用 `marked.js` 渲染 Markdown + `highlight.js` 代码高亮

---

## 四、工具实现细节

### 4.1 MCP 日志查询工具（query_log.go）

通过 MCP（Model Context Protocol）协议连接腾讯云 CLS 日志服务。

- 传输方式：SSE（Server-Sent Events）
- 协议版本：MCP LATEST_PROTOCOL_VERSION
- 连接流程：创建 SSE 客户端 → 初始化 MCP 握手 → 获取工具列表
- 工具列表由 MCP 服务端动态提供，Agent 直接使用

### 4.2 Prometheus 告警查询工具（query_metrics_alerts.go）

- 端点：`http://192.168.149.128:9090/api/v1/alerts`
- HTTP 客户端超时：10秒
- 去重逻辑：按 alertname 去重，保留首次出现的告警
- 输出字段：告警名称、描述、状态（firing/pending）、激活时间、持续时长

### 4.3 MySQL CRUD 工具（mysql_crud.go）

**安全防护措施：**
- 禁止多语句 SQL（检测分号）
- 关键字黑名单：DROP、TRUNCATE、ALTER、GRANT、REVOKE、CREATE、RENAME、SHUTDOWN、LOAD_FILE、INTO OUTFILE/DUMPFILE
- query 模式仅允许 SELECT
- exec 模式仅允许 INSERT/UPDATE/DELETE
- UPDATE/DELETE 必须包含 WHERE 子句
- 查询超时：8秒
- 最大返回行数：200
- 支持只读模式和审批机制

### 4.4 知识库检索工具（query_internal_docs.go）

- 复用 Milvus Retriever 进行语义检索
- 返回 JSON 格式的检索结果

### 4.5 当前时间工具（get_current_time.go）

- 返回：秒级/毫秒级/微秒级 Unix 时间戳 + 可读格式

---

## 五、向量数据库（Milvus）实现

### 5.1 集合 Schema

集合名：`biz`，数据库名：`agent`

| 字段 | 类型 | 说明 |
|------|------|------|
| id | VarChar(256) | 主键 |
| vector | FloatVector(2048) | 向量字段 |
| content | VarChar(8192) | 文档内容 |
| metadata | JSON | 动态字段，启用 dynamic fields |

### 5.2 索引配置

所有字段使用 AUTOINDEX，向量字段使用 COSINE 度量。

### 5.3 检索参数

- TopK：3
- 相似度阈值：0.8
- 搜索参数：AUTOINDEX，nprobe=1

### 5.4 Embedding 模型

- 模型：豆包多模态 Embedding（doubao-embedding-vision-250615）
- 维度：2048
- 提供方：火山引擎 Ark API
- 重试策略：2次重试，指数退避（200ms × (attempt+1)）
- 类型转换：API 返回 float32 → 转换为 float64

### 5.5 初始化流程

1. 连接默认数据库
2. 检查/创建 `agent` 数据库
3. 检查/创建 `biz` 集合（含完整 schema）
4. 为所有字段创建 AUTOINDEX 索引

---

## 六、基础设施与中间件

### 6.1 MySQL 连接池配置

| 参数 | 默认值 |
|------|--------|
| 最大连接数 | 50 |
| 最大空闲连接 | 10 |
| 连接最大生命周期 | 30分钟 |
| 空闲连接超时 | 5分钟 |
| Ping 超时 | 3秒 |
| 慢查询阈值 | 500ms |
| 预编译语句 | 启用 |

配置来源优先级：环境变量 `MYSQL_DSN` > 配置文件

### 6.2 中间件

**CORS 中间件：** 使用 GoFrame 默认 CORS 配置，允许所有来源。

**统一响应中间件：** 所有响应包装为：
```json
{
  "message": "OK 或错误信息",
  "data": "处理结果"
}
```

### 6.3 Docker Compose 基础设施

| 服务 | 版本 | 端口 | 用途 |
|------|------|------|------|
| Milvus Standalone | v2.5.10 | 19530 | 向量数据库 |
| etcd | v3.5.18 | 2379 | Milvus 配置存储 |
| MinIO | 2023-03-20 | 9000/9001 | Milvus 对象存储 |
| Attu | v2.6 | 8000 | Milvus 管理界面 |
| Prometheus | latest | 9090 | 监控告警 |

---

## 七、API 接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/api/v1/chat` | POST | 单轮对话（非流式） |
| `/api/v1/chat_stream` | POST | 流式对话（SSE） |
| `/api/v1/upload` | POST | 文件上传到知识库 |
| `/api/v1/ai_ops` | POST | AI 运维分析（Plan-Execute-Replan） |

请求体统一包含 `Id`（会话ID）和 `Question`（用户问题）。

---

## 八、前端实现

- 纯 JavaScript（ES6+），无框架
- 使用 `fetch` + `ReadableStream` 处理 SSE 流式响应
- `localStorage` 持久化聊天历史（最多50条对话）
- 支持"快速"和"流式"两种对话模式切换
- 文件上传支持 `.txt`、`.md`、`.markdown`，最大 50MB
- 流式显示时有闪烁光标动画（`▋`），结束后渲染 Markdown
- AIOps 结果支持可折叠的详情展示

---

## 九、面试高频问题准备

### Q1: 为什么选择 Eino 框架而不是 LangChain？
Eino 是字节跳动 Cloudwego 团队开源的 Go 原生 AI 编排框架，相比 LangChain（Python）：
- Go 原生，与项目技术栈一致，无需跨语言调用
- 基于 DAG 图编排，支持并行分支（如 RAG 检索和对话上下文准备并行）
- 内置 ReAct Agent、Plan-Execute 等模式
- 与 Milvus、Ark API 等有现成的扩展组件

### Q2: Token 预算管理为什么用 Lua 脚本？
- 原子性：裁剪历史消息时需要读取总 token → 判断是否超预算 → 删除最旧轮次 → 更新元数据，这些操作必须原子执行
- 性能：一次 EVAL 调用完成所有操作，避免多次 Redis 往返
- 并发安全：多个请求同时操作同一会话时不会出现数据不一致

### Q3: RAG 检索的效果如何保证？
- 使用 COSINE 相似度 + 0.8 阈值过滤低质量结果
- TopK=3 控制注入上下文的文档数量，避免 token 浪费
- 文档按 Markdown 标题分割，保证语义完整性
- 上传新文件时先删除旧版本记录，避免重复

### Q4: 为什么用两个不同的 DeepSeek 模型？
- DeepSeek V3.1 Think（推理模型）：用于 Planner 和 Replanner，需要深度思考和规划能力
- DeepSeek V3 Quick（快速模型）：用于 Executor 和 ReAct Agent，需要快速响应和工具调用
- 这样在推理质量和响应速度之间取得平衡

### Q5: MySQL CRUD 工具的安全设计思路？
多层防护：关键字黑名单 → 操作类型限制 → WHERE 子句强制 → 查询超时 → 行数限制 → 只读模式 → 审批机制。防止 Agent 在自主调用时执行危险 SQL。

### Q6: SSE 流式传输相比 WebSocket 的优势？
- 单向推送场景（服务端→客户端）更简单
- 基于 HTTP，无需额外协议升级
- 自动重连机制
- 与 GoFrame 的 HTTP 服务天然兼容
- 对于 LLM 逐 token 输出的场景完全够用
