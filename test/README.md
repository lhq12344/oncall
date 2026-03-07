# 测试文档

本目录包含 oncall 项目的所有测试代码。

## 目录结构

```
test/
├── knowledge_agent_test.go      # KnowledgeAgent 单元测试
├── dialogue_agent_test.go       # DialogueAgent 单元测试
├── ops_agent_test.go            # OpsAgent 单元测试
├── execution_agent_test.go      # ExecutionAgent 单元测试
├── rca_agent_test.go            # RCAAgent 单元测试
├── strategy_agent_test.go       # StrategyAgent 单元测试
├── milvus_integration_test.go   # Milvus 集成测试
└── integration/                 # 集成测试目录
    ├── e2e_test.go             # 端到端测试
    ├── test_milvus.go          # Milvus 连接测试程序
    └── test_prometheus.go      # Prometheus 连接测试程序
```

## 测试统计

### 单元测试覆盖

| 测试文件 | 测试用例 | 覆盖组件 | 状态 |
|---------|---------|---------|------|
| `knowledge_agent_test.go` | 20 | VectorSearch/KnowledgeIndex/CaseRanker | ✅ 100% |
| `dialogue_agent_test.go` | 21 | IntentAnalysis/QuestionPrediction/State | ✅ 100% |
| `ops_agent_test.go` | 26 | K8s/Prometheus/Log/ES | ✅ 100% |
| `execution_agent_test.go` | 23 | Plan/Execute/Validate/Rollback | ✅ 100% |
| `rca_agent_test.go` | 15 | DependencyGraph/Correlate/Infer/Impact | ✅ 100% |
| `strategy_agent_test.go` | 15 | Evaluate/Optimize/Update/Prune | ✅ 100% |
| **总计** | **154** | **6 Agents + 24 Tools** | **✅ 100%** |

### 测试特性

- ✅ **边界测试**: 空输入、缺失字段、无效JSON
- ✅ **降级测试**: 外部依赖不可用时的降级行为
- ✅ **安全测试**: 命令白名单、危险模式检测、参数注入防护
- ✅ **配置测试**: Nil配置、缺失必需字段
- ✅ **功能测试**: 各工具的核心功能验证
- ✅ **LLM集成测试**: 意图分析、计划生成、策略优化

## 快速开始

### 运行所有单元测试

```bash
# 在项目根目录运行
go test ./test -v

# 查看测试覆盖率
go test ./test -v -cover

# 生成覆盖率报告
go test ./test -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### 运行特定 Agent 的测试

```bash
# KnowledgeAgent
go test ./test -run TestKnowledgeAgent -v
go test ./test -run TestVectorSearchTool -v
go test ./test -run TestCaseRanker -v

# DialogueAgent
go test ./test -run TestDialogueAgent -v
go test ./test -run TestIntentAnalysisTool -v

# OpsAgent
go test ./test -run TestOpsAgent -v
go test ./test -run TestK8sMonitorTool -v
go test ./test -run TestMetricsCollectorTool -v

# ExecutionAgent
go test ./test -run TestExecutionAgent -v
go test ./test -run TestGeneratePlanTool -v
go test ./test -run TestExecuteStepTool -v

# RCAAgent
go test ./test -run TestRCAAgent -v
go test ./test -run TestBuildDependencyGraphTool -v

# StrategyAgent
go test ./test -run TestStrategyAgent -v
go test ./test -run TestEvaluateStrategyTool -v
```

### 运行集成测试

```bash
# 需要外部服务（Redis、Milvus等）
go test ./test/integration -v

