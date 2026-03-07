# Agent 示例程序

本目录包含用于测试和演示各个 Agent 功能的独立程序。

## 目录结构

```
examples/
├── chat_cmd/          # DialogueAgent 对话测试
├── ai_ops_cmd/        # OpsAgent 运维监控测试
├── knowledge_cmd/     # KnowledgeAgent 知识检索测试
├── recall_cmd/        # 召回测试
└── llm_tool_cmd/      # LLM 工具测试
```

## 使用方法

### 1. DialogueAgent 测试

测试意图分析和问题预测功能：

```bash
cd internal/ai/examples/chat_cmd
go run main.go
```

**功能**：
- 意图识别（monitor/diagnose/knowledge/execute/general）
- 语义熵计算
- 问题预测

### 2. OpsAgent 测试

测试运维监控功能：

```bash
cd internal/ai/examples/ai_ops_cmd
go run main.go
```

**功能**：
- K8s 资源监控
- Prometheus 指标查询
- 日志分析

**前置条件**：
- Prometheus 运行在 `http://localhost:30090`
- K8s 集群可访问（可选）

### 3. KnowledgeAgent 测试

测试知识检索和索引功能：

```bash
cd internal/ai/examples/knowledge_cmd
go run main.go
```

**功能**：
- 向量检索
- 知识索引
- 案例排序

**前置条件**：
- Milvus 运行在 `localhost:31953`

### 4. Recall 测试

测试召回功能：

```bash
cd internal/ai/examples/recall_cmd
go run main.go
```

### 5. LLM Tool 测试

测试 LLM 工具调用：

```bash
cd internal/ai/examples/llm_tool_cmd
go run main.go
```

## 环境要求

所有示例程序都需要：

1. **LLM 配置**：
   - DeepSeek V3 API Key（通过 Volcengine Ark）
   - 配置文件：`manifest/config/config.yaml`

2. **可选服务**（根据具体示例）：
   - Redis (localhost:6379)
   - Milvus (localhost:31953)
   - Prometheus (localhost:30090)
   - K8s 集群

## 注意事项

1. 这些是**开发调试工具**，不是生产代码
2. 每个程序都是独立的，可以单独运行
3. 如果外部服务不可用，程序会使用降级模式或报错
4. 建议在开发环境中使用，不要在生产环境运行

## 与测试的区别

| 类型 | 位置 | 用途 | 运行方式 |
|------|------|------|---------|
| **示例程序** | `internal/ai/examples/` | 手动测试、演示功能 | `go run main.go` |
| **单元测试** | `test/*_test.go` | 自动化测试 | `go test ./test -v` |
| **集成测试** | `test/integration/` | 端到端测试 | `go test ./test/integration -v` |

## 相关文档

- [项目架构文档](../../../docs/update.md)
- [单元测试说明](../../../test/README.md)
- [部署文档](../../../manifest/README.md)
