# 集成测试

本目录包含需要外部服务的集成测试和端到端测试。

## 测试文件

### 1. `e2e_test.go` - 端到端测试

测试完整的 Supervisor Agent 工作流程。

**测试用例**：
- `TestEndToEnd_SupervisorAgent` - 单轮对话测试
- `TestEndToEnd_MultiRound` - 多轮对话测试

**运行方式**：
```bash
# 运行所有集成测试
go test ./test/integration -v

# 跳过集成测试（快速测试）
go test ./test/integration -v -short
```

**前置条件**：
- Redis 运行在 `localhost:6379`
- Milvus 运行在 `localhost:31953`
- LLM API 配置正确

### 2. `test_milvus.go` - Milvus 连接测试

独立的 Milvus 连接测试程序（非测试用例）。

**运行方式**：
```bash
cd test/integration
go run test_milvus.go
```

**功能**：
- 测试 Milvus 连接
- 列出所有 Collections
- 验证数据库配置

### 3. `test_prometheus.go` - Prometheus 连接测试

独立的 Prometheus 连接测试程序（非测试用例）。

**运行方式**：
```bash
cd test/integration
go run test_prometheus.go
```

**功能**：
- 测试 Prometheus 连接
- 验证 OpsAgent 工具可用性
- 测试指标采集

## 环境配置

### 启动所有依赖服务

本项目使用 Kubernetes 部署所有依赖服务：

```bash
# 检查 K8s 集群状态
kubectl cluster-info

# 查看服务状态
kubectl get pods -n infra
kubectl get svc -n infra

# 验证服务可用性
redis-cli -h localhost -p 30379 ping
curl http://localhost:30090/-/healthy  # Prometheus
curl http://localhost:31953/health     # Milvus
```

**注意**：测试使用 NodePort 访问服务，端口映射如下：
- Redis: 6379 → 30379
- Prometheus: 9090 → 30090
- Milvus: 19530 → 31953
- MySQL: 3306 → 30306

### 服务列表

| 服务 | 地址 | 用途 | 状态 |
|------|------|------|------|
| Redis | localhost:30379 (NodePort) | 会话存储 | ✅ 可用 |
| Milvus | localhost:31953 (NodePort) | 向量数据库 | ✅ 可用 |
| Prometheus | localhost:30090 (NodePort) | 指标监控 | ✅ 可用 |
| MySQL | localhost:30306 (NodePort) | 数据存储 | ✅ 可用 |
| Etcd | localhost:2379 | 配置中心 | ✅ 可用 |
| K8s Cluster | https://127.0.0.1:6443 | 容器编排 | ✅ 可用 |

## 测试策略

### 单元测试 vs 集成测试

| 类型 | 位置 | 依赖 | 运行速度 | 覆盖范围 |
|------|------|------|---------|---------|
| **单元测试** | `test/*_test.go` | 无外部依赖 | 快（3-4秒） | 单个组件 |
| **集成测试** | `test/integration/` | 需要外部服务 | 慢（10-30秒） | 端到端流程 |

### 运行策略

**开发阶段**：
```bash
# 快速验证 - 只运行单元测试
go test ./test -v

# 完整验证 - 运行所有测试
go test ./test/... -v
```

**CI/CD 流程**：
```bash
# 1. 单元测试（必须通过）
go test ./test -v

# 2. 集成测试（可选，需要环境）
go test ./test/integration -v
```

## 测试覆盖率

当前集成测试覆盖：

- ✅ Prometheus 指标采集（完整通过）
  - ✅ 即时查询
  - ✅ 范围查询
  - ✅ CPU/内存指标
  - ✅ 聚合查询
  - ✅ 性能测试
- ✅ Redis 会话管理（完整通过）
  - ✅ 会话存储
  - ✅ 会话恢复
  - ✅ 并发会话
  - ✅ 会话隔离
  - ✅ 性能测试
- ⚠️  K8s 资源监控（部分通过 - 降级模式）
  - ✅ Pod 监控（降级响应）
  - ✅ 可用性检测
  - ⚠️  Deployment/Service/Node 查询（需要集群内运行）
- ⚠️  SupervisorAgent 端到端流程（需要 LLM API）
  - ⚠️  单轮对话（需要有效的 API 密钥）
  - ⚠️  多轮对话（需要有效的 API 密钥）
  - ⚠️  知识检索（需要有效的 API 密钥）

## 添加新的集成测试

### 1. 创建测试文件

```go
package integration

import (
    "testing"
)

func TestYourFeature(t *testing.T) {
    // 跳过短测试模式
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    // 测试逻辑
    // ...
}
```

### 2. 遵循命名约定

- 测试文件：`*_test.go`
- 测试函数：`TestXxx`
- 独立程序：`test_*.go`（不是测试用例）

### 3. 处理外部依赖

```go
// 检查服务可用性
if !isServiceAvailable("localhost:6379") {
    t.Skip("Redis not available")
}
```

## 故障排查

### Redis 连接失败

```bash
# 检查 Redis 是否运行
redis-cli ping

# 启动 Redis
docker-compose up -d redis
```

### Milvus 连接失败

```bash
# 检查 Milvus 状态
curl http://localhost:31953/health

# 重启 Milvus
docker-compose restart milvus-standalone
```

### 测试超时

```bash
# 增加超时时间
go test ./test/integration -v -timeout 5m
```

## 相关文档

- [单元测试文档](../README.md)
- [项目架构文档](../../docs/update.md)
- [部署文档](../../manifest/docker/README.md)
