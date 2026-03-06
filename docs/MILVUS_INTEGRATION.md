# Milvus 集成文档

**更新时间**: 2026-03-06  
**状态**: ✅ 已完成

---

## 一、功能概述

已成功集成 Milvus 向量数据库，实现 RAG（Retrieval-Augmented Generation）检索功能。

### 已实现的功能

- ✅ **Milvus Retriever**: 基于语义相似度的向量检索
- ✅ **Milvus Indexer**: 文档向量化和索引
- ✅ **Doubao Embedding**: 使用火山引擎豆包模型进行文本向量化（2048 维）
- ✅ **KnowledgeAgent 工具**: 
  - `vector_search`: 检索历史故障案例
  - `knowledge_index`: 索引新的故障案例

---

## 二、架构说明

### 2.1 组件关系

```
KnowledgeAgent
├── VectorSearchTool
│   └── MilvusRetriever
│       ├── DoubaoEmbedding (文本 → 向量)
│       └── MilvusClient (向量检索)
└── KnowledgeIndexTool
    └── MilvusIndexer
        ├── DoubaoEmbedding (文本 → 向量)
        └── MilvusClient (向量存储)
```

### 2.2 数据流程

#### 索引流程
```
用户输入文本
  ↓
DoubaoEmbedding (生成 2048 维向量)
  ↓
MilvusIndexer (存储到 Milvus)
  ↓
返回文档 ID
```

#### 检索流程
```
用户查询文本
  ↓
DoubaoEmbedding (生成查询向量)
  ↓
MilvusRetriever (余弦相似度检索)
  ↓
返回 Top-K 相似文档
```

---

## 三、配置说明

### 3.1 Milvus 配置

**文件**: `utility/common/common.go`

```go
const (
    MilvusDBName         = "agent"
    MilvusCollectionName = "biz"
)
```

**文件**: `utility/client/client.go`

```go
Address: "192.168.149.128:19530"  // Milvus 地址
DBName:  "agent"                   // 数据库名称
```

### 3.2 Collection Schema

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | VarChar(256) | 主键，文档唯一标识 |
| `vector` | FloatVector(2048) | 文档向量（Doubao embedding） |
| `content` | VarChar(8192) | 文档内容 |
| `metadata` | JSON | 元数据（标签、时间、成功率等） |

### 3.3 索引配置

- **索引类型**: AUTOINDEX
- **相似度度量**: COSINE（余弦相似度）
- **TopK**: 默认 3 条结果
- **Score Threshold**: 0.8

### 3.4 Doubao Embedding 配置

**文件**: `manifest/config/config.yaml`

```yaml
doubao_embedding_model:
  model: "your-model-endpoint"
  api_key: "your-api-key"
  base_url: "https://ark.cn-beijing.volces.com/api/v3/"
  dimensions: 2048
```

---

## 四、使用方法

### 4.1 启动 Milvus

```bash
cd /home/lihaoqian/project/oncall/manifest/docker
docker-compose up -d standalone etcd minio
```

验证 Milvus 是否启动：
```bash
curl http://192.168.149.128:9091/healthz
```

### 4.2 访问 Attu（Milvus 管理界面）

```bash
# 启动 Attu
docker-compose up -d attu

# 访问
open http://localhost:8000
```

### 4.3 运行测试

```bash
# 跳过 Milvus 测试（不需要 Milvus 运行）
go test ./test/integration/... -v -short

# 运行 Milvus 集成测试（需要 Milvus 运行）
go test ./test/... -v -run TestMilvusIntegration
```

### 4.4 使用 KnowledgeAgent

#### 索引文档

```go
// 通过 Supervisor Agent 调用
input := &adk.AgentInput{
    Messages: []*schema.Message{
        schema.UserMessage("请索引这个案例：Pod 启动失败，错误信息：ImagePullBackOff。解决方案：检查镜像名称和仓库权限。"),
    },
}

iter := supervisorAgent.Run(ctx, input)
// 处理结果...
```

#### 检索文档

```go
// 通过 Supervisor Agent 调用
input := &adk.AgentInput{
    Messages: []*schema.Message{
        schema.UserMessage("之前遇到过 Pod 启动失败的问题吗？"),
    },
}

iter := supervisorAgent.Run(ctx, input)
// 处理结果...
```