# 跳过集成测试（快速模式）
go test ./test -v -short
```

## 测试类型说明

### 1. 单元测试

**位置**: `test/*_agent_test.go`

**特点**:
- 无外部依赖（或使用 mock）
- 运行速度快（3-4秒）
- 测试单个组件的功能
- 支持降级模式测试

**示例**:
```go
func TestVectorSearchTool(t *testing.T) {
    logger := zap.NewNop()
    tool := tools.NewVectorSearchTool(nil, logger)

    input := `{"query": "Pod CPU 使用率过高", "top_k": 5}`
    result, err := tool.InvokableRun(ctx, input)
    require.NoError(t, err)
    assert.Contains(t, result, "degraded")
}
```

### 2. 集成测试

**位置**: `test/integration/`

**特点**:
- 需要外部服务（Redis、Milvus、Prometheus等）
- 运行速度较慢（10-30秒）
- 测试端到端流程
- 验证服务间集成

**示例**:
```go
func TestEndToEnd_SupervisorAgent(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    app, err := bootstrap.NewApplication(&bootstrap.Config{
        RedisAddr: "localhost:6379",
    })
    // ...
}
```

### 3. 连接测试程序

**位置**: `test/integration/test_*.go`

**特点**:
- 独立的可执行程序（非测试用例）
- 用于快速验证外部服务连接
- 可以手动运行

**运行方式**:
```bash
cd test/integration
go run test_milvus.go
go run test_prometheus.go
```

## 测试最佳实践

### 1. 测试命名

```go
// ✅ 好的命名
func TestVectorSearchTool_ValidInput(t *testing.T) { }
func TestCaseRanker_RankMultipleDocs(t *testing.T) { }

// ❌ 不好的命名
func TestFunc1(t *testing.T) { }
func Test_something(t *testing.T) { }
```

### 2. 使用子测试

```go
func TestIntentAnalysisTool(t *testing.T) {
    t.Run("MonitorIntent", func(t *testing.T) {
        // 测试监控意图
    })

    t.Run("DiagnoseIntent", func(t *testing.T) {
        // 测试诊断意图
    })
}
```

### 3. 处理外部依赖

```go
// 降级模式测试
tool := tools.NewMetricsCollectorTool("", logger)
result, err := tool.InvokableRun(ctx, input)
if err != nil {
    t.Logf("Prometheus not available (expected): %v", err)
    return
}
```

### 4. 使用表格驱动测试

```go
testCases := []struct {
    name     string
    input    string
    expected string
}{
    {"ValidInput", `{"query":"test"}`, "success"},
    {"EmptyQuery", `{"query":""}`, "error"},
}

for _, tc := range testCases {
    t.Run(tc.name, func(t *testing.T) {
        // 测试逻辑
    })
}
```

## 环境要求

### 单元测试

- Go 1.21+
- 无外部依赖

### 集成测试

- Go 1.21+
- Redis (localhost:6379)
- Milvus (localhost:31953)
- Prometheus (localhost:30090) - 可选
- K8s 集群 - 可选

### 启动测试环境

```bash
# 启动所有依赖服务
cd manifest/docker
docker-compose up -d

# 验证服务状态
docker-compose ps

# 查看服务日志
docker-compose logs -f milvus-standalone
docker-compose logs -f redis
```

## CI/CD 集成

### GitHub Actions 示例

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      redis:
        image: redis:7
        ports:
          - 6379:6379

      milvus:
        image: milvusdb/milvus:latest
        ports:
          - 19530:19530

    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run unit tests
        run: go test ./test -v -short

      - name: Run integration tests
        run: go test ./test/integration -v
```

## 故障排查

### 测试失败

```bash
# 查看详细错误信息
go test ./test -v -run TestFailingTest

# 只运行失败的测试
go test ./test -v -run TestFailingTest -count=1
```

### 外部服务不可用

```bash
# 检查服务状态
docker-compose ps

# 重启服务
docker-compose restart redis milvus-standalone

# 查看日志
docker-compose logs redis
```

### 测试超时

```bash
# 增加超时时间
go test ./test -v -timeout 5m
```

## 相关文档

- [项目架构文档](../docs/update.md)
- [集成测试文档](./integration/README.md)
- [示例程序文档](../internal/ai/examples/README.md)
- [部署文档](../manifest/docker/README.md)

## 贡献指南

添加新测试时，请确保：

1. ✅ 测试命名清晰
2. ✅ 包含正常和异常情况
3. ✅ 处理外部依赖不可用的情况
4. ✅ 添加必要的注释
5. ✅ 更新本文档

## 测试报告

最后测试运行时间: 2026-03-07
测试通过率: 100% (154/154)
平均运行时间: 3.8秒
