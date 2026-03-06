# Phase 1 实施进展

## 已完成的工作

### 1. 上下文管理模块 (`internal/context/`)

#### 核心文件：
- **`types.go`**: 定义了所有上下文相关的数据结构
  - `SessionContext`: 会话上下文（对话历史、用户意图、元数据）
  - `AgentContext`: Agent 上下文（状态、工具调用历史、中间结果）
  - `ExecutionContext`: 执行上下文（任务队列、执行日志、回滚栈）
  - `GlobalContext`: 全局上下文（管理所有会话、Agent、执行）

- **`manager.go`**: 上下文管理器实现
  - `CreateSession()`: 创建新会话
  - `GetSession()`: 获取会话（支持 L1/L2 自动恢复）
  - `AddMessage()`: 添加消息到对话历史
  - `CreateAgentContext()`: 创建 Agent 上下文
  - `CreateExecutionContext()`: 创建执行上下文
  - `MigrateToL2()`: 数据迁移（L1 → L2）

- **`storage.go`**: 存储层抽象接口
  - 定义了 `Storage` 接口，支持多种存储实现

- **`redis_storage.go`**: Redis 存储实现
  - 实现了 `Storage` 接口
  - 支持会话、Agent、执行上下文的持久化

- **`manager_test.go`**: 单元测试
  - 测试会话创建、获取、消息添加
  - 测试 Agent 上下文、执行上下文
  - 测试数据迁移

### 2. Supervisor Agent (`internal/agent/supervisor/`)

#### 核心文件：
- **`supervisor.go`**: Supervisor Agent 主逻辑
  - `HandleRequest()`: 处理用户请求的主入口
  - `handleMonitorRequest()`: 处理监控查询
  - `handleDiagnoseRequest()`: 处理故障诊断（串行协作）
  - `handleExecuteRequest()`: 处理执行操作
  - `handleKnowledgeRequest()`: 处理知识检索
  - 工具注册与管理

- **`router.go`**: Agent 路由器
  - `ClassifyIntent()`: 意图分类（基于关键词匹配）
  - `RouteToAgent()`: 根据意图路由到对应的 Agent
  - 支持 4 种意图类型：monitor、diagnose、execute、knowledge

- **`aggregator.go`**: 结果聚合器
  - `Aggregate()`: 聚合多个 Agent 的结果
  - `AggregateWithPriority()`: 按优先级聚合结果

### 3. 应用初始化 (`internal/bootstrap/`)

#### 核心文件：
- **`app.go`**: 应用启动逻辑
  - `NewApplication()`: 初始化所有组件
  - 初始化日志（zap）
  - 初始化 Redis 客户端
  - 初始化上下文管理器
  - 初始化 Supervisor Agent
  - 启动后台任务（数据迁移）

### 4. 主程序更新 (`main.go`)

- 集成新的 Agent 架构
- 保持与现有 HTTP 服务的兼容性

## 架构特点

### 1. 分层上下文管理
```
GlobalContext（全局）
├── SessionContext（会话级）
├── AgentContext（Agent 级）
└── ExecutionContext（执行级）
```

### 2. 并发安全
- 使用 `sync.Map` 管理全局状态
- 使用 `sync.RWMutex` 保护单个上下文
- 支持多会话并发

### 3. 冷热分离存储
- **L1（热数据）**: Go sync.Map（内存）
- **L2（温数据）**: Redis（持久化）
- **L3（向量数据）**: Milvus（待实现）
- **L4（冷数据）**: PostgreSQL（待实现）

### 4. 意图驱动路由
- 自动识别用户意图（monitor/diagnose/execute/knowledge）
- 根据意图路由到对应的 Agent
- 支持置信度评估

## 测试方法

### 1. 运行单元测试
```bash
cd /home/lihaoqian/project/oncall
go test ./internal/context/... -v
```

### 2. 启动服务
```bash
go run main.go
```

### 3. 测试 API（使用现有接口）
```bash
curl -X POST http://localhost:6872/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "查看 pod 状态"}'
```

## 下一步计划（Phase 2）

### 1. Knowledge Agent
- 复用现有的 `chat_pipeline` 和 `knowledge_index_pipeline`
- 实现向量检索工具
- 实现案例排序和反馈机制

### 2. Dialogue Agent
- 实现意图预测模块
- 实现语义熵计算
- 实现候选问题生成

### 3. Ops Agent
- 实现 K8s 监控工具
- 实现 Prometheus 查询工具
- 实现动态阈值检测

### 4. 工具集成
- 将 Agent 封装为 Tool
- 在 Supervisor 中注册工具
- 实现串行协作流程

## 技术债务

1. **意图分类**: 当前使用简单的关键词匹配，后续需要用 LLM 增强
2. **错误处理**: 需要完善错误处理和重试机制
3. **监控指标**: 需要添加 Prometheus 指标（Agent 调用次数、延迟等）
4. **日志**: 需要完善结构化日志
5. **配置管理**: 需要将硬编码的配置移到配置文件

## 依赖项

已有依赖（无需额外安装）：
- `github.com/cloudwego/eino` - Eino ADK
- `github.com/redis/go-redis/v9` - Redis 客户端
- `github.com/google/uuid` - UUID 生成
- `go.uber.org/zap` - 日志库
- `github.com/gogf/gf/v2` - GoFrame 框架

## 文件清单

```
internal/
├── context/
│   ├── types.go              # 上下文数据结构
│   ├── manager.go            # 上下文管理器
│   ├── storage.go            # 存储接口
│   ├── redis_storage.go      # Redis 存储实现
│   └── manager_test.go       # 单元测试
├── agent/
│   └── supervisor/
│       ├── supervisor.go     # Supervisor Agent
│       ├── router.go         # 路由器
│       └── aggregator.go     # 结果聚合器
└── bootstrap/
    └── app.go                # 应用初始化
```

## 总结

Phase 1 已完成基础框架的搭建，包括：
- ✅ 上下文管理模块（支持分层存储、并发安全、数据迁移）
- ✅ Supervisor Agent（支持意图分类、路由、结果聚合）
- ✅ 应用初始化（集成所有组件）
- ✅ 单元测试（验证核心功能）

系统已具备基本的会话管理和意图路由能力，为 Phase 2 的核心 Agent 开发奠定了坚实基础。