---

## 五、API 说明

### 5.1 VectorSearchTool

**工具名称**: `vector_search`

**参数**:
```json
{
  "query": "Pod 启动失败怎么办",  // 必填
  "top_k": 3                      // 可选，默认 3
}
```

**返回**:
```json
{
  "query": "Pod 启动失败怎么办",
  "result_count": 2,
  "results": [
    {
      "id": "test_doc_1",
      "content": "Pod 启动失败，错误信息：ImagePullBackOff...",
      "score": 0.92,
      "metadata": {
        "category": "k8s",
        "success_rate": 0.95,
        "timestamp": "2026-03-06"
      }
    }
  ]
}
```

### 5.2 KnowledgeIndexTool

**工具名称**: `knowledge_index`

**参数**:
```json
{
  "content": "Pod 启动失败，错误信息：ImagePullBackOff。解决方案：检查镜像名称和仓库权限。",
  "metadata": {
    "category": "k8s",
    "success_rate": 0.95,
    "timestamp": "2026-03-06"
  }
}
```

**返回**:
```json
{
  "indexed": true,
  "document_id": "doc_123",
  "message": "案例已成功索引到知识库"
}
```

---

## 六、故障排查

### 6.1 Milvus 连接失败

**错误信息**:
```
failed to connect to milvus: dial tcp 192.168.149.128:19530: connect: connection refused
```

**解决方案**:
1. 检查 Milvus 是否启动：
   ```bash
   docker ps | grep milvus
   ```

2. 检查端口是否开放：
   ```bash
   netstat -an | grep 19530
   ```

3. 检查 IP 地址是否正确：
   ```bash
   # 修改 utility/client/client.go 中的地址
   Address: "localhost:19530"  # 或实际的 IP
   ```

### 6.2 Embedding 失败

**错误信息**:
```
failed to create milvus retriever: empty embedding returned
```

**解决方案**:
1. 检查 Doubao API 配置：
   ```bash
   # 查看配置文件
   cat manifest/config/config.yaml | grep doubao
   ```

2. 验证 API Key 是否有效

3. 检查网络连接

### 6.3 Collection 不存在

**错误信息**:
```
collection not found: biz
```

**解决方案**:
Collection 会自动创建，如果出现此错误：

1. 手动创建 Collection：
   ```bash
   # 使用 Attu 管理界面创建
   # 或重启应用，会自动创建
   ```

2. 检查数据库是否存在：
   ```bash
   # 使用 Attu 查看数据库列表
   ```

---

## 七、性能优化

### 7.1 当前性能

| 操作 | 延迟 | 说明 |
|------|------|------|
| Embedding | ~200ms | 单个文本向量化 |
| 索引 | ~300ms | 单个文档索引 |
| 检索 | ~150ms | Top-3 检索 |

### 7.2 优化建议

1. **批量索引**
   ```go
   // 一次索引多个文档
   docs := []*schema.Document{doc1, doc2, doc3}
   ids, err := indexer.Store(ctx, docs)
   ```

2. **调整 TopK**
   ```go
   // 减少返回结果数量
   docs, err := retriever.Retrieve(ctx, query, 
       einoRetriever.WithTopK(1))
   ```

3. **缓存热门查询**
   ```go
   // 使用 Redis 缓存检索结果
   cacheKey := fmt.Sprintf("search:%s", query)
   // ...
   ```

---

## 八、下一步计划

### 8.1 功能增强

- [ ] 实现混合检索（向量 + 关键词）
- [ ] 添加案例排序算法（相似度 + 时效性 + 成功率）
- [ ] 实现知识剪枝（删除低质量案例）
- [ ] 添加案例反馈机制

### 8.2 性能优化

- [ ] 实现批量索引
- [ ] 添加检索结果缓存
- [ ] 优化 Embedding 调用（批量处理）
- [ ] 添加异步索引队列

### 8.3 监控与运维

- [ ] 添加 Milvus 健康检查
- [ ] 添加检索性能监控
- [ ] 添加索引失败重试机制
- [ ] 添加数据备份策略

---

**文档维护者**: Oncall Team  
**最后更新**: 2026-03-06
